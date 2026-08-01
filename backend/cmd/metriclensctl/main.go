package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMetricLensURL = "http://localhost:9999"
	maxResponseBytes     = 2 << 20
	maxErrorRunes        = 512
	maxReadinessTimeout  = 2 * time.Minute
	httpTimeout          = maxReadinessTimeout + 5*time.Second
	settleBuffer         = 100 * time.Millisecond
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one CLI invocation and returns its stable process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	global := flag.NewFlagSet("metriclensctl", flag.ContinueOnError)
	global.SetOutput(stderr)
	global.Usage = func() {
		if _, err := fmt.Fprintln(stderr, "usage: metriclensctl [--url URL] COMMAND [OPTIONS]"); err != nil {
			return
		}
		if _, err := fmt.Fprintln(stderr, "commands: capabilities, wait, observe, start, compare, evidence, evaluate"); err != nil {
			return
		}
	}
	baseURL := os.Getenv("METRICLENS_URL")
	if baseURL == "" {
		baseURL = defaultMetricLensURL
	}
	global.StringVar(&baseURL, "url", baseURL, "MetricLens base URL (or METRICLENS_URL)")
	if err := global.Parse(args); err != nil {
		return exitUsage(stderr, err)
	}
	if err := validateBaseURL(baseURL); err != nil {
		return exitUsage(stderr, err)
	}
	rest := global.Args()
	if len(rest) == 0 {
		global.Usage()
		return 1
	}

	parsedBaseURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return exitUsage(stderr, errors.New("--url must be a valid http or https base URL"))
	}
	client := &metricLensClient{
		baseURL: parsedBaseURL,
		http:    &http.Client{Timeout: httpTimeout},
	}
	ctx := context.Background()
	switch rest[0] {
	case "capabilities":
		return runCapabilities(ctx, client, rest[1:], stdout, stderr)
	case "wait":
		return runWait(ctx, client, rest[1:], stdout, stderr)
	case "observe":
		return runObserve(ctx, client, rest[1:], stdout, stderr)
	case "start":
		return runStart(ctx, client, rest[1:], stdout, stderr)
	case "compare":
		return runCompare(ctx, client, rest[1:], stdout, stderr)
	case "evidence":
		return runEvidence(ctx, client, rest[1:], stdout, stderr)
	case "evaluate":
		return runEvaluate(ctx, client, rest[1:], stdout, stderr)
	default:
		return exitUsage(stderr, fmt.Errorf("unknown command %q", rest[0]))
	}
}

type metricLensClient struct {
	baseURL       *url.URL
	http          *http.Client
	responseBytes int64
}

type httpStatusError struct {
	status  int
	message string
}

func (e *httpStatusError) Error() string {
	if e.message == "" {
		return fmt.Sprintf("MetricLens returned HTTP %d", e.status)
	}
	return fmt.Sprintf("MetricLens returned HTTP %d: %s", e.status, e.message)
}

func (c *metricLensClient) request(ctx context.Context, method, path string, query url.Values, payload any) ([]byte, error) {
	endpoint := *c.baseURL
	rawPath := strings.TrimRight(c.baseURL.EscapedPath(), "/") + path
	decodedPath, err := url.PathUnescape(rawPath)
	if err != nil {
		return nil, fmt.Errorf("encode request path: %w", err)
	}
	endpoint.Path = decodedPath
	endpoint.RawPath = rawPath
	if len(query) > 0 {
		endpoint.RawQuery = query.Encode()
	} else {
		endpoint.RawQuery = ""
	}

	var body io.Reader
	if payload != nil {
		encoded, encodeErr := json.Marshal(payload)
		if encodeErr != nil {
			return nil, fmt.Errorf("encode request: %w", encodeErr)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return
		}
	}()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	c.responseBytes += int64(len(data))
	if readErr != nil {
		return nil, fmt.Errorf("read response: %w", readErr)
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return data, &httpStatusError{status: resp.StatusCode, message: responseErrorMessage(data)}
	}
	return data, nil
}

