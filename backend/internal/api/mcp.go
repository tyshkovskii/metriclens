package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"metriclens/backend/internal/diagnosis"
	"metriclens/backend/internal/model"
)

const mcpInstructions = "Wait for explicit services with wait_for_stack after starting them, then call start_experiment immediately before the workload. Use normal development tools, call compare_experiment afterward, and request metric evidence only for significant findings. MetricLens observes retained metrics; it does not start Docker, provide logs or traces, or replay execution."

type observeStackInput struct {
	Window      string   `json:"window,omitempty" jsonschema:"positive duration, bounded by retained history"`
	Limit       *int     `json:"limit,omitempty" jsonschema:"maximum number of findings"`
	Severities  []string `json:"severities,omitempty" jsonschema:"filter by info, warning, or error"`
	Services    []string `json:"services,omitempty" jsonschema:"filter by service name"`
	ChangedOnly bool     `json:"changedOnly,omitempty" jsonschema:"only findings whose state changed"`
}

type waitForStackInput struct {
	Services  []string `json:"services" jsonschema:"Compose or service names to wait for"`
	TimeoutMs *int64   `json:"timeoutMs,omitempty" jsonschema:"non-negative wait in milliseconds, up to 120000; omitted uses the default"`
}

type startExperimentInput struct {
	Name        string `json:"name,omitempty" jsonschema:"optional human-readable experiment name"`
	ClientRunID string `json:"clientRunId,omitempty" jsonschema:"optional caller correlation ID"`
}

type compareExperimentInput struct {
	From        string   `json:"from" jsonschema:"marker ID returned by start_experiment"`
	To          string   `json:"to,omitempty" jsonschema:"optional marker ID; omitted compares through now"`
	Limit       *int     `json:"limit,omitempty" jsonschema:"maximum number of findings"`
	Severities  []string `json:"severities,omitempty" jsonschema:"filter by info, warning, or error"`
	Services    []string `json:"services,omitempty" jsonschema:"filter by service name"`
	ChangedOnly bool     `json:"changedOnly,omitempty" jsonschema:"only findings whose state changed"`
}

type metricEvidenceInput struct {
	TargetID  string   `json:"targetId" jsonschema:"target ID returned by the targets endpoint"`
	Metrics   []string `json:"metrics" jsonschema:"one to ten metric names"`
	Start     string   `json:"start,omitempty" jsonschema:"optional RFC3339 lower bound"`
	End       string   `json:"end,omitempty" jsonschema:"optional RFC3339 upper bound"`
	At        string   `json:"at,omitempty" jsonschema:"optional RFC3339 point-in-time bound"`
	MaxPoints *int     `json:"maxPoints,omitempty" jsonschema:"optional per-series point cap, up to 200"`
}

type metricEvidenceOutput struct {
	Series          []model.Series `json:"series"`
	SeriesCount     int            `json:"seriesCount"`
	PointCount      int            `json:"pointCount"`
	SeriesTruncated bool           `json:"seriesTruncated"`
	PointsTruncated bool           `json:"pointsTruncated"`
}

func newMCPHandler(s *Server) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "metriclens", Version: Version}, &mcp.ServerOptions{
		Instructions: mcpInstructions,
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	})
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
	stateChanging := &mcp.ToolAnnotations{DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "observe_stack",
		Title:       "Observe stack",
		Description: "Return a compact, bounded diagnosis report for the retained metric window.",
		Annotations: readOnly,
	}, s.observeStack)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wait_for_stack",
		Title:       "Wait for stack",
		Description: "Wait until every requested service has a successful scrape, or return readiness evidence.",
		Annotations: readOnly,
	}, s.waitForStack)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "start_experiment",
		Title:       "Start experiment",
		Description: "Create a retained marker immediately before an agent workload.",
		Annotations: stateChanging,
	}, s.startExperiment)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "compare_experiment",
		Title:       "Compare experiment",
		Description: "Compare retained observations from a marker through another marker or now.",
		Annotations: readOnly,
	}, s.compareExperiment)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_metric_evidence",
		Title:       "Get metric evidence",
		Description: "Return bounded metric series for a significant diagnosis finding.",
		Annotations: readOnly,
	}, s.getMetricEvidence)

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
	return http.NewCrossOriginProtection().Handler(handler)
}

func boolPtr(value bool) *bool {
	return &value
}

func (s *Server) observeStack(ctx context.Context, _ *mcp.CallToolRequest, input observeStackInput) (*mcp.CallToolResult, diagnosis.Report, error) {
	request, err := s.parseMCPReportRequest(input.Window, input.Limit, input.Severities, input.Services, input.ChangedOnly)
	if err != nil {
		return nil, diagnosis.Report{}, mcpToolError(err)
	}
	return nil, diagnosis.BuildWithOptions(s.targets, s.currentTime(), request.Window, request.Limit, request.Options), nil
}

func (s *Server) waitForStack(ctx context.Context, _ *mcp.CallToolRequest, input waitForStackInput) (*mcp.CallToolResult, readinessResponse, error) {
	services, err := parseServiceList(input.Services)
	if err != nil {
		return nil, readinessResponse{}, mcpToolError(err)
	}
	timeout := DefaultReadinessTimeout
	if input.TimeoutMs != nil {
		if *input.TimeoutMs < 0 || *input.TimeoutMs > MaxReadinessTimeout.Milliseconds() {
			return nil, readinessResponse{}, mcpToolError(errors.New("timeoutMs must be between 0 and 120000"))
		}
		timeout = time.Duration(*input.TimeoutMs) * time.Millisecond
	}
	result, err := s.waitForReadiness(ctx, services, timeout)
	if err != nil {
		return nil, readinessResponse{}, mcpToolError(errors.New("readiness request canceled before services became ready"))
	}
	return nil, result, nil
}

