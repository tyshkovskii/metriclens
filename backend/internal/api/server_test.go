package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"metriclens/backend/internal/diagnosis"
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

type fakeBatchTargetStore struct {
	fakeTargetStore
	batch      []model.Series
	gotMetrics []string
	gotStart   *time.Time
	gotEnd     *time.Time
	gotAt      *time.Time
}

type fakeIntervalTargetStore struct {
	fakeTargetStore
	interval time.Duration
	err      error
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

func (f *fakeBatchTargetStore) TargetSeriesBatch(_ string, metrics []string, start, end, at *time.Time) []model.Series {
	f.gotMetrics = append([]string(nil), metrics...)
	f.gotStart = start
	f.gotEnd = end
	f.gotAt = at
	return f.batch
}

func (f *fakeIntervalTargetStore) SetInterval(interval time.Duration) error {
	if f.err != nil {
		return f.err
	}
	f.interval = interval
	return nil
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
	if Version != "0.3.0" || body["version"] != Version {
		t.Fatalf("version = %q (constant %q), want 0.3.0", body["version"], Version)
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

func TestConfigUpdate(t *testing.T) {
	store := &fakeIntervalTargetStore{}
	server := NewServer(fakeContainerLister{}, store, Config{
		ScrapeInterval: 5 * time.Second,
		Retention:      15 * time.Minute,
	})
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPut,
		"/api/config",
		strings.NewReader(`{"scrapeIntervalMs":10000}`),
	)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if store.interval != 10*time.Second {
		t.Fatalf("interval = %s, want 10s", store.interval)
	}

	get := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/config", nil)
	getRec := httptest.NewRecorder()
	server.ServeHTTP(getRec, get)
	var body map[string]int64
	if err := json.NewDecoder(getRec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["scrapeIntervalMs"] != 10_000 {
		t.Fatalf("scrapeIntervalMs = %d, want 10000", body["scrapeIntervalMs"])
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

func TestTargetSeriesBatch(t *testing.T) {
	point := model.SeriesPoint{TS: "2026-06-06T12:00:00Z", Value: 2}
	store := &fakeBatchTargetStore{
		fakeTargetStore: fakeTargetStore{},
		batch:           []model.Series{{TargetID: "abc123", Metric: "a", Labels: map[string]string{}, Points: []model.SeriesPoint{point}}},
	}
	server := NewServer(fakeContainerLister{}, store, Config{})
	start := "2026-06-06T11:00:00Z"
	end := "2026-06-06T13:00:00Z"
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/abc123/series/batch?metrics=b,a&metrics=a&start="+start+"&end="+end, nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(store.gotMetrics) != 2 || store.gotMetrics[0] != "a" || store.gotMetrics[1] != "b" {
		t.Fatalf("metrics = %#v, want sorted unique metrics", store.gotMetrics)
	}
	if store.gotStart == nil || store.gotEnd == nil || store.gotAt != nil {
		t.Fatalf("bounds = start %v end %v at %v, want start/end only", store.gotStart, store.gotEnd, store.gotAt)
	}
	var body []model.Series
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 || body[0].Metric != "a" {
		t.Fatalf("body = %#v, want batch result", body)
	}
}

func TestTargetSeriesBatchAtAndRangeAreContradictory(t *testing.T) {
	server := NewServer(fakeContainerLister{}, &fakeBatchTargetStore{}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/abc123/series/batch?metrics=up&at=2026-06-06T12:00:00Z&start=2026-06-06T11:00:00Z", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "at cannot be combined with start or end" {
		t.Fatalf("error = %q, want contradiction message", body["error"])
	}
}

func TestTargetSeriesBatchRejectsInvalidBounds(t *testing.T) {
	server := NewServer(fakeContainerLister{}, &fakeBatchTargetStore{}, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/abc123/series/batch?metrics=up&start=nope", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestTargetSeriesBatchRejectsTooManyMetricsAndInvalidMaxPoints(t *testing.T) {
	server := NewServer(fakeContainerLister{}, &fakeBatchTargetStore{}, Config{})
	tooMany := "/api/targets/abc123/series/batch?metrics=" + strings.Join([]string{
		"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k",
	}, ",")
	for _, path := range []string{tooMany, "/api/targets/abc123/series/batch?metrics=up&maxPoints=0"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("path %q status = %d, want %d", path, rec.Code, http.StatusBadRequest)
		}
	}
}

func TestTargetSeriesBatchCapsSeriesAndPointsWithHeaders(t *testing.T) {
	series := make([]model.Series, 0, MaxBatchSeriesTotal+10)
	for index := 0; index < MaxBatchSeriesTotal+10; index++ {
		series = append(series, model.Series{
			TargetID: "abc123",
			Metric:   "up",
			Labels:   map[string]string{"instance": fmt.Sprintf("%02d", index)},
			Points: []model.SeriesPoint{
				{TS: "2026-06-06T12:00:00Z", Value: float64(index)},
			},
		})
	}
	store := &fakeBatchTargetStore{fakeTargetStore: fakeTargetStore{}, batch: series}
	server := NewServer(fakeContainerLister{}, store, Config{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/abc123/series/batch?metrics=up", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body []model.Series
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != MaxBatchSeriesPerMetric || rec.Header().Get("X-MetricLens-Series-Truncated") != "true" {
		t.Fatalf("series = %d truncated = %q, want %d and true", len(body), rec.Header().Get("X-MetricLens-Series-Truncated"), MaxBatchSeriesPerMetric)
	}
	if rec.Header().Get("X-MetricLens-Series-Count") != fmt.Sprint(MaxBatchSeriesPerMetric) || rec.Header().Get("X-MetricLens-Point-Count") != fmt.Sprint(MaxBatchSeriesPerMetric) {
		t.Fatalf("counts = series %q points %q, want %d each", rec.Header().Get("X-MetricLens-Series-Count"), rec.Header().Get("X-MetricLens-Point-Count"), MaxBatchSeriesPerMetric)
	}
	if rec.Header().Get("X-MetricLens-Points-Truncated") != "false" {
		t.Fatalf("points truncated = %q, want false", rec.Header().Get("X-MetricLens-Points-Truncated"))
	}
}

func TestTargetSeriesBatchDownsamplesAndPreservesEndpoints(t *testing.T) {
	points := make([]model.SeriesPoint, 0, 10)
	for index := 0; index < 10; index++ {
		points = append(points, model.SeriesPoint{TS: fmt.Sprintf("2026-06-06T12:00:%02dZ", index), Value: float64(index)})
	}
	limited, stats := limitBatchSeries([]model.Series{{Metric: "up", Points: points}}, 4)
	if len(limited) != 1 || len(limited[0].Points) != 4 {
		t.Fatalf("limited = %#v, stats = %#v, want one series with four points", limited, stats)
	}
	got := limited[0].Points
	if got[0].Value != 0 || got[len(got)-1].Value != 9 || !stats.pointsTruncated {
		t.Fatalf("points = %#v stats = %#v, want first 0 last 9 and truncation", got, stats)
	}
	latest, _ := limitBatchSeries([]model.Series{{Metric: "up", Points: points}}, 1)
	if len(latest[0].Points) != 1 || latest[0].Points[0].Value != 9 {
		t.Fatalf("maxPoints=1 points = %#v, want latest point", latest[0].Points)
	}
}

func TestBatchLimitsEnforceGlobalSeriesAndPointCaps(t *testing.T) {
	series := make([]model.Series, 0, MaxBatchSeriesTotal+5)
	for index := 0; index < MaxBatchSeriesTotal+5; index++ {
		series = append(series, model.Series{Metric: fmt.Sprintf("metric_%02d", index), Points: []model.SeriesPoint{{TS: "2026-06-06T12:00:00Z", Value: float64(index)}}})
	}
	limited, stats := limitBatchSeries(series, MaxBatchPointsPerSeries)
	if len(limited) != MaxBatchSeriesTotal || stats.seriesCount != MaxBatchSeriesTotal {
		t.Fatalf("series count = %d/%d, want %d", len(limited), stats.seriesCount, MaxBatchSeriesTotal)
	}
	if stats.pointCount != MaxBatchSeriesTotal || !stats.seriesTruncated || stats.pointsTruncated {
		t.Fatalf("stats = %#v, want only global series truncation", stats)
	}

	pointSeries := make([]model.Series, 0, 3)
	for index := 0; index < 3; index++ {
		points := make([]model.SeriesPoint, 0, MaxBatchPointsPerSeries+1)
		for pointIndex := 0; pointIndex < MaxBatchPointsPerSeries+1; pointIndex++ {
			points = append(points, model.SeriesPoint{TS: fmt.Sprintf("2026-06-06T12:00:%02dZ", pointIndex%60), Value: float64(pointIndex)})
		}
		pointSeries = append(pointSeries, model.Series{Metric: fmt.Sprintf("point_metric_%02d", index), Points: points})
	}
	pointLimited, pointStats := limitBatchSeries(pointSeries, MaxBatchPointsPerSeries)
	if pointStats.pointCount != MaxBatchPointsTotal || !pointStats.pointsTruncated {
		t.Fatalf("point stats = %#v, want global point truncation at %d", pointStats, MaxBatchPointsTotal)
	}
	if len(pointLimited[0].Points) != MaxBatchPointsPerSeries || len(pointLimited[len(pointLimited)-1].Points) != MaxBatchPointsTotal-2*MaxBatchPointsPerSeries {
		t.Fatalf("first/last point counts = %d/%d, want %d/%d after global cap", len(pointLimited[0].Points), len(pointLimited[len(pointLimited)-1].Points), MaxBatchPointsPerSeries, MaxBatchPointsTotal-2*MaxBatchPointsPerSeries)
	}
}

func TestParseBatchMaxPointsCapsConfiguredMaximum(t *testing.T) {
	if got, err := parseBatchMaxPoints("999"); err != nil || got != MaxBatchPointsPerSeries {
		t.Fatalf("maxPoints=999 = %d, %v, want capped %d", got, err, MaxBatchPointsPerSeries)
	}
}

func TestTargetSeriesBatchNativeAndFallbackHaveIdenticalLimits(t *testing.T) {
	series := []model.Series{{
		TargetID: "abc123",
		Metric:   "up",
		Labels:   map[string]string{"instance": "one"},
		Points: []model.SeriesPoint{
			{TS: "2026-06-06T12:00:00Z", Value: 0},
			{TS: "2026-06-06T12:00:01Z", Value: 1},
			{TS: "2026-06-06T12:00:02Z", Value: 2},
		},
	}}
	native := NewServer(fakeContainerLister{}, &fakeBatchTargetStore{fakeTargetStore: fakeTargetStore{}, batch: series}, Config{})
	fallback := NewServer(fakeContainerLister{}, fakeTargetStore{series: series}, Config{})

	nativeRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/abc123/series/batch?metrics=up&maxPoints=2", nil)
	nativeResponse := httptest.NewRecorder()
	native.ServeHTTP(nativeResponse, nativeRequest)
	fallbackRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/targets/abc123/series/batch?metrics=up&maxPoints=2", nil)
	fallbackResponse := httptest.NewRecorder()
	fallback.ServeHTTP(fallbackResponse, fallbackRequest)
	if nativeResponse.Code != http.StatusOK || fallbackResponse.Code != http.StatusOK {
		t.Fatalf("statuses = %d and %d, want 200", nativeResponse.Code, fallbackResponse.Code)
	}
	if nativeResponse.Body.String() != fallbackResponse.Body.String() {
		t.Fatalf("native body = %s, fallback body = %s, want parity", nativeResponse.Body.String(), fallbackResponse.Body.String())
	}
	for _, header := range []string{"X-MetricLens-Series-Count", "X-MetricLens-Point-Count", "X-MetricLens-Series-Truncated", "X-MetricLens-Points-Truncated"} {
		if nativeResponse.Header().Get(header) != fallbackResponse.Header().Get(header) {
			t.Fatalf("header %s = %q and %q, want parity", header, nativeResponse.Header().Get(header), fallbackResponse.Header().Get(header))
		}
	}
	var body []model.Series
	if err := json.NewDecoder(nativeResponse.Body).Decode(&body); err != nil {
		t.Fatalf("decode native body: %v", err)
	}
	if len(body) != 1 || len(body[0].Points) != 2 || body[0].Points[0].Value != 0 || body[0].Points[1].Value != 2 {
		t.Fatalf("body = %#v, want endpoints after limiting", body)
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

func TestReport(t *testing.T) {
	target := model.Target{ID: "down-1", ServiceName: "api", Status: model.TargetStatusDown, LastError: "connection refused"}
	server := NewServer(fakeContainerLister{}, fakeTargetStore{targets: []model.Target{target}}, Config{Retention: 5 * time.Minute})
	server.now = func() time.Time { return time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC) }
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/report?window=2m&limit=1", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body diagnosis.Report
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "error" || body.Targets.Down != 1 || len(body.Findings) != 1 {
		t.Fatalf("report = %#v, want one down-target error", body)
	}
	if body.Window.DurationMs != 2*60*1000 {
		t.Fatalf("duration = %d, want 120000", body.Window.DurationMs)
	}
}

func TestReportRejectsWindowBeyondRetention(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{Retention: time.Minute})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/report?window=2m", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "window query parameter must not exceed retention" {
		t.Fatalf("error = %q, want retention validation", body["error"])
	}
}

func TestReportRejectsInvalidFilters(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	for _, path := range []string{
		"/api/report?severity=debug",
		"/api/report?severity=info,,error",
		"/api/report?service=",
		"/api/report?changedOnly=maybe",
	} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("path %q status = %d, want %d", path, rec.Code, http.StatusBadRequest)
		}
	}
}

func TestMarkersAndCompare(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{Retention: 10 * time.Minute})
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	server.now = func() time.Time { return now }

	first := createMarker(t, server)
	now = now.Add(time.Minute)
	second := createMarker(t, server)

	listRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/markers", nil)
	listResponse := httptest.NewRecorder()
	server.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listResponse.Code, http.StatusOK)
	}
	var markers []Marker
	if err := json.NewDecoder(listResponse.Body).Decode(&markers); err != nil {
		t.Fatalf("decode markers: %v", err)
	}
	if len(markers) != 2 || markers[0].ID != first.ID || markers[1].ID != second.ID {
		t.Fatalf("markers = %#v, want creation order", markers)
	}

	compareRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/compare?from="+first.ID+"&to="+second.ID+"&limit=1", nil)
	compareResponse := httptest.NewRecorder()
	server.ServeHTTP(compareResponse, compareRequest)
	if compareResponse.Code != http.StatusOK {
		t.Fatalf("compare status = %d, want %d", compareResponse.Code, http.StatusOK)
	}
	var report diagnosis.Report
	if err := json.NewDecoder(compareResponse.Body).Decode(&report); err != nil {
		t.Fatalf("decode compare report: %v", err)
	}
	if report.Window.Start != first.CreatedAt || report.Window.End != second.CreatedAt {
		t.Fatalf("window = %#v, want marker timestamps", report.Window)
	}
	if report.From == nil || report.From.ID != first.ID || report.To == nil || report.To.ID != second.ID {
		t.Fatalf("marker refs = from %#v to %#v, want compare markers", report.From, report.To)
	}
}

