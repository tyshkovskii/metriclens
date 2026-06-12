package scraper

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"metriclens/backend/internal/model"
	"metriclens/backend/internal/storage"
)

type fakeContainerLister struct {
	containers []model.DiscoveredContainer
	err        error
}

type fakeTargetProber struct {
	targets []model.Target
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f fakeContainerLister) ListContainers(context.Context) ([]model.DiscoveredContainer, error) {
	return f.containers, f.err
}

func (f fakeTargetProber) Probe(context.Context, []model.DiscoveredContainer) []model.Target {
	return f.targets
}

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRunOnceScrapesUpTargets(t *testing.T) {
	scraper := New(
		fakeContainerLister{containers: []model.DiscoveredContainer{{ID: "container-1"}}},
		fakeTargetProber{targets: []model.Target{
			{
				ID:            "abc123",
				ServiceName:   "api",
				ContainerName: "api-1",
				URL:           "http://api:8080/metrics",
				Status:        model.TargetStatusUp,
				DiscoveredAt:  "2026-06-06T12:00:00Z",
			},
		}},
		&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "http://api:8080/metrics" {
				t.Fatalf("request URL = %q, want metrics URL", req.URL.String())
			}
			return response(http.StatusOK, "# TYPE up gauge\nup 1\n"), nil
		})},
		nil,
		time.Second,
	)
	scraper.now = func() time.Time { return time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC) }

	if err := scraper.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	targets := scraper.Targets()
	if len(targets) != 1 {
		t.Fatalf("targets length = %d, want 1", len(targets))
	}
	if targets[0].Status != model.TargetStatusUp {
		t.Fatalf("status = %q, want up", targets[0].Status)
	}
	if targets[0].LastScrapeAt != "2026-06-06T12:00:00Z" {
		t.Fatalf("lastScrapeAt = %q, want fixed time", targets[0].LastScrapeAt)
	}
	if targets[0].LastScrapeDuration == "" {
		t.Fatal("lastScrapeDuration is empty")
	}

	metrics, ok := scraper.TargetMetrics("abc123")
	if !ok {
		t.Fatal("TargetMetrics() ok = false, want true")
	}
	if len(metrics.Families) != 1 {
		t.Fatalf("families length = %d, want 1", len(metrics.Families))
	}
	if metrics.Families[0].Name != "up" {
		t.Fatalf("family name = %q, want up", metrics.Families[0].Name)
	}
}

func TestRunOnceMarksTargetDownOnScrapeHTTPFailure(t *testing.T) {
	scraper := New(
		fakeContainerLister{containers: []model.DiscoveredContainer{{ID: "container-1"}}},
		fakeTargetProber{targets: []model.Target{
			{
				ID:            "abc123",
				ServiceName:   "api",
				ContainerName: "api-1",
				URL:           "http://api:8080/metrics",
				Status:        model.TargetStatusUp,
				DiscoveredAt:  "2026-06-06T12:00:00Z",
			},
		}},
		&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return response(http.StatusInternalServerError, "error"), nil
		})},
		nil,
		time.Second,
	)

	if err := scraper.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	targets := scraper.Targets()
	if targets[0].Status != model.TargetStatusDown {
		t.Fatalf("status = %q, want down", targets[0].Status)
	}
	if !strings.Contains(targets[0].LastError, "HTTP 500") {
		t.Fatalf("lastError = %q, want HTTP 500", targets[0].LastError)
	}
}

func TestRunOnceMarksTargetDownOnBadMetrics(t *testing.T) {
	scraper := New(
		fakeContainerLister{containers: []model.DiscoveredContainer{{ID: "container-1"}}},
		fakeTargetProber{targets: []model.Target{
			{
				ID:            "abc123",
				ServiceName:   "api",
				ContainerName: "api-1",
				URL:           "http://api:8080/metrics",
				Status:        model.TargetStatusUp,
				DiscoveredAt:  "2026-06-06T12:00:00Z",
			},
		}},
		&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return response(http.StatusOK, "requests_total nope\n"), nil
		})},
		nil,
		time.Second,
	)

	if err := scraper.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	targets := scraper.Targets()
	if targets[0].Status != model.TargetStatusDown {
		t.Fatalf("status = %q, want down", targets[0].Status)
	}
	if !strings.Contains(targets[0].LastError, "parse metrics") {
		t.Fatalf("lastError = %q, want parse error", targets[0].LastError)
	}
}

func TestRunOnceStoresProbeDownTargetWithoutScraping(t *testing.T) {
	scraper := New(
		fakeContainerLister{containers: []model.DiscoveredContainer{{ID: "container-1"}}},
		fakeTargetProber{targets: []model.Target{
			{
				ID:            "abc123",
				ServiceName:   "api",
				ContainerName: "api-1",
				Status:        model.TargetStatusDown,
				LastError:     "no Prometheus endpoint found",
				DiscoveredAt:  "2026-06-06T12:00:00Z",
			},
		}},
		&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatalf("unexpected scrape request to %s", req.URL.String())
			return nil, errors.New("unexpected request")
		})},
		nil,
		time.Second,
	)

	if err := scraper.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	targets := scraper.Targets()
	if len(targets) != 1 {
		t.Fatalf("targets length = %d, want 1", len(targets))
	}
	if targets[0].Status != model.TargetStatusDown {
		t.Fatalf("status = %q, want down", targets[0].Status)
	}
}

func TestRunOnceRecordsDiscoveryError(t *testing.T) {
	scraper := New(
		fakeContainerLister{err: errors.New("docker unavailable")},
		fakeTargetProber{},
		nil,
		nil,
		time.Second,
	)

	err := scraper.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce() error = nil, want error")
	}
	if scraper.LastError() == nil {
		t.Fatal("LastError() = nil, want error")
	}
}

func TestRunOnceRecordsSeries(t *testing.T) {
	seriesStore := storage.New(time.Minute)
	scraper := New(
		fakeContainerLister{containers: []model.DiscoveredContainer{{ID: "container-1"}}},
		fakeTargetProber{targets: []model.Target{
			{
				ID:            "abc123",
				ServiceName:   "api",
				ContainerName: "api-1",
				URL:           "http://api:8080/metrics",
				Status:        model.TargetStatusUp,
				DiscoveredAt:  "2026-06-06T12:00:00Z",
			},
		}},
		&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return response(http.StatusOK, `# TYPE http_requests_total counter
http_requests_total{method="GET",status="200"} 10
`), nil
		})},
		seriesStore,
		time.Second,
	)
	scraper.now = func() time.Time { return time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC) }

	if err := scraper.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	series := scraper.TargetSeries("abc123", "http_requests_total", nil)
	if len(series) != 1 {
		t.Fatalf("series length = %d, want 1", len(series))
	}
	if series[0].Labels["method"] != "GET" {
		t.Fatalf("method label = %q, want GET", series[0].Labels["method"])
	}
	if len(series[0].Points) != 1 || series[0].Points[0].Value != 10 {
		t.Fatalf("points = %#v, want one point with value 10", series[0].Points)
	}
}

func response(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