func (s *Server) startExperiment(_ context.Context, _ *mcp.CallToolRequest, input startExperimentInput) (*mcp.CallToolResult, Marker, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.ClientRunID = strings.TrimSpace(input.ClientRunID)
	if err := validateMarkerField("name", input.Name); err != nil {
		return nil, Marker{}, mcpToolError(err)
	}
	if err := validateMarkerField("clientRunId", input.ClientRunID); err != nil {
		return nil, Marker{}, mcpToolError(err)
	}
	return nil, s.markers.create(s.currentTime(), s.effectiveRetention(), input.Name, input.ClientRunID), nil
}

func (s *Server) compareExperiment(_ context.Context, _ *mcp.CallToolRequest, input compareExperimentInput) (*mcp.CallToolResult, diagnosis.Report, error) {
	input.From = strings.TrimSpace(input.From)
	if input.From == "" {
		return nil, diagnosis.Report{}, mcpToolError(errors.New("from marker is required"))
	}
	input.To = strings.TrimSpace(input.To)
	now := s.currentTime()
	retention := s.effectiveRetention()
	from, ok := s.markers.get(input.From, now, retention)
	if !ok {
		return nil, diagnosis.Report{}, mcpToolError(errors.New("unknown or expired from marker"))
	}
	toTime := now
	var toMarker *storedMarker
	if input.To != "" {
		to, ok := s.markers.get(input.To, now, retention)
		if !ok {
			return nil, diagnosis.Report{}, mcpToolError(errors.New("unknown or expired to marker"))
		}
		toTime = to.time
		toMarker = &to
	}
	if !from.time.Before(toTime) {
		return nil, diagnosis.Report{}, mcpToolError(errors.New("from marker must be before to marker"))
	}
	limit, err := mcpLimit(input.Limit)
	if err != nil {
		return nil, diagnosis.Report{}, mcpToolError(err)
	}
	options, err := mcpReportOptions(input.Severities, input.Services, input.ChangedOnly)
	if err != nil {
		return nil, diagnosis.Report{}, mcpToolError(err)
	}
	report := diagnosis.BuildWithOptions(s.targets, toTime, toTime.Sub(from.time), limit, options)
	report.From = markerRef(from.Marker)
	if toMarker != nil {
		report.To = markerRef(toMarker.Marker)
	}
	return nil, report, nil
}

func (s *Server) getMetricEvidence(_ context.Context, _ *mcp.CallToolRequest, input metricEvidenceInput) (*mcp.CallToolResult, metricEvidenceOutput, error) {
	targetID := strings.TrimSpace(input.TargetID)
	if targetID == "" {
		return nil, metricEvidenceOutput{}, mcpToolError(errors.New("targetId is required"))
	}
	metrics, err := parseMCPMetricNames(input.Metrics)
	if err != nil {
		return nil, metricEvidenceOutput{}, mcpToolError(err)
	}
	values := url.Values{}
	for _, metric := range metrics {
		values.Add("metrics", metric)
	}
	if input.Start != "" {
		values.Set("start", input.Start)
	}
	if input.End != "" {
		values.Set("end", input.End)
	}
	if input.At != "" {
		values.Set("at", input.At)
	}
	bounds, err := parseSeriesBoundsValues(values)
	if err != nil {
		return nil, metricEvidenceOutput{}, mcpToolError(err)
	}
	maxPointsRaw := ""
	if input.MaxPoints != nil {
		maxPointsRaw = strconv.Itoa(*input.MaxPoints)
	}
	maxPoints, err := parseBatchMaxPoints(maxPointsRaw)
	if err != nil {
		return nil, metricEvidenceOutput{}, mcpToolError(err)
	}
	result := s.batchSeries(targetID, metrics, bounds, maxPoints)
	return nil, metricEvidenceOutput(result), nil
}

func (s *Server) parseMCPReportRequest(window string, limit *int, severities, services []string, changedOnly bool) (reportRequest, error) {
	values := url.Values{}
	if window != "" {
		values.Set("window", window)
	}
	if limit != nil {
		values.Set("limit", strconv.Itoa(*limit))
	}
	for _, severity := range severities {
		values.Add("severity", severity)
	}
	for _, service := range services {
		values.Add("service", service)
	}
	if changedOnly {
		values.Set("changedOnly", "true")
	}
	return s.parseReportRequest(values)
}

func mcpReportOptions(severities, services []string, changedOnly bool) (diagnosis.BuildOptions, error) {
	values := url.Values{}
	for _, severity := range severities {
		values.Add("severity", severity)
	}
	for _, service := range services {
		values.Add("service", service)
	}
	if changedOnly {
		values.Set("changedOnly", "true")
	}
	return parseReportOptions(values)
}

func mcpLimit(limit *int) (int, error) {
	if limit == nil {
		return diagnosis.DefaultLimit, nil
	}
	return parseReportLimit(strconv.Itoa(*limit))
}

func parseMCPMetricNames(raw []string) ([]string, error) {
	values := url.Values{}
	for _, value := range raw {
		values.Add("metrics", value)
	}
	return parseMetricNames(values)
}

func mcpToolError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(err.Error())
	if utf8.RuneCountInString(message) > 512 {
		runes := []rune(message)
		message = string(runes[:509]) + "..."
	}
	return errors.New(message)
}
