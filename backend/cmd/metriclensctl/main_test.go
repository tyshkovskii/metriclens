package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestCommandsUseRESTSurfaceAndCompactJSON(t *testing.T) {
	type requestRecord struct {
		method string
		path   string
		query  string
		body   string
	}
	var (
		mu       sync.Mutex
		requests []requestRecord
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		mu.Lock()
		requests = append(requests, requestRecord{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: string(body)})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		responses := map[string]string{
			"/api/capabilities":               `{"name":"metriclens", "scrapeIntervalMs":5000}`,
			"/api/readiness":                  `{"ready":true,"services":[]}`,
			"/api/report":                     `{"status":"ok","findings":[]}`,
			"/api/markers":                    `{"id":"marker-1","createdAt":"2026-01-01T00:00:00Z"}`,
			"/api/compare":                    `{"status":"ok","findings":[]}`,
			"/api/targets/api-1/series/batch": `[{"metric":"up","points":[]}]`,
		}
		response, ok := responses[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, err := io.WriteString(w, response); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	cases := []struct {
		name string
		args []string
	}{
		{name: "capabilities", args: []string{"--url", server.URL, "capabilities"}},
		{name: "wait", args: []string{"--url", server.URL, "wait", "--services", "api,worker", "--timeout", "0s"}},
		{name: "observe", args: []string{"--url", server.URL, "observe", "--window", "2m", "--limit", "3", "--severity", "warning,error", "--services", "api", "--changed-only"}},
		{name: "start", args: []string{"--url", server.URL, "start", "--name", "checkout", "--client-run-id", "run-1"}},
		{name: "compare", args: []string{"--url", server.URL, "compare", "--from", "marker-1", "--to", "marker-2", "--limit", "3", "--severity", "error", "--services", "api", "--changed-only"}},
		{name: "evidence", args: []string{"--url", server.URL, "evidence", "--target", "api-1", "--metric", "up,requests_total", "--start", "2026-01-01T00:00:00Z", "--end", "2026-01-01T00:01:00Z", "--max-points", "5"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(testCase.args, &stdout, &stderr); code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			if strings.Contains(stdout.String(), " ") {
				t.Fatalf("stdout = %q, want compact JSON", stdout.String())
			}
			var value any
			if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
				t.Fatalf("decode stdout %q: %v", stdout.String(), err)
			}
		})
	}

	mu.Lock()
	got := append([]requestRecord(nil), requests...)
	mu.Unlock()
	if len(got) != len(cases) {
		t.Fatalf("request count = %d, want %d", len(got), len(cases))
	}
	if got[0].method != http.MethodGet || got[0].path != "/api/capabilities" {
		t.Fatalf("capabilities request = %#v", got[0])
	}
	if got[1].path != "/api/readiness" || got[1].query != "service=api&service=worker&timeout=0s" {
		t.Fatalf("readiness request = %#v", got[1])
	}
	if got[2].path != "/api/report" || !strings.Contains(got[2].query, "changedOnly=true") || !strings.Contains(got[2].query, "severity=warning") || !strings.Contains(got[2].query, "severity=error") {
		t.Fatalf("observe request = %#v", got[2])
	}
	if got[3].method != http.MethodPost || got[3].path != "/api/markers" || got[3].body != `{"clientRunId":"run-1","name":"checkout"}` {
		t.Fatalf("start request = %#v", got[3])
	}
	if got[4].path != "/api/compare" || !strings.Contains(got[4].query, "from=marker-1") || !strings.Contains(got[4].query, "to=marker-2") {
		t.Fatalf("compare request = %#v", got[4])
	}
	if got[5].path != "/api/targets/api-1/series/batch" || !strings.Contains(got[5].query, "metrics=up%2Crequests_total") || !strings.Contains(got[5].query, "maxPoints=5") {
		t.Fatalf("evidence request = %#v", got[5])
	}
}

func TestWait408PrintsReadinessAndReturnsEvaluationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestTimeout)
		if _, err := io.WriteString(w, `{"ready":false,"services":[{"service":"api"}]}`); err != nil {
			t.Errorf("write readiness: %v", err)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--url", server.URL, "wait", "--services", "api", "--timeout", "1s"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr = %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != `{"ready":false,"services":[{"service":"api"}]}` {
		t.Fatalf("stdout = %q, want compact readiness JSON", got)
	}
}

