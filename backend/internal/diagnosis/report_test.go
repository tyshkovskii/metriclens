package diagnosis

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"metriclens/backend/internal/model"
)

type fakeSource struct {
	targets []model.Target
	metrics map[string]model.TargetMetricsResponse
	series  map[string][]model.Series
}

func (f fakeSource) Targets() []model.Target { return f.targets }

func (f fakeSource) TargetMetrics(id string) (model.TargetMetricsResponse, bool) {
	metrics, ok := f.metrics[id]
	return metrics, ok
}

func (f fakeSource) TargetSeries(id, metric string, _ map[string]string) []model.Series {
	all := f.series[id]
	result := make([]model.Series, 0)
	for _, item := range all {
		if item.Metric == metric {
			result = append(result, item)
		}
	}
	return result
}

func TestBuildReportsQualityCounterResetAndGauge(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 2, 0, 0, time.UTC)
	target := model.Target{ID: "target-1", ServiceName: "api", Status: model.TargetStatusUp}
	source := fakeSource{
		targets: []model.Target{target},
		metrics: map[string]model.TargetMetricsResponse{
			"target-1": {
				Target: target,
				Families: []model.MetricFamily{
					{Name: "requests_total", Type: model.MetricTypeCounter, HasType: true, Samples: []model.MetricSample{{Metric: "requests_total", Labels: map[string]string{}, Value: 3}}},
					{Name: "queue_depth", Type: model.MetricTypeGauge, HasType: true, Samples: []model.MetricSample{{Metric: "queue_depth", Labels: map[string]string{}, Value: 3}}},
					{Name: "quality_metric", Type: model.MetricTypeGauge, HasType: false, Samples: []model.MetricSample{{Metric: "quality_metric", Labels: map[string]string{}, Value: 1}}},
				},
			},
		},
		series: map[string][]model.Series{
			"target-1": {
				{Metric: "requests_total", Labels: map[string]string{}, Points: []model.SeriesPoint{
					{TS: "2026-06-06T12:00:00Z", Value: 100},
					{TS: "2026-06-06T12:01:00Z", Value: 110},
					{TS: "2026-06-06T12:02:00Z", Value: 3},
				}},
				{Metric: "queue_depth", Labels: map[string]string{}, Points: []model.SeriesPoint{
					{TS: "2026-06-06T12:00:00Z", Value: 2},
					{TS: "2026-06-06T12:01:00Z", Value: 5},
					{TS: "2026-06-06T12:02:00Z", Value: 3},
				}},
			},
		},
	}

	report := Build(source, now, 2*time.Minute, 20)
	if report.Status != "warning" {
		t.Fatalf("status = %q, want warning", report.Status)
	}
	if report.Targets.Total != 1 || report.Targets.Up != 1 || report.Targets.Healthy != 1 {
		t.Fatalf("targets = %#v, want one healthy target", report.Targets)
	}
	var counter, gauge, qualityFinding *Finding
	for i := range report.Findings {
		finding := &report.Findings[i]
		switch finding.Signal {
		case "counter":
			counter = finding
		case "gauge":
			if finding.Metric == "queue_depth" {
				gauge = finding
			}
		case "quality_warning":
			qualityFinding = finding
		}
	}
	if counter == nil || counter.Delta == nil || *counter.Delta != 13 {
		t.Fatalf("counter = %#v, want reset-aware delta 13", counter)
	}
	if gauge == nil || gauge.Current == nil || gauge.Peak == nil || *gauge.Current != 3 || *gauge.Peak != 5 {
		t.Fatalf("gauge = %#v, want current 3 peak 5", gauge)
	}
	if qualityFinding == nil {
		t.Fatal("quality warning finding missing")
	}
	if report.Window.DurationMs != 2*60*1000 {
		t.Fatalf("window duration = %d, want 120000", report.Window.DurationMs)
	}
}

func TestBuildRanksErrorsAndReportsOmittedFindings(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	source := fakeSource{targets: []model.Target{
		{ID: "down", ServiceName: "api", Status: model.TargetStatusDown, LastError: "connection refused"},
		{ID: "up", ServiceName: "worker", Status: model.TargetStatusUp},
	}}

	report := Build(source, now, time.Minute, 1)
	if report.Status != "error" {
		t.Fatalf("status = %q, want error", report.Status)
	}
	if len(report.Findings) != 1 || report.Findings[0].Severity != "error" {
		t.Fatalf("findings = %#v, want one error finding", report.Findings)
	}
	if report.Omitted != 0 {
		t.Fatalf("omitted = %d, want 0", report.Omitted)
	}
}