func (c *metricLensClient) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.request(ctx, http.MethodGet, path, query, nil)
}

func (c *metricLensClient) post(ctx context.Context, path string, payload any) ([]byte, error) {
	return c.request(ctx, http.MethodPost, path, nil, payload)
}

func runJSONRequest(ctx context.Context, client *metricLensClient, path string, query url.Values, payload any, stdout, stderr io.Writer) int {
	body, err := client.request(ctx, http.MethodGet, path, query, payload)
	if err != nil {
		return exitRequest(stderr, err)
	}
	return writeCompactJSON(stdout, body, stderr)
}

func responseErrorMessage(data []byte) string {
	var response struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &response); err == nil && strings.TrimSpace(response.Error) != "" {
		return boundText(response.Error)
	}
	if len(data) == 0 {
		return "empty response"
	}
	return boundText(strings.TrimSpace(string(data)))
}

func runCapabilities(ctx context.Context, client *metricLensClient, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		return exitUsage(stderr, errors.New("capabilities does not accept options"))
	}
	return runJSONRequest(ctx, client, "/api/capabilities", nil, nil, stdout, stderr)
}

func runWait(ctx context.Context, client *metricLensClient, args []string, stdout, stderr io.Writer) int {
	fs := newCommandFlagSet("wait", stderr, "usage: metriclensctl wait --services SERVICE[,SERVICE...] [--timeout DURATION]")
	var services csvValues
	fs.Var(&services, "services", "required service names")
	timeout := fs.Duration("timeout", 60*time.Second, "readiness timeout")
	if err := fs.Parse(args); err != nil {
		return exitUsage(stderr, err)
	}
	if len(fs.Args()) != 0 {
		return exitUsage(stderr, errors.New("wait does not accept positional arguments"))
	}
	if err := validateServices(services); err != nil {
		return exitUsage(stderr, err)
	}
	if err := validateReadinessTimeout(*timeout); err != nil {
		return exitUsage(stderr, err)
	}
	query := url.Values{}
	for _, service := range services {
		query.Add("service", service)
	}
	query.Set("timeout", timeout.String())
	body, err := client.get(ctx, "/api/readiness", query)
	if err != nil {
		var statusErr *httpStatusError
		if errors.As(err, &statusErr) && statusErr.status == http.StatusRequestTimeout {
			if writeCompactJSON(stdout, body, stderr) != 0 {
				return 1
			}
			return 2
		}
		return exitRequest(stderr, err)
	}
	return writeCompactJSON(stdout, body, stderr)
}

func runObserve(ctx context.Context, client *metricLensClient, args []string, stdout, stderr io.Writer) int {
	fs := newCommandFlagSet("observe", stderr, "usage: metriclensctl observe [--window DURATION] [--limit N] [--severity SEVERITY] [--services SERVICE] [--changed-only]")
	window := fs.String("window", "", "diagnosis window")
	limit := fs.Int("limit", 0, "maximum findings")
	var severities, services csvValues
	fs.Var(&severities, "severity", "finding severity filter")
	fs.Var(&services, "services", "service filter")
	changedOnly := fs.Bool("changed-only", false, "only changed findings")
	if err := fs.Parse(args); err != nil {
		return exitUsage(stderr, err)
	}
	if len(fs.Args()) != 0 {
		return exitUsage(stderr, errors.New("observe does not accept positional arguments"))
	}
	if *limit < 0 {
		return exitUsage(stderr, errors.New("limit must not be negative"))
	}
	query := reportQuery(*window, *limit, severities, services, *changedOnly)
	body, err := client.get(ctx, "/api/report", query)
	if err != nil {
		return exitRequest(stderr, err)
	}
	return writeCompactJSON(stdout, body, stderr)
}

