package scraper

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"metriclens/backend/internal/model"
	"metriclens/backend/internal/storage"
)

type fakeContainerLister struct {
	containers []model.DiscoveredContainer
	err        error
}

type containerListerFunc func(context.Context) ([]model.DiscoveredContainer, error)

func (f containerListerFunc) ListContainers(ctx context.Context) ([]model.DiscoveredContainer, error) {
	return f(ctx)
}

type fakeTargetProber struct {
	targets []model.Target
}

type sequenceTargetProber struct {
	sequences [][]model.Target
	index     int
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f fakeContainerLister) ListContainers(context.Context) ([]model.DiscoveredContainer, error) {
	return f.containers, f.err
}

func (f fakeTargetProber) Probe(context.Context, []model.DiscoveredContainer) []model.Target {
	return f.targets
}

func (p *sequenceTargetProber) Probe(context.Context, []model.DiscoveredContainer) []model.Target {
	if len(p.sequences) == 0 {
		return nil
	}
	index := p.index
	if index >= len(p.sequences) {
		index = len(p.sequences) - 1
	}
	p.index++
	return p.sequences[index]
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

func TestConcurrentIntervalUpdatesDoNotWaitForBusyScraper(t *testing.T) {
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scraper := New(containerListerFunc(func(ctx context.Context) ([]model.DiscoveredContainer, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}), fakeTargetProber{}, nil, nil, time.Minute)
	scraper.Start(ctx)
	<-started

	// Scraping cannot receive an interval update until cancellation. All
	// concurrent API callers must still finish, with the newest value saved.
	var setters sync.WaitGroup
	for worker := range 32 {
		setters.Go(func() {
			for range 100 {
				if err := scraper.SetInterval(time.Duration(worker+1) * time.Second); err != nil {
					t.Errorf("SetInterval() error = %v", err)
				}
			}
		})
	}
	finished := make(chan struct{})
	go func() {
		setters.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("interval setters blocked while scraper was busy")
	}
	if err := scraper.SetInterval(2 * time.Second); err != nil {
		t.Fatalf("final SetInterval() error = %v", err)
	}
	if interval := scraper.currentInterval(); interval != 2*time.Second {
		t.Fatalf("interval = %s, want latest value 2s", interval)
	}
}

func TestRunOncePrunesHistoryWithoutActiveTargets(t *testing.T) {
	for _, discoveryError := range []error{nil, errors.New("Docker unavailable")} {
		store := storage.New(time.Minute)
		first := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
		store.Record("removed-target", []model.MetricFamily{{Samples: []model.MetricSample{
			{Metric: "up", Value: 1},
		}}}, first)
		scraper := New(fakeContainerLister{err: discoveryError}, fakeTargetProber{}, nil, store, time.Second)
		scraper.now = func() time.Time { return first.Add(2 * time.Minute) }
		if err := scraper.RunOnce(context.Background()); !errors.Is(err, discoveryError) {
			t.Fatalf("RunOnce() error = %v, want %v", err, discoveryError)
		}
		if series := store.Series("removed-target", "up", nil); len(series) != 0 {
			t.Fatalf("expired history retained with discovery error %v: %#v", discoveryError, series)
		}
	}
}

func TestScrapeRejectsOversizedResponseWithoutRecordingPartialMetrics(t *testing.T) {
	for _, size := range []int{maxScrapeBody, maxScrapeBody + 1} {
		body := "# TYPE up gauge\nup 1\n"
		padding := size - len(body)
		body += strings.Repeat("#\n", padding/2) + strings.Repeat("#", padding%2)
		store := storage.New(time.Minute)
		scraper := New(nil, nil, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, body), nil
		})}, store, time.Second)
		target, _, ok := scraper.scrapeTarget(context.Background(), model.Target{ID: "target", URL: "http://api/metrics"})
		if size > maxScrapeBody {
			if ok || target.Status != model.TargetStatusDown || !strings.Contains(target.LastError, "exceeds") {
				t.Fatalf("oversized scrape accepted: ok=%v target=%#v", ok, target)
			}
			if series := store.Series("target", "up", nil); len(series) != 0 {
				t.Fatalf("partial metrics stored: %#v", series)
			}
		} else if !ok || target.Status != model.TargetStatusUp {
			t.Fatalf("response at exact limit rejected: %s", target.LastError)
		}
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

func TestLifecycleEventsTrackTransitionsAndDisappearance(t *testing.T) {
	prober := &sequenceTargetProber{sequences: [][]model.Target{
		{{ID: "target-1", ServiceName: "api", ContainerName: "api-1", Status: model.TargetStatusUp, URL: "http://api/metrics"}},
		{{ID: "target-1", ServiceName: "api", ContainerName: "api-1", Status: model.TargetStatusDown, LastError: "scrape failed"}},
		{},
	}}
	scraper := New(
		fakeContainerLister{containers: []model.DiscoveredContainer{{ID: "container-1"}}},
		prober,
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, "# TYPE up gauge\nup 1\n"), nil
		})},
		storage.New(time.Minute),
		time.Second,
	)
	times := []time.Time{
		time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 6, 12, 0, 10, 0, time.UTC),
		time.Date(2026, 6, 6, 12, 0, 20, 0, time.UTC),
	}
	current := times[0]
	scraper.now = func() time.Time {
		return current
	}
	for index := range prober.sequences {
		current = times[index]
		if err := scraper.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}
	}

	events := scraper.LifecycleEvents(times[0], times[2])
	if len(events) != 3 {
		t.Fatalf("events length = %d, want 3: %#v", len(events), events)
	}
	if events[0].Kind != model.LifecycleEventAppeared || events[1].Kind != model.LifecycleEventStatusTransition || events[2].Kind != model.LifecycleEventDisappeared {
		t.Fatalf("events = %#v, want appeared, transition, disappeared", events)
	}
	if events[1].To != model.TargetStatusDown || events[2].TargetID != "target-1" {
		t.Fatalf("events = %#v, want down transition and disappeared target", events)
	}
}

