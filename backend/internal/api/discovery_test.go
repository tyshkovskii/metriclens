package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"metriclens/backend/internal/diagnosis"
)

func TestOpenAPIAndLLMsDiscoveryDocuments(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	openAPIResponse := httptest.NewRecorder()
	server.ServeHTTP(openAPIResponse, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/openapi.json", nil))
	if openAPIResponse.Code != http.StatusOK || openAPIResponse.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("OpenAPI response = status %d content type %q, want 200/application/json", openAPIResponse.Code, openAPIResponse.Header().Get("Content-Type"))
	}
	var document struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
		Paths      map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(openAPIResponse.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}
	if !strings.HasPrefix(document.OpenAPI, "3.1.") {
		t.Fatalf("openapi = %q, want 3.1.x", document.OpenAPI)
	}
	if document.Info.Version != Version {
		t.Fatalf("OpenAPI version = %q, want API version %q", document.Info.Version, Version)
	}
	expectedPaths := []string{
		"/openapi.json", "/llms.txt", "/mcp", "/api/health", "/api/version", "/api/config", "/api/capabilities",
		"/api/report", "/api/readiness", "/api/markers", "/api/compare", "/api/containers", "/api/targets",
		"/api/targets/{targetId}/metrics", "/api/targets/{targetId}/series", "/api/targets/{targetId}/series/batch",
		"/api/targets/{targetId}/panels", "/api/targets/{targetId}/quality",
	}
	if len(document.Paths) != len(expectedPaths) {
		t.Fatalf("OpenAPI path count = %d, want exact inventory count %d", len(document.Paths), len(expectedPaths))
	}
	for _, path := range expectedPaths {
		if _, ok := document.Paths[path]; !ok {
			t.Errorf("OpenAPI paths missing %s", path)
		}
	}
	for _, schema := range []string{"Error", "Capabilities", "Report", "Finding", "Evidence", "Readiness", "Target", "Series"} {
		if _, ok := document.Components.Schemas[schema]; !ok {
			t.Errorf("OpenAPI schemas missing %s", schema)
		}
	}
	batch := map[string]any{}
	if err := json.Unmarshal(document.Paths["/api/targets/{targetId}/series/batch"], &batch); err != nil {
		t.Fatalf("decode batch path: %v", err)
	}
	batchGet, ok := batch["get"].(map[string]any)
	if !ok {
		t.Fatal("batch path has no GET operation")
	}
	parameters, ok := batchGet["parameters"].([]any)
	if !ok {
		t.Fatal("batch GET has no parameters")
	}
	parameterNames := map[string]bool{}
	for _, parameter := range parameters {
		if item, itemOK := parameter.(map[string]any); itemOK {
			if name, nameOK := item["name"].(string); nameOK {
				parameterNames[name] = true
			}
			if ref, refOK := item["$ref"].(string); refOK && ref == "#/components/parameters/TargetId" {
				parameterNames["targetId"] = true
			}
		}
	}
	for _, name := range []string{"targetId", "metrics", "metric", "start", "end", "at", "maxPoints"} {
		if !parameterNames[name] {
			t.Errorf("batch parameters missing %s", name)
		}
	}
	readiness := map[string]any{}
	if err := json.Unmarshal(document.Paths["/api/readiness"], &readiness); err != nil {
		t.Fatalf("decode readiness path: %v", err)
	}
	readinessGet, ok := readiness["get"].(map[string]any)
	if !ok {
		t.Fatal("readiness path has no GET operation")
	}
	responses, ok := readinessGet["responses"].(map[string]any)
	if !ok {
		t.Fatal("readiness GET has no responses")
	}
	if _, ok := responses["408"]; !ok {
		t.Fatal("readiness OpenAPI response missing 408 timeout")
	}

	llmsResponse := httptest.NewRecorder()
	server.ServeHTTP(llmsResponse, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/llms.txt", nil))
	llms := llmsResponse.Body.String()
	if llmsResponse.Code != http.StatusOK || llmsResponse.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("llms response = status %d content type %q, want 200/text/plain", llmsResponse.Code, llmsResponse.Header().Get("Content-Type"))
	}
	if len(llms) >= 2*1024 || !strings.HasPrefix(llms, "# MetricLens") {
		t.Fatalf("llms length=%d/header=%q, want concise llms.txt", len(llms), llms[:minStringLen(len(llms), 32)])
	}
	for _, phrase := range []string{"/api/readiness", "/mcp", "observe_stack", "named marker", "/api/compare", "evidence", "does not start Docker", "logs or traces", "retention"} {
		if !strings.Contains(strings.ToLower(llms), strings.ToLower(phrase)) {
			t.Errorf("llms.txt missing %q", phrase)
		}
	}
}

func TestCapabilitiesAreDeterministicAndUseSharedLimits(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{ScrapeInterval: 5 * time.Second, Retention: 2 * time.Minute})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/capabilities", nil)
	firstResponse := httptest.NewRecorder()
	server.ServeHTTP(firstResponse, request)
	secondResponse := httptest.NewRecorder()
	server.ServeHTTP(secondResponse, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/capabilities", nil))
	if firstResponse.Code != http.StatusOK || firstResponse.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("capabilities response = status %d content type %q, want 200/application/json", firstResponse.Code, firstResponse.Header().Get("Content-Type"))
	}
	if firstResponse.Body.String() != secondResponse.Body.String() {
		t.Fatalf("capabilities body changed between identical requests: %s vs %s", firstResponse.Body.String(), secondResponse.Body.String())
	}
	var body capabilitiesResponse
	if err := json.Unmarshal(firstResponse.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if body.Name != "metriclens" || body.Version != Version || body.ScrapeIntervalMs != 5000 || body.RetentionMs != 120000 {
		t.Fatalf("capabilities identity/config = %#v, want current values", body)
	}
	if !sort.StringsAreSorted(body.Features) || len(body.Features) == 0 {
		t.Fatalf("features = %#v, want sorted implemented features", body.Features)
	}
	hasMCPTools := false
	for _, feature := range body.Features {
		if feature == "mcp-tools" {
			hasMCPTools = true
		}
	}
	if !hasMCPTools {
		t.Fatalf("features = %#v, want mcp-tools discovery feature", body.Features)
	}
	if body.Limits.ReportFindings != diagnosis.MaxLimit || body.Limits.MarkerFieldChars != MaxMarkerFieldRunes || body.Limits.ReadinessTimeoutMs != MaxReadinessTimeout.Milliseconds() || body.Limits.BatchMetrics != MaxBatchMetrics || body.Limits.BatchSeriesPerMetric != MaxBatchSeriesPerMetric || body.Limits.BatchSeriesTotal != MaxBatchSeriesTotal || body.Limits.BatchPointsPerSeries != MaxBatchPointsPerSeries || body.Limits.BatchPointsTotal != MaxBatchPointsTotal || body.Limits.BatchDefaultMaxPoints != DefaultBatchMaxPoints {
		t.Fatalf("capability limits = %#v, want shared constants", body.Limits)
	}
	if body.Links.OpenAPI != "/openapi.json" || body.Links.LLMs != "/llms.txt" || body.Links.MCP != "/mcp" {
		t.Fatalf("links = %#v, want discovery URLs", body.Links)
	}
}

func minStringLen(length, maximum int) int {
	if length < maximum {
		return length
	}
	return maximum
}