func runStart(ctx context.Context, client *metricLensClient, args []string, stdout, stderr io.Writer) int {
	fs := newCommandFlagSet("start", stderr, "usage: metriclensctl start [--name NAME] [--client-run-id ID]")
	name := fs.String("name", "", "optional marker name")
	clientRunID := fs.String("client-run-id", "", "optional caller correlation ID")
	if err := fs.Parse(args); err != nil {
		return exitUsage(stderr, err)
	}
	if len(fs.Args()) != 0 {
		return exitUsage(stderr, errors.New("start does not accept positional arguments"))
	}
	body, err := client.post(ctx, "/api/markers", map[string]string{"name": *name, "clientRunId": *clientRunID})
	if err != nil {
		return exitRequest(stderr, err)
	}
	return writeCompactJSON(stdout, body, stderr)
}

func runCompare(ctx context.Context, client *metricLensClient, args []string, stdout, stderr io.Writer) int {
	fs := newCommandFlagSet("compare", stderr, "usage: metriclensctl compare --from MARKER [--to MARKER] [--limit N] [--severity SEVERITY] [--services SERVICE] [--changed-only]")
	from := fs.String("from", "", "required starting marker")
	to := fs.String("to", "", "optional ending marker")
	limit := fs.Int("limit", 0, "maximum findings")
	var severities, services csvValues
	fs.Var(&severities, "severity", "finding severity filter")
	fs.Var(&services, "services", "service filter")
	changedOnly := fs.Bool("changed-only", false, "only changed findings")
	if err := fs.Parse(args); err != nil {
		return exitUsage(stderr, err)
	}
	if len(fs.Args()) != 0 {
		return exitUsage(stderr, errors.New("compare does not accept positional arguments"))
	}
	if strings.TrimSpace(*from) == "" {
		return exitUsage(stderr, errors.New("--from is required"))
	}
	if *limit < 0 {
		return exitUsage(stderr, errors.New("limit must not be negative"))
	}
	query := reportQuery("", *limit, severities, services, *changedOnly)
	query.Set("from", *from)
	if *to != "" {
		query.Set("to", *to)
	}
	body, err := client.get(ctx, "/api/compare", query)
	if err != nil {
		return exitRequest(stderr, err)
	}
	return writeCompactJSON(stdout, body, stderr)
}

func runEvidence(ctx context.Context, client *metricLensClient, args []string, stdout, stderr io.Writer) int {
	fs := newCommandFlagSet("evidence", stderr, "usage: metriclensctl evidence --target TARGET --metric METRIC[,METRIC...] [--start TIME|--end TIME|--at TIME] [--max-points N]")
	target := fs.String("target", "", "required target ID")
	var metrics csvValues
	fs.Var(&metrics, "metric", "required metric name; repeat or comma-separate")
	start := fs.String("start", "", "RFC3339 lower bound")
	end := fs.String("end", "", "RFC3339 upper bound")
	at := fs.String("at", "", "RFC3339 point-in-time bound")
	maxPoints := fs.Int("max-points", 0, "per-series point cap")
	if err := fs.Parse(args); err != nil {
		return exitUsage(stderr, err)
	}
	if len(fs.Args()) != 0 {
		return exitUsage(stderr, errors.New("evidence does not accept positional arguments"))
	}
	if strings.TrimSpace(*target) == "" {
		return exitUsage(stderr, errors.New("--target is required"))
	}
	if len(metrics) == 0 {
		return exitUsage(stderr, errors.New("at least one --metric is required"))
	}
	if *maxPoints < 0 {
		return exitUsage(stderr, errors.New("max-points must not be negative"))
	}
	query := url.Values{}
	query.Set("metrics", strings.Join(metrics, ","))
	if *start != "" {
		query.Set("start", *start)
	}
	if *end != "" {
		query.Set("end", *end)
	}
	if *at != "" {
		query.Set("at", *at)
	}
	if *maxPoints > 0 {
		query.Set("maxPoints", strconv.Itoa(*maxPoints))
	}
	path := "/api/targets/" + url.PathEscape(*target) + "/series/batch"
	body, err := client.get(ctx, path, query)
	if err != nil {
		return exitRequest(stderr, err)
	}
	return writeCompactJSON(stdout, body, stderr)
}