func TestMarkersAcceptOptionalNamesAndClientRunID(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/markers", strings.NewReader(`{"name":"checkout test","clientRunId":"run-42"}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}
	var marker Marker
	if err := json.NewDecoder(response.Body).Decode(&marker); err != nil {
		t.Fatalf("decode marker: %v", err)
	}
	if marker.Name != "checkout test" || marker.ClientRunID != "run-42" {
		t.Fatalf("marker = %#v, want named marker", marker)
	}

	listRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/markers", nil)
	listResponse := httptest.NewRecorder()
	server.ServeHTTP(listResponse, listRequest)
	var markers []Marker
	if err := json.NewDecoder(listResponse.Body).Decode(&markers); err != nil {
		t.Fatalf("decode marker list: %v", err)
	}
	if len(markers) != 1 || markers[0].Name != marker.Name || markers[0].ClientRunID != marker.ClientRunID {
		t.Fatalf("markers = %#v, want named marker fields", markers)
	}
}

func TestMarkerRequestValidation(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	for _, body := range []string{
		`{"name":` + `"` + strings.Repeat("x", 129) + `"}`,
		`{"clientRunId":"\u0001"}`,
		`not-json`,
	} {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/markers", strings.NewReader(body))
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %q status = %d, want %d", body, response.Code, http.StatusBadRequest)
		}
	}
}

