package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"metriclens/backend/internal/model"
)

type fakeContainerLister struct {
	containers []model.DiscoveredContainer
	err        error
}

type fakeTargetStore struct {
	targets []model.Target
	metrics model.TargetMetricsResponse
	series  []model.Series
	found   bool
	lastErr error
}

func (f fakeContainerLister) ListContainers(context.Context) ([]model.DiscoveredContainer, error) {
	return f.containers, f.err
}

func (f fakeTargetStore) Targets() []model.Target {
	return f.targets
}

func (f fakeTargetStore) TargetMetrics(string) (model.TargetMetricsResponse, bool) {
	return f.metrics, f.found
}

func (f fakeTargetStore) TargetSeries(targetID, metric string, labels map[string]string) []model.Series {
	return f.series
}

func (f fakeTargetStore) LastError() error {
	return f.lastErr
}

func TestHealth(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status body = %q, want ok", body["status"])
	}
}

func TestVersion(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["name"] != "metriclens" {
		t.Fatalf("name = %q, want metriclens", body["name"])
	}
	if body["version"] != Version {
		t.Fatalf("version = %q, want %s", body["version"], Version)
	}
}

func TestConfig(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{
		ScrapeInterval: 5 * time.Second,
		Retention:      15 * time.Minute,
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]int64
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["scrapeIntervalMs"] != 5000 {
		t.Fatalf("scrapeIntervalMs = %d, want 5000", body["scrapeIntervalMs"])
	}
	if body["retentionMs"] != 15*60*1000 {
		t.Fatalf("retentionMs = %d, want %d", body["retentionMs"], 15*60*1000)
	}
}

func TestContainers(t *testing.T) {
	expected := []model.DiscoveredContainer{
		{
			ID:             "abc123",
			Name:           "api-1",
			Image:          "example/api:latest",
			State:          model.ContainerStateRunning,
			ComposeProject: "example",
			ComposeService: "api",
			Networks:       []string{"example_default"},
			ExposedPorts:   []int{8080},
			Labels: map[string]string{
				"com.docker.compose.project": "example",
				"com.docker.compose.service": "api",
			},
		},
	}
	server := NewServer(fakeContainerLister{containers: expected}, fakeTargetStore{}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/containers", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body []model.DiscoveredContainer
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("containers length = %d, want 1", len(body))
	}
	if body[0].ComposeProject != expected[0].ComposeProject {
		t.Fatalf("composeProject = %q, want %q", body[0].ComposeProject, expected[0].ComposeProject)
	}
	if body[0].ComposeService != expected[0].ComposeService {
		t.Fatalf("composeService = %q, want %q", body[0].ComposeService, expected[0].ComposeService)
	}
}