func TestBuildUsesMostRecentBaselineBeforeWindow(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 2, 0, 0, time.UTC)
	target := model.Target{ID: "target-1", ServiceName: "api", Status: model.TargetStatusUp}
	source := fakeSource{
		targets: []model.Target{target},
		metrics: map[string]model.TargetMetricsResponse{
			"target-1": {Target: target, Families: []model.MetricFamily{{
				Name: "requests_total", Type: model.MetricTypeCounter, HasType: true,
				Samples: []model.MetricSample{{Metric: "requests_total", Labels: map[string]string{}, Value: 12}},
			}}},
		},
		series: map[string][]model.Series{
			"target-1": {{Metric: "requests_total", Labels: map[string]string{}, Points: []model.SeriesPoint{
				{TS: "2026-06-06T12:00:30Z", Value: 10},
				{TS: "2026-06-06T12:01:30Z", Value: 12},
			}}},
		},
	}

	report := Build(source, now, time.Minute, 10)
	for _, finding := range report.Findings {
		if finding.Signal == "counter" {
			if finding.Delta == nil || *finding.Delta != 2 {
				t.Fatalf("counter delta = %v, want 2 using baseline", finding.Delta)
			}
			return
		}
	}
	t.Fatal("counter finding missing")
}

func TestBuildIncludesLifecycleEvents(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 1, 0, 0, time.UTC)
	target := model.Target{ID: "target-1", ServiceName: "api", Status: model.TargetStatusUp}
	source := lifecycleFakeSource{
		fakeSource: fakeSource{targets: []model.Target{target}},
		events: []model.LifecycleEvent{{
			At: "2026-06-06T12:00:30Z", Kind: model.LifecycleEventDisappeared,
			TargetID: "old", ServiceName: "api", From: model.TargetStatusUp,
		}},
	}
	report := Build(source, now, time.Minute, 10)
	for _, finding := range report.Findings {
		if finding.Signal == "lifecycle" && finding.TargetID == "old" {
			if finding.Severity != "error" {
				t.Fatalf("lifecycle severity = %q, want error", finding.Severity)
			}
			return
		}
	}
	t.Fatal("disappearance finding missing")
}

type lifecycleFakeSource struct {
	fakeSource
	events []model.LifecycleEvent
}

func (f lifecycleFakeSource) LifecycleEvents(start, end time.Time) []model.LifecycleEvent {
	result := make([]model.LifecycleEvent, 0)
	for _, event := range f.events {
		at, err := time.Parse(time.RFC3339Nano, event.At)
		if err != nil {
			continue
		}
		if !at.Before(start) && !at.After(end) {
			result = append(result, event)
		}
	}
	return result
}

func TestBuildFiltersBeforeLimitAndScopesTargetSummary(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	apiTarget := model.Target{ID: "api-1", ServiceName: "api", Status: model.TargetStatusDown, LastError: "api unavailable"}
	workerTarget := model.Target{ID: "worker-1", ServiceName: "worker", Status: model.TargetStatusDown, LastError: "worker unavailable"}
	source := fakeSource{
		targets: []model.Target{apiTarget, workerTarget},
		metrics: map[string]model.TargetMetricsResponse{
			"api-1": {Target: apiTarget, Families: []model.MetricFamily{{Name: "api_metric", HasType: false}}},
		},
	}
	report := BuildWithOptions(source, now, time.Minute, 1, BuildOptions{
		Severities: []string{"warning"},
		Services:   []string{"api"},
	})
	if report.Targets.Total != 1 || report.Targets.Down != 1 {
		t.Fatalf("targets = %#v, want only api target", report.Targets)
	}
	if len(report.Findings) != 1 || report.Findings[0].Severity != "warning" || report.Omitted != 0 {
		t.Fatalf("findings = %#v omitted = %d, want one filtered warning", report.Findings, report.Omitted)
	}
}