func reportQuery(window string, limit int, severities, services csvValues, changedOnly bool) url.Values {
	query := url.Values{}
	if window != "" {
		query.Set("window", window)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	for _, severity := range severities {
		query.Add("severity", severity)
	}
	for _, service := range services {
		query.Add("service", service)
	}
	if changedOnly {
		query.Set("changedOnly", "true")
	}
	return query
}

func validateServices(services csvValues) error {
	if len(services) == 0 {
		return errors.New("--services requires at least one service")
	}
	return nil
}

func validateReadinessTimeout(timeout time.Duration) error {
	if timeout < 0 {
		return errors.New("timeout must not be negative")
	}
	if timeout > maxReadinessTimeout {
		return errors.New("timeout must not exceed 2m")
	}
	return nil
}

func runEvaluate(ctx context.Context, client *metricLensClient, args []string, stdout, stderr io.Writer) int {
	fs := newCommandFlagSet("evaluate", stderr, "usage: metriclensctl evaluate --services SERVICE --expect SIGNAL [--expect SIGNAL] [options] -- WORKLOAD ARG...")
	var services, expectedSignals csvValues
	fs.Var(&services, "services", "required service names")
	fs.Var(&expectedSignals, "expect", "required expected finding signal")
	severityRaw := fs.String("severity", "warning,error", "compare severity filter (default warning,error)")
	timeout := fs.Duration("timeout", 60*time.Second, "readiness timeout")
	settle := fs.String("settle", "auto", "auto or non-negative duration")
	minF1 := fs.Float64("min-f1", 1.0, "minimum signal F1 score")
	if err := fs.Parse(args); err != nil {
		return exitUsage(stderr, err)
	}
	hasSeparator := false
	for _, arg := range args {
		if arg == "--" {
			hasSeparator = true
			break
		}
	}
	if !hasSeparator {
		return exitUsage(stderr, errors.New("evaluate requires -- before the workload"))
	}
	workload := fs.Args()
	if len(workload) > 0 && workload[0] == "--" {
		workload = workload[1:]
	}
	if err := validateServices(services); err != nil {
		return exitUsage(stderr, err)
	}
	if len(expectedSignals) == 0 {
		return exitUsage(stderr, errors.New("at least one --expect is required"))
	}
	if len(workload) == 0 {
		return exitUsage(stderr, errors.New("a workload is required after --"))
	}
	if err := validateReadinessTimeout(*timeout); err != nil {
		return exitUsage(stderr, err)
	}
	var severities csvValues
	if err := severities.Set(*severityRaw); err != nil {
		return exitUsage(stderr, err)
	}
	if *minF1 < 0 || *minF1 > 1 || (*minF1 != *minF1) {
		return exitUsage(stderr, errors.New("min-f1 must be between 0 and 1"))
	}
	settleDuration, autoSettle, err := parseSettle(*settle)
	if err != nil {
		return exitUsage(stderr, err)
	}

	started := time.Now()
	capabilitiesBody, err := client.get(ctx, "/api/capabilities", nil)
	if err != nil {
		return exitRequest(stderr, err)
	}
	var capabilities struct {
		ScrapeIntervalMs int64 `json:"scrapeIntervalMs"`
	}
	if decodeErr := decodeJSON(capabilitiesBody, &capabilities); decodeErr != nil {
		return exitRequest(stderr, fmt.Errorf("decode capabilities: %w", decodeErr))
	}

	readinessQuery := url.Values{}
	for _, service := range services {
		readinessQuery.Add("service", service)
	}
	readinessQuery.Set("timeout", timeout.String())
	readinessBody, err := client.get(ctx, "/api/readiness", readinessQuery)
	if err != nil {
		return exitRequest(stderr, err)
	}
	var readiness struct {
		Ready bool `json:"ready"`
	}
	if decodeErr := decodeJSON(readinessBody, &readiness); decodeErr != nil {
		return exitRequest(stderr, fmt.Errorf("decode readiness: %w", decodeErr))
	}
	if !readiness.Ready {
		return exitRequest(stderr, errors.New("requested services are not ready"))
	}

	markerBody, err := client.post(ctx, "/api/markers", map[string]string{"name": "metriclensctl evaluate", "clientRunId": "metriclensctl"})
	if err != nil {
		return exitRequest(stderr, err)
	}
	var marker struct {
		ID string `json:"id"`
	}
	if decodeErr := decodeJSON(markerBody, &marker); decodeErr != nil {
		return exitRequest(stderr, fmt.Errorf("decode marker: %w", decodeErr))
	}
	if strings.TrimSpace(marker.ID) == "" {
		return exitRequest(stderr, errors.New("marker response did not contain an id"))
	}

	workloadExitCode := runWorkload(ctx, workload, stderr)
	if autoSettle {
		settleDuration = time.Duration(capabilities.ScrapeIntervalMs)*time.Millisecond + settleBuffer
	}
	if settleDuration > 0 {
		timer := time.NewTimer(settleDuration)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return exitRequest(stderr, ctx.Err())
		case <-timer.C:
		}
	}

	compareQuery := reportQuery("", 0, severities, nil, false)
	compareQuery.Set("from", marker.ID)
	body, err := client.get(ctx, "/api/compare", compareQuery)
	if err != nil {
		return exitRequest(stderr, err)
	}
	var report struct {
		Findings []struct {
			Signal string `json:"signal"`
		} `json:"findings"`
		Omitted int `json:"omitted"`
	}
	if err := decodeJSON(body, &report); err != nil {
		return exitRequest(stderr, fmt.Errorf("decode compare: %w", err))
	}

	expected := sortedUnique(expectedSignals)
	actualValues := make([]string, 0, len(report.Findings))
	for _, finding := range report.Findings {
		if strings.TrimSpace(finding.Signal) != "" {
			actualValues = append(actualValues, finding.Signal)
		}
	}
	actual := sortedUnique(actualValues)
	matched, falsePositive, falseNegative := signalSets(expected, actual)
	precision, recall, f1 := signalScores(len(matched), len(expected), len(actual))
	result := evaluationResult{
		DiscoveryCalls:          1,
		ToolCalls:               3,
		ResponseBytes:           client.responseBytes,
		EstimatedResponseTokens: (client.responseBytes + 3) / 4,
		ExpectedSignals:         expected,
		ActualSignals:           actual,
		MatchedSignals:          matched,
		FalsePositiveSignals:    falsePositive,
		FalseNegativeSignals:    falseNegative,
		OmittedFindings:         report.Omitted,
		Precision:               precision,
		Recall:                  recall,
		SignalF1:                f1,
		WorkloadExitCode:        workloadExitCode,
		DurationMs:              time.Since(started).Milliseconds(),
		Passed:                  workloadExitCode == 0 && f1 >= *minF1 && report.Omitted == 0,
	}
	if err := writeValueJSON(stdout, result); err != nil {
		return exitRequest(stderr, err)
	}
	if !result.Passed {
		return 2
	}
	return 0
}