func TestCLIRejectsUsageURLAPIJSONAndOversizeErrors(t *testing.T) {
	validServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/capabilities":
			w.WriteHeader(http.StatusInternalServerError)
			if _, err := io.WriteString(w, `{"error":"bad request"}`); err != nil {
				t.Errorf("write error response: %v", err)
			}
		case "/api/report":
			if _, err := io.WriteString(w, "not-json"); err != nil {
				t.Errorf("write invalid response: %v", err)
			}
		default:
			if _, err := io.WriteString(w, strings.Repeat("x", maxResponseBytes+1)); err != nil {
				t.Errorf("write oversize response: %v", err)
			}
		}
	}))
	defer validServer.Close()

	tests := []struct {
		name string
		args []string
	}{
		{name: "missing command", args: []string{}},
		{name: "invalid url", args: []string{"--url", "ftp://localhost", "capabilities"}},
		{name: "api error", args: []string{"--url", validServer.URL, "capabilities"}},
		{name: "invalid json", args: []string{"--url", validServer.URL, "observe"}},
		{name: "oversize", args: []string{"--url", validServer.URL, "evidence", "--target", "target", "--metric", "up"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(testCase.args, &stdout, &stderr); code != 1 {
				t.Fatalf("exit code = %d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if stderr.Len() == 0 {
				t.Fatal("stderr is empty, want bounded diagnostic")
			}
		})
	}
}