func TestBuildChangedOnlyRetainsActionableAndLifecycleFindings(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	target := model.Target{ID: "target-1", ServiceName: "api", Status: model.TargetStatusUp}
	source := lifecycleFakeSource{
		fakeSource: fakeSource{
			targets: []model.Target{target},
			metrics: map[string]model.TargetMetricsResponse{
				"target-1": {Target: target, Families: []model.MetricFamily{
					{Name: "queue_depth", Type: model.MetricTypeGauge, HasHelp: true, HasType: true, Samples: []model.MetricSample{{Metric: "queue_depth", Value: 1}}},
					{Name: "requests_total", Type: model.MetricTypeCounter, HasHelp: true, HasType: true, Samples: []model.MetricSample{{Metric: "requests_total", Value: 1}}},
					{Name: "missing_type", Type: model.MetricTypeGauge, HasHelp: true, HasType: false, Samples: []model.MetricSample{{Metric: "missing_type", Value: 1}}},
				}},
			},
			series: map[string][]model.Series{
				"target-1": {
					{Metric: "queue_depth", Points: []model.SeriesPoint{{TS: "2026-06-06T11:59:00Z", Value: 1}, {TS: "2026-06-06T12:00:00Z", Value: 1}}},
					{Metric: "requests_total", Points: []model.SeriesPoint{{TS: "2026-06-06T11:59:00Z", Value: 1}, {TS: "2026-06-06T12:00:00Z", Value: 1}}},
				},
			},
		},
		events: []model.LifecycleEvent{{At: "2026-06-06T11:59:30Z", Kind: model.LifecycleEventAppeared, TargetID: "target-1", ServiceName: "api"}},
	}
	report := BuildWithOptions(source, now, time.Minute, 20, BuildOptions{ChangedOnly: true})
	seen := map[string]bool{}
	for _, finding := range report.Findings {
		seen[finding.Signal] = true
	}
	if seen["counter"] || seen["gauge"] || !seen["quality_warning"] || !seen["lifecycle"] {
		t.Fatalf("signals = %#v, want actionable/lifecycle findings without zero-change metrics", seen)
	}
}

func TestBuildFindingIDsEvidenceAndDiagnosticBounds(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 2, 0, 0, time.UTC)
	downTarget := model.Target{ID: "down-1", ServiceName: "api", Status: model.TargetStatusDown, LastError: strings.Repeat("界", 600)}
	metricTarget := model.Target{ID: "metric-1", ServiceName: "worker", Status: model.TargetStatusUp}
	source := fakeSource{
		targets: []model.Target{downTarget, metricTarget},
		metrics: map[string]model.TargetMetricsResponse{
			"metric-1": {Target: metricTarget, Families: []model.MetricFamily{{Name: "queue_depth", Type: model.MetricTypeGauge, HasHelp: true, HasType: true, Samples: []model.MetricSample{{Metric: "queue_depth", Value: 3}}}}},
		},
		series: map[string][]model.Series{
			"metric-1": {{Metric: "queue_depth", Points: []model.SeriesPoint{{TS: "2026-06-06T12:01:00Z", Value: 1}, {TS: "2026-06-06T12:02:00Z", Value: 3}}}},
		},
	}
	first := Build(source, now, time.Minute, 20)
	second := Build(source, now, time.Minute, 20)
	if len(first.Findings) != len(second.Findings) {
		t.Fatalf("finding counts = %d and %d, want deterministic output", len(first.Findings), len(second.Findings))
	}
	ids := map[string]struct{}{}
	for index, finding := range first.Findings {
		if finding.ID == "" || finding.ID != second.Findings[index].ID {
			t.Fatalf("finding[%d] ids = %q and %q, want stable non-empty ids", index, finding.ID, second.Findings[index].ID)
		}
		if _, duplicate := ids[finding.ID]; duplicate {
			t.Fatalf("duplicate finding id %q", finding.ID)
		}
		ids[finding.ID] = struct{}{}
		if len([]rune(finding.Message)) > 512 || !utf8.ValidString(finding.Message) {
			t.Fatalf("finding message has invalid bound: runes=%d valid=%v", len([]rune(finding.Message)), utf8.ValidString(finding.Message))
		}
		if finding.Signal == "gauge" {
			if finding.Evidence == nil || finding.Evidence.TargetID != metricTarget.ID || len(finding.Evidence.Metrics) != 1 || finding.Evidence.Metrics[0] != "queue_depth" {
				t.Fatalf("evidence = %#v, want queue_depth descriptor", finding.Evidence)
			}
			if finding.Evidence.Start != first.Window.Start || finding.Evidence.End != first.Window.End {
				t.Fatalf("evidence window = %#v, want report window", finding.Evidence)
			}
		}
	}
}

func TestBoundedDiagnosticTruncatesToValidUTF8(t *testing.T) {
	value := boundedDiagnostic(strings.Repeat("é", 600))
	if len([]rune(value)) != 512 || !strings.HasSuffix(value, "…") || !utf8.ValidString(value) {
		t.Fatalf("bounded value runes=%d suffix=%q valid=%v, want 512-rune ellipsis string", len([]rune(value)), value[len(value)-3:], utf8.ValidString(value))
	}
}