func TestReadinessReadyForSelectedServices(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{targets: []model.Target{
		{ID: "worker-1", ServiceName: "worker", Status: model.TargetStatusUp, LastScrapeAt: "2026-06-06T12:00:00Z"},
		{ID: "api-1", ServiceName: "api", Status: model.TargetStatusUp, LastScrapeAt: "2026-06-06T12:00:00Z"},
	}}, Config{})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/readiness?service=worker&service=api", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body readinessResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode readiness: %v", err)
	}
	if !body.Ready || len(body.Services) != 2 || body.Services[0].Service != "api" || body.Services[1].Service != "worker" {
		t.Fatalf("readiness = %#v, want sorted ready services", body)
	}
	if body.WaitedMs > 100 {
		t.Fatalf("waitedMs = %d, want an immediate readiness result", body.WaitedMs)
	}
}

func TestReadinessTimeoutAndMissingService(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/readiness?service=missing&timeout=0", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestTimeout)
	}
	var body readinessResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode readiness: %v", err)
	}
	if body.Ready || len(body.Services) != 1 || body.Services[0].State != "missing" {
		t.Fatalf("readiness = %#v, want missing timeout", body)
	}
}

func TestReadinessValidatesServicesAndTimeout(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{})
	for _, path := range []string{
		"/api/readiness",
		"/api/readiness?service=api&timeout=-1s",
		"/api/readiness?service=api&timeout=121s",
	} {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("path %q status = %d, want %d", path, response.Code, http.StatusBadRequest)
		}
	}
}