func TestEvaluateCallOrderCountsAndSignalScores(t *testing.T) {
	var (
		mu       sync.Mutex
		calls    []string
		markerID = "marker-eval"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/capabilities":
			writeTestJSON(t, w, `{"scrapeIntervalMs":5000}`)
		case "/api/readiness":
			if r.URL.Query().Get("service") != "api" || r.URL.Query().Get("timeout") != "2s" {
				t.Errorf("readiness query = %s", r.URL.RawQuery)
			}
			writeTestJSON(t, w, `{"ready":true}`)
		case "/api/markers":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read marker body: %v", err)
			}
			if string(body) != `{"clientRunId":"metriclensctl","name":"metriclensctl evaluate"}` {
				t.Errorf("marker body = %s", body)
			}
			writeTestJSON(t, w, `{"id":"marker-eval"}`)
		case "/api/compare":
			if r.URL.Query().Get("from") != markerID || r.URL.Query().Get("severity") != "error" {
				t.Errorf("compare query = %s", r.URL.RawQuery)
			}
			writeTestJSON(t, w, `{"findings":[{"signal":"z"},{"signal":"a"},{"signal":"a"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("METRICLENSCTL_WORKLOAD", "1")
	var stdout, stderr bytes.Buffer
	args := []string{"--url", server.URL, "evaluate", "--services", "api", "--expect", "z,a,a", "--severity", "error", "--timeout", "2s", "--settle", "0", "--min-f1", "1", "--", os.Args[0], "-test.run=TestMetricLensctlWorkloadHelper"}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var result evaluationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode evaluation output %q: %v", stdout.String(), err)
	}
	if result.DiscoveryCalls != 1 || result.ToolCalls != 3 || result.ResponseBytes != responseBytesForTest() || result.EstimatedResponseTokens != (result.ResponseBytes+3)/4 {
		t.Fatalf("counts = %#v, want discovery=1 tools=3 bytes=%d", result, responseBytesForTest())
	}
	if !reflect.DeepEqual(result.ExpectedSignals, []string{"a", "z"}) || !reflect.DeepEqual(result.ActualSignals, []string{"a", "z"}) || !reflect.DeepEqual(result.MatchedSignals, []string{"a", "z"}) || len(result.FalsePositiveSignals) != 0 || len(result.FalseNegativeSignals) != 0 {
		t.Fatalf("signal sets = %#v", result)
	}
	if result.Precision != 1 || result.Recall != 1 || result.SignalF1 != 1 || result.WorkloadExitCode != 0 || !result.Passed {
		t.Fatalf("scores = %#v, want perfect pass", result)
	}
	if !strings.Contains(stderr.String(), "workload stdout") || !strings.Contains(stderr.String(), "workload stderr") {
		t.Fatalf("workload output = %q, want stderr only", stderr.String())
	}
	mu.Lock()
	gotCalls := append([]string(nil), calls...)
	mu.Unlock()
	if !reflect.DeepEqual(gotCalls, []string{"GET /api/capabilities", "GET /api/readiness", "POST /api/markers", "GET /api/compare"}) {
		t.Fatalf("call order = %#v", gotCalls)
	}
}

func TestEvaluateFailureReturnsTwoAndStillCompares(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/capabilities":
			writeTestJSON(t, w, `{"scrapeIntervalMs":0}`)
		case "/api/readiness":
			writeTestJSON(t, w, `{"ready":true}`)
		case "/api/markers":
			writeTestJSON(t, w, `{"id":"marker-fail"}`)
		case "/api/compare":
			if got := r.URL.Query()["severity"]; !reflect.DeepEqual(got, []string{"warning", "error"}) {
				t.Errorf("default compare severity = %#v, want warning,error", got)
			}
			writeTestJSON(t, w, `{"findings":[],"omitted":1}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("METRICLENSCTL_WORKLOAD_EXIT", "7")
	t.Setenv("METRICLENSCTL_WORKLOAD", "1")
	var stdout, stderr bytes.Buffer
	args := []string{"--url", server.URL, "evaluate", "--services", "api", "--expect", "missing_signal", "--settle", "0", "--", os.Args[0], "-test.run=TestMetricLensctlWorkloadHelper"}
	if code := run(args, &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var result evaluationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode evaluation output: %v", err)
	}
	if result.WorkloadExitCode != 7 || result.OmittedFindings != 1 || result.Passed || result.SignalF1 != 0 {
		t.Fatalf("failure result = %#v", result)
	}
	if !reflect.DeepEqual(calls, []string{"/api/capabilities", "/api/readiness", "/api/markers", "/api/compare"}) {
		t.Fatalf("calls = %#v, want compare after failed workload", calls)
	}
}

func TestMetricLensctlWorkloadHelper(t *testing.T) {
	if os.Getenv("METRICLENSCTL_WORKLOAD") != "1" {
		return
	}
	if _, err := fmt.Fprintln(os.Stdout, "workload stdout"); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(os.Stderr, "workload stderr"); err != nil {
		t.Fatal(err)
	}
	if raw := os.Getenv("METRICLENSCTL_WORKLOAD_EXIT"); raw != "" {
		code, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatal(err)
		}
		os.Exit(code)
	}
	os.Exit(0)
}

func TestDockerfileBuildsAndCopiesCLI(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	dockerfile := string(data)
	for _, phrase := range []string{
		"go build -trimpath -ldflags=\"-s -w\" -o /out/metriclensctl ./cmd/metriclensctl",
		"COPY --from=build /out/metriclensctl /usr/local/bin/metriclensctl",
		"ENTRYPOINT [\"/usr/local/bin/metriclens\"]",
	} {
		if !strings.Contains(dockerfile, phrase) {
			t.Errorf("Dockerfile missing %q", phrase)
		}
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value string) {
	t.Helper()
	if _, err := io.WriteString(w, value); err != nil {
		t.Errorf("write JSON: %v", err)
	}
}

func responseBytesForTest() int64 {
	return int64(len(`{"scrapeIntervalMs":5000}`) + len(`{"ready":true}`) + len(`{"id":"marker-eval"}`) + len(`{"findings":[{"signal":"z"},{"signal":"a"},{"signal":"a"}]}`))
}