type evaluationResult struct {
	DiscoveryCalls          int      `json:"discoveryCalls"`
	ToolCalls               int      `json:"toolCalls"`
	ResponseBytes           int64    `json:"responseBytes"`
	EstimatedResponseTokens int64    `json:"estimatedResponseTokens"`
	ExpectedSignals         []string `json:"expectedSignals"`
	ActualSignals           []string `json:"actualSignals"`
	MatchedSignals          []string `json:"matchedSignals"`
	FalsePositiveSignals    []string `json:"falsePositiveSignals"`
	FalseNegativeSignals    []string `json:"falseNegativeSignals"`
	OmittedFindings         int      `json:"omittedFindings"`
	Precision               float64  `json:"precision"`
	Recall                  float64  `json:"recall"`
	SignalF1                float64  `json:"signalF1"`
	WorkloadExitCode        int      `json:"workloadExitCode"`
	DurationMs              int64    `json:"durationMs"`
	Passed                  bool     `json:"passed"`
}

func runWorkload(ctx context.Context, args []string, stderr io.Writer) int {
	// #nosec G204 -- evaluate intentionally executes the explicit user workload without a shell.
	command := exec.CommandContext(ctx, args[0], args[1:]...)
	command.Stdout = stderr
	command.Stderr = stderr
	err := command.Run()
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

func parseSettle(raw string) (time.Duration, bool, error) {
	if raw == "auto" {
		return 0, true, nil
	}
	duration, err := time.ParseDuration(raw)
	if err != nil || duration < 0 {
		return 0, false, errors.New("settle must be auto or a non-negative duration")
	}
	return duration, false, nil
}

func sortedUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func signalSets(expected, actual []string) (matched, falsePositive, falseNegative []string) {
	expectedSet := make(map[string]struct{}, len(expected))
	actualSet := make(map[string]struct{}, len(actual))
	for _, value := range expected {
		expectedSet[value] = struct{}{}
	}
	for _, value := range actual {
		actualSet[value] = struct{}{}
	}
	for _, value := range expected {
		if _, ok := actualSet[value]; ok {
			matched = append(matched, value)
		} else {
			falseNegative = append(falseNegative, value)
		}
	}
	for _, value := range actual {
		if _, ok := expectedSet[value]; !ok {
			falsePositive = append(falsePositive, value)
		}
	}
	return matched, falsePositive, falseNegative
}

func signalScores(matched, expected, actual int) (precision, recall, f1 float64) {
	if actual == 0 {
		if expected == 0 {
			return 1, 1, 1
		}
		return 0, 0, 0
	}
	precision = float64(matched) / float64(actual)
	if expected == 0 {
		return precision, 0, 0
	}
	recall = float64(matched) / float64(expected)
	if precision+recall == 0 {
		return precision, recall, 0
	}
	f1 = 2 * precision * recall / (precision + recall)
	return precision, recall, f1
}

func newCommandFlagSet(name string, stderr io.Writer, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		if _, err := fmt.Fprintln(stderr, usage); err != nil {
			return
		}
	}
	return fs
}