func TestMarkersExpireWithRetention(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{Retention: time.Minute})
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	server.now = func() time.Time { return now }
	marker := createMarker(t, server)
	now = now.Add(2 * time.Minute)

	listRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/markers", nil)
	listResponse := httptest.NewRecorder()
	server.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listResponse.Code, http.StatusOK)
	}
	var markers []Marker
	if err := json.NewDecoder(listResponse.Body).Decode(&markers); err != nil {
		t.Fatalf("decode markers: %v", err)
	}
	if len(markers) != 0 {
		t.Fatalf("markers = %#v, want expired marker removed", markers)
	}

	compareRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/compare?from="+marker.ID, nil)
	compareResponse := httptest.NewRecorder()
	server.ServeHTTP(compareResponse, compareRequest)
	if compareResponse.Code != http.StatusNotFound {
		t.Fatalf("compare status = %d, want %d", compareResponse.Code, http.StatusNotFound)
	}
}

func TestCompareValidatesMarkersAndLimit(t *testing.T) {
	server := NewServer(fakeContainerLister{}, fakeTargetStore{}, Config{Retention: time.Hour})
	now := time.Date(2026, 6, 6, 12, 1, 0, 0, time.UTC)
	server.now = func() time.Time { return now }
	later := createMarker(t, server)
	now = now.Add(-time.Minute)
	earlier := createMarker(t, server)

	for _, path := range []string{
		"/api/compare",
		"/api/compare?from=" + later.ID + "&to=" + earlier.ID,
		"/api/compare?from=" + earlier.ID + "&limit=999",
		"/api/compare?from=" + earlier.ID + "&to=missing",
	} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Fatalf("path %q status = %d, want validation error", path, rec.Code)
		}
	}
}

func createMarker(t *testing.T, server *Server) Marker {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/markers", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", rec.Code, http.StatusCreated)
	}
	var marker Marker
	if err := json.NewDecoder(rec.Body).Decode(&marker); err != nil {
		t.Fatalf("decode marker: %v", err)
	}
	return marker
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
