package diagnosis

import (
	"testing"
	"time"

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
