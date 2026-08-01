package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"metriclens/backend/internal/model"
)

func TestMCPToolsThroughOfficialClient(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	store := &fakeBatchTargetStore{
		fakeTargetStore: fakeTargetStore{
			targets: []model.Target{{ID: "api-1", ServiceName: "api", Status: model.TargetStatusUp, LastScrapeAt: now.Format(time.RFC3339)}},
		},
		batch: []model.Series{{
			TargetID: "api-1", Metric: "http_requests_total", Labels: map[string]string{"status": "500"},
			Points: []model.SeriesPoint{{TS: now.Add(-2 * time.Minute).Format(time.RFC3339Nano), Value: 1}, {TS: now.Add(-time.Minute).Format(time.RFC3339Nano), Value: 3}},
		}},
	}
	server := NewServer(fakeContainerLister{}, store, Config{Retention: 10 * time.Minute})
	server.now = func() time.Time { return now }
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL + "/mcp",
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	defer func() {
		if closeErr := session.Close(); closeErr != nil {
			t.Errorf("close MCP session: %v", closeErr)
		}
	}()

	initialization := session.InitializeResult()
	if initialization == nil || initialization.ServerInfo.Name != "metriclens" || initialization.ServerInfo.Version != Version {
		t.Fatalf("initialize result = %#v, want metriclens identity", initialization)
	}
	if initialization.Instructions != mcpInstructions {
		t.Fatalf("instructions = %q, want concise workflow instructions", initialization.Instructions)
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 5 {
		t.Fatalf("tool count = %d, want exactly five", len(tools.Tools))
	}
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		if tool.Title == "" || tool.Description == "" || tool.OutputSchema == nil {
			t.Errorf("tool %q metadata = %#v, want title/description/output schema", tool.Name, tool)
		}
		if tool.Annotations == nil || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			t.Errorf("tool %q annotations = %#v, want OpenWorldHint false", tool.Name, tool.Annotations)
		}
		if schema, ok := tool.InputSchema.(map[string]any); !ok || schema["type"] != "object" {
			t.Errorf("tool %q input schema = %#v, want object schema", tool.Name, tool.InputSchema)
		}
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"compare_experiment", "get_metric_evidence", "observe_stack", "start_experiment", "wait_for_stack"}) {
		t.Fatalf("tool names = %#v, want exact names", names)
	}
	for _, tool := range tools.Tools {
		switch tool.Name {
		case "observe_stack", "wait_for_stack", "compare_experiment", "get_metric_evidence":
			if !tool.Annotations.ReadOnlyHint {
				t.Errorf("tool %q ReadOnlyHint = false, want true", tool.Name)
			}
		case "start_experiment":
			if tool.Annotations.ReadOnlyHint || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
				t.Errorf("start annotations = %#v, want readOnly false/destructive false", tool.Annotations)
			}
		}
	}

	waitResult := callMCPTool(t, session, "wait_for_stack", map[string]any{"services": []string{"api"}, "timeoutMs": 0})
	if waitResult.IsError {
		t.Fatalf("wait_for_stack result = %#v, want success", waitResult)
	}
	var readiness readinessResponse
	decodeMCPStructured(t, waitResult, &readiness)
	if !readiness.Ready || len(readiness.Services) != 1 || readiness.Services[0].Service != "api" {
		t.Fatalf("readiness = %#v, want ready api", readiness)
	}

	startResult := callMCPTool(t, session, "start_experiment", map[string]any{"name": "checkout", "clientRunId": "run-1"})
	var marker Marker
	decodeMCPStructured(t, startResult, &marker)
	if marker.ID == "" || marker.Name != "checkout" || marker.ClientRunID != "run-1" {
		t.Fatalf("marker = %#v, want named marker", marker)
	}
	now = now.Add(time.Minute)
	compareResult := callMCPTool(t, session, "compare_experiment", map[string]any{"from": marker.ID, "limit": 1})
	var report map[string]any
	decodeMCPStructured(t, compareResult, &report)
	if report["from"] == nil {
		t.Fatalf("compare structured result = %#v, want from marker", report)
	}

	observeResult := callMCPTool(t, session, "observe_stack", map[string]any{"window": "2m", "limit": 1, "services": []string{"api"}})
	var observed map[string]any
	decodeMCPStructured(t, observeResult, &observed)
	if observed["window"] == nil || observed["findings"] == nil {
		t.Fatalf("observe structured result = %#v, want compact report", observed)
	}

	evidenceResult := callMCPTool(t, session, "get_metric_evidence", map[string]any{
		"targetId": "api-1", "metrics": []string{"http_requests_total"}, "maxPoints": 1,
	})
	var evidence metricEvidenceOutput
	decodeMCPStructured(t, evidenceResult, &evidence)
	if evidence.SeriesCount != 1 || evidence.PointCount != 1 || evidence.SeriesTruncated || !evidence.PointsTruncated || len(evidence.Series) != 1 || len(evidence.Series[0].Points) != 1 {
		t.Fatalf("evidence = %#v, want bounded one-point metadata", evidence)
	}

	invalidResult := callMCPTool(t, session, "wait_for_stack", map[string]any{})
	if !invalidResult.IsError {
		t.Fatalf("invalid wait result = %#v, want IsError", invalidResult)
	}
	invalidEvidence := callMCPTool(t, session, "get_metric_evidence", map[string]any{"metrics": []string{"up"}})
	if !invalidEvidence.IsError {
		t.Fatalf("invalid evidence result = %#v, want IsError", invalidEvidence)
	}
}

func TestMCPRejectsCrossOriginAndHostileLoopbackHost(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	originRequest := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/mcp", bytes.NewBufferString(`{}`))
	originRequest.Header.Set("Content-Type", "application/json")
	originRequest.Header.Set("Origin", "https://evil.example")
	originResponse := httptest.NewRecorder()
	server.ServeHTTP(originResponse, originRequest)
	if originResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want 403", originResponse.Code)
	}

	hostRequest := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/mcp", bytes.NewBufferString(`{}`))
	hostRequest.Header.Set("Content-Type", "application/json")
	hostRequest.Host = "evil.example"
	hostRequest = hostRequest.WithContext(context.WithValue(hostRequest.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9999}))
	hostResponse := httptest.NewRecorder()
	server.ServeHTTP(hostResponse, hostRequest)
	if hostResponse.Code != http.StatusForbidden {
		t.Fatalf("hostile Host status = %d, want 403", hostResponse.Code)
	}
}

func TestMCPHTTPWriteTimeoutCoversReadiness(t *testing.T) {
	if HTTPWriteTimeout <= MaxReadinessTimeout {
		t.Fatalf("HTTPWriteTimeout = %s, want greater than MaxReadinessTimeout %s", HTTPWriteTimeout, MaxReadinessTimeout)
	}
}

func callMCPTool(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return result
}

func decodeMCPStructured(t *testing.T, result *mcp.CallToolResult, target any) {
	t.Helper()
	if result.IsError {
		t.Fatalf("MCP tool error content = %#v", result.Content)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode structured content %s: %v", string(data), err)
	}
}

func TestMCPErrorTextIsBounded(t *testing.T) {
	err := mcpToolError(fmt.Errorf("%s", strings.Repeat("x", 1000)))
	if len([]rune(err.Error())) > 512 {
		t.Fatalf("error length = %d runes, want <=512", len([]rune(err.Error())))
	}
}