func writeCompactJSON(stdout io.Writer, data []byte, stderr io.Writer) int {
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return exitRequest(stderr, fmt.Errorf("response was not valid JSON: %w", err))
	}
	if _, err := compact.WriteTo(stdout); err != nil {
		return exitRequest(stderr, fmt.Errorf("write output: %w", err))
	}
	if _, err := io.WriteString(stdout, "\n"); err != nil {
		return exitRequest(stderr, fmt.Errorf("write output: %w", err))
	}
	return 0
}

func writeValueJSON(stdout io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode output: %w", err)
	}
	if _, err := stdout.Write(data); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if _, err := io.WriteString(stdout, "\n"); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

func decodeJSON(data []byte, target any) error {
	if err := json.Unmarshal(data, target); err != nil {
		return err
	}
	return nil
}

func exitUsage(stderr io.Writer, err error) int {
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "metriclensctl: %s\n", boundText(err.Error())); writeErr != nil {
			return 1
		}
	}
	return 1
}

func exitRequest(stderr io.Writer, err error) int {
	return exitUsage(stderr, err)
}

func boundText(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maxErrorRunes {
		return value
	}
	return string(runes[:maxErrorRunes-3]) + "..."
}

func validateBaseURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return errors.New("--url must be a valid http or https URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("--url must be a valid http or https base URL")
	}
	return nil
}

type csvValues []string

func (v *csvValues) String() string {
	return strings.Join(*v, ",")
}

func (v *csvValues) Set(raw string) error {
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return errors.New("list values must not be empty")
		}
		*v = append(*v, part)
	}
	return nil
}