type exitingContainerLister struct {
	fakeContainerLister
	exits map[string]string
}

func (f exitingContainerLister) ContainerExit(_ context.Context, id string) (string, bool) {
	reason, ok := f.exits[id]
	return reason, ok
}

func TestDisappearedTargetReportsHowItsContainerEnded(t *testing.T) {
	prober := &sequenceTargetProber{sequences: [][]model.Target{
		{{ID: "worker-1", HistoryID: "compose:shop/worker/1", ServiceName: "worker", Status: model.TargetStatusDown, LastError: "GET http://worker:9090/metrics failed"}},
		{},
	}}
	lister := exitingContainerLister{exits: map[string]string{"worker-1": "container exited with code 137, OOM killed"}}
	scraper := New(lister, prober, nil, storage.New(time.Minute), time.Second)
	first := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	scraper.now = func() time.Time { return first }
	if err := scraper.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce() error = %v", err)
	}
	scraper.now = func() time.Time { return first.Add(5 * time.Second) }
	if err := scraper.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce() error = %v", err)
	}
	events := scraper.LifecycleEvents(first, first.Add(5*time.Second))
	last := events[len(events)-1]
	if last.Kind != model.LifecycleEventDisappeared || last.Error != "container exited with code 137, OOM killed after scrape error: GET http://worker:9090/metrics failed" {
		t.Fatalf("events = %#v, want disappearance explained by the container exit", events)
	}
}

func TestNoMetricsEndpointOnlyForTargetsNeverScraped(t *testing.T) {
	scrapedTarget := model.Target{ID: "api-1", HistoryID: "compose:shop/api/1", ServiceName: "api", URL: "http://api:8080/metrics", Status: model.TargetStatusUp}
	brokenTarget := scrapedTarget
	brokenTarget.URL, brokenTarget.Status, brokenTarget.NoMetricsEndpoint = "", model.TargetStatusDown, true
	redis := model.Target{ID: "redis-1", HistoryID: "compose:shop/redis/1", ServiceName: "redis", Status: model.TargetStatusDown, NoMetricsEndpoint: true}
	prober := &sequenceTargetProber{sequences: [][]model.Target{{scrapedTarget, redis}, {brokenTarget, redis}}}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "# TYPE up gauge\nup 1\n"), nil
	})}
	scraper := New(fakeContainerLister{}, prober, client, nil, time.Second)
	for range 2 {
		if err := scraper.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}
	}
	flags := map[string]bool{}
	for _, target := range scraper.Targets() {
		flags[target.ServiceName] = target.NoMetricsEndpoint
	}
	if flags["api"] || !flags["redis"] {
		t.Fatalf("noMetricsEndpoint = %#v, want only the never-scraped redis flagged", flags)
	}
}

func TestLifecycleEventsHonorSeriesRetention(t *testing.T) {
	prober := &sequenceTargetProber{sequences: [][]model.Target{
		{{ID: "target-1", ServiceName: "api", Status: model.TargetStatusDown}},
		{},
	}}
	store := storage.New(time.Minute)
	scraper := New(fakeContainerLister{}, prober, nil, store, time.Second)
	first := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	second := first.Add(2 * time.Minute)
	scraper.now = func() time.Time { return first }
	if err := scraper.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce() error = %v", err)
	}
	scraper.now = func() time.Time { return second }
	if err := scraper.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce() error = %v", err)
	}
	events := scraper.LifecycleEvents(first, second)
	if len(events) != 1 || events[0].Kind != model.LifecycleEventDisappeared {
		t.Fatalf("events = %#v, want only retained disappearance", events)
	}
}

func TestLifecycleEventsRecordStableTargetRecreation(t *testing.T) {
	prober := &sequenceTargetProber{sequences: [][]model.Target{
		{{ID: "old-container", HistoryID: "compose:app/api/1", ServiceName: "api", Status: model.TargetStatusDown}},
		{{ID: "new-container", HistoryID: "compose:app/api/1", ServiceName: "api", Status: model.TargetStatusDown}},
		{},
	}}
	scraper := New(fakeContainerLister{}, prober, nil, nil, time.Second)
	current := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	scraper.now = func() time.Time { return current }
	for index := range prober.sequences {
		current = time.Date(2026, 6, 6, 12, 0, index*10, 0, time.UTC)
		if err := scraper.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}
	}

	first := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	second := first.Add(10 * time.Second)
	events := scraper.LifecycleEvents(first, second)
	if len(events) != 2 || events[0].Kind != model.LifecycleEventAppeared || events[1].Kind != model.LifecycleEventRecreated {
		t.Fatalf("events = %#v, want appeared then recreated without disappearance", events)
	}
	if events[1].TargetID != "new-container" {
		t.Fatalf("recreated target id = %q, want new-container", events[1].TargetID)
	}
}

func response(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