func TestContainersError(t *testing.T) {
	server := NewServer(fakeContainerLister{err: errors.New("docker unavailable")}, fakeTargetStore{}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/containers", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestTargets(t *testing.T) {
	expected := []model.Target{
		{
			ID:            "abc123",
			ServiceName:   "api",
			ContainerName: "api-1",
			URL:           "http://api:8080/metrics",
			Status:        model.TargetStatusUp,
			DiscoveredAt:  "2026-06-06T12:00:00Z",
		},
	}
	server := NewServer(fakeContainerLister{}, fakeTargetStore{targets: expected}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body []model.Target
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("targets length = %d, want 1", len(body))
	}
	if body[0].URL != expected[0].URL {
		t.Fatalf("url = %q, want %q", body[0].URL, expected[0].URL)
	}
	if body[0].Status != model.TargetStatusUp {
		t.Fatalf("status = %q, want up", body[0].Status)
	}
}

func TestTargetsStoreError(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{lastErr: errors.New("docker unavailable")}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestTargetMetrics(t *testing.T) {
	expected := model.TargetMetricsResponse{
		Target: model.Target{
			ID:            "abc123",
			ServiceName:   "api",
			ContainerName: "api-1",
			URL:           "http://api:8080/metrics",
			Status:        model.TargetStatusUp,
			DiscoveredAt:  "2026-06-06T12:00:00Z",
		},
		Families: []model.MetricFamily{
			{
				Name: "up",
				Type: model.MetricTypeGauge,
				Samples: []model.MetricSample{
					{Metric: "up", Labels: map[string]string{}, Value: 1},
				},
			},
		},
	}
	server := NewServer(fakeContainerLister{}, fakeTargetStore{metrics: expected, found: true}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/abc123/metrics", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body model.TargetMetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Target.ID != expected.Target.ID {
		t.Fatalf("target id = %q, want %q", body.Target.ID, expected.Target.ID)
	}
	if len(body.Families) != 1 {
		t.Fatalf("families length = %d, want 1", len(body.Families))
	}
}

func TestTargetMetricsNotFound(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/missing/metrics", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestTargetSeries(t *testing.T) {
	expected := []model.Series{
		{
			TargetID: "abc123",
			Metric:   "up",
			Labels:   map[string]string{},
			Points: []model.SeriesPoint{
				{TS: "2026-06-06T12:00:00Z", Value: 1},
			},
		},
	}
	server := NewServer(fakeContainerLister{}, fakeTargetStore{series: expected}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/abc123/series?metric=up", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body []model.Series
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("series length = %d, want 1", len(body))
	}
	if body[0].Metric != "up" {
		t.Fatalf("metric = %q, want up", body[0].Metric)
	}
}

func TestTargetSeriesRequiresMetric(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/abc123/series", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestTargetSeriesRejectsBadLabels(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/abc123/series?metric=up&labels=nope", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestDecodeLabels(t *testing.T) {
	labels, err := decodeLabels(`{"method":"GET","status":"200"}`)
	if err != nil {
		t.Fatalf("decodeLabels() error = %v", err)
	}
	if labels["method"] != "GET" || labels["status"] != "200" {
		t.Fatalf("labels = %#v, want decoded labels", labels)
	}
}

func TestDecodeLabelsRejectsInvalidJSON(t *testing.T) {
	if _, err := decodeLabels("nope"); err == nil {
		t.Fatal("decodeLabels() error = nil, want error")
	}
}

func TestTargetPanels(t *testing.T) {
	metrics := model.TargetMetricsResponse{
		Target: model.Target{ID: "abc123"},
		Families: []model.MetricFamily{
			{
				Name: "http_requests_total",
				Type: model.MetricTypeCounter,
				Samples: []model.MetricSample{
					{
						Metric: "http_requests_total",
						Labels: map[string]string{"method": "GET", "route": "/users", "status": "200"},
						Value:  10,
					},
				},
			},
		},
	}
	server := NewServer(fakeContainerLister{}, fakeTargetStore{metrics: metrics, found: true}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/abc123/panels", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body []model.SuggestedPanel
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("panels length = 0, want suggestions")
	}
	if body[0].Reason == "" {
		t.Fatal("first panel reason is empty")
	}
}

func TestTargetPanelsNotFound(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/missing/panels", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestTargetQuality(t *testing.T) {
	metrics := model.TargetMetricsResponse{
		Target: model.Target{ID: "abc123"},
		Families: []model.MetricFamily{
			{
				Name: "custom_metric",
				Type: model.MetricTypeUntyped,
			},
		},
	}
	server := NewServer(fakeContainerLister{}, fakeTargetStore{metrics: metrics, found: true}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/abc123/quality", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body []model.MetricQualityIssue
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("quality issues length = 0, want issues")
	}
	if body[0].Metric != "custom_metric" {
		t.Fatalf("metric = %q, want custom_metric", body[0].Metric)
	}
}

func TestTargetQualityNotFound(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/missing/quality", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestUnknownAPIPathReturnsJSONNotFound(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/missing", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("content-type = %q, want application/json", contentType)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "api endpoint not found" {
		t.Fatalf("error = %q, want api endpoint not found", body["error"])
	}
}
