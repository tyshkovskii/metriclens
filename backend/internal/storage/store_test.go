package storage

import (
	"reflect"
	"testing"
	"time"

	"metriclens/backend/internal/model"
)

func TestRecordStoresLabelVariantsSeparately(t *testing.T) {
	store := New(time.Minute)
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	store.Record("target-1", []model.MetricFamily{
		{
			Name: "http_requests_total",
			Type: model.MetricTypeCounter,
			Samples: []model.MetricSample{
				{Metric: "http_requests_total", Labels: map[string]string{"method": "GET", "status": "200"}, Value: 10},
				{Metric: "http_requests_total", Labels: map[string]string{"method": "GET", "status": "500"}, Value: 2},
			},
		},
	}, now)

	series := store.Series("target-1", "http_requests_total", nil)
	if len(series) != 2 {
		t.Fatalf("series length = %d, want 2", len(series))
	}
	if series[0].Labels["status"] == series[1].Labels["status"] {
		t.Fatalf("series labels were not kept separate: %#v", series)
	}
}

func TestSeriesCanFilterByLabels(t *testing.T) {
	store := New(time.Minute)
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	store.Record("target-1", []model.MetricFamily{
		{
			Name: "http_requests_total",
			Type: model.MetricTypeCounter,
			Samples: []model.MetricSample{
				{Metric: "http_requests_total", Labels: map[string]string{"method": "GET", "status": "200"}, Value: 10},
				{Metric: "http_requests_total", Labels: map[string]string{"method": "GET", "status": "500"}, Value: 2},
			},
		},
	}, now)

	series := store.Series("target-1", "http_requests_total", map[string]string{"status": "500", "method": "GET"})
	if len(series) != 1 {
		t.Fatalf("series length = %d, want 1", len(series))
	}
	if series[0].Points[0].Value != 2 {
		t.Fatalf("point value = %v, want 2", series[0].Points[0].Value)
	}
}

func TestRecordDropsPointsOlderThanRetention(t *testing.T) {
	store := New(time.Minute)
	first := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	second := first.Add(2 * time.Minute)

	store.Record("target-1", familiesWithSample("up", 1), first)
	store.Record("target-1", familiesWithSample("up", 2), second)

	series := store.Series("target-1", "up", nil)
	if len(series) != 1 {
		t.Fatalf("series length = %d, want 1", len(series))
	}
	if len(series[0].Points) != 1 {
		t.Fatalf("points length = %d, want 1", len(series[0].Points))
	}
	if series[0].Points[0].Value != 2 {
		t.Fatalf("point value = %v, want 2", series[0].Points[0].Value)
	}
}

func TestRecordUsesSampleTimestampWhenPresent(t *testing.T) {
	store := New(time.Minute)
	scrapedAt := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	timestamp := scrapedAt.Add(-30 * time.Second).UnixMilli()

	store.Record("target-1", []model.MetricFamily{
		{
			Name: "up",
			Type: model.MetricTypeGauge,
			Samples: []model.MetricSample{
				{Metric: "up", Labels: map[string]string{}, Value: 1, Timestamp: &timestamp},
			},
		},
	}, scrapedAt)

	series := store.Series("target-1", "up", nil)
	if len(series) != 1 || len(series[0].Points) != 1 {
		t.Fatalf("series = %#v, want one point", series)
	}
	if series[0].Points[0].TS != scrapedAt.Add(-30*time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("ts = %q, want sample timestamp", series[0].Points[0].TS)
	}
}

func TestPruneExpiresAbsentTargetsAndLabelVariants(t *testing.T) {
	store := New(time.Minute)
	first := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	store.Record("removed-target", familiesWithSample("up", 1), first)
	store.Record("active-target", []model.MetricFamily{{Samples: []model.MetricSample{
		{Metric: "requests_total", Labels: map[string]string{"route": "/old"}, Value: 1},
	}}}, first)
	store.Record("active-target", []model.MetricFamily{{Samples: []model.MetricSample{
		{Metric: "requests_total", Labels: map[string]string{"route": "/new"}, Value: 2},
	}}}, first.Add(time.Minute))

	store.Prune(first.Add(90 * time.Second))
	if series := store.Series("removed-target", "up", nil); len(series) != 0 {
		t.Fatalf("removed target still retained: %#v", series)
	}
	series := store.Series("active-target", "requests_total", nil)
	if len(series) != 1 || series[0].Labels["route"] != "/new" {
		t.Fatalf("series = %#v, want only unexpired label variant", series)
	}
	if len(store.series) != 1 {
		t.Fatalf("stored series = %d, want expired identities deleted", len(store.series))
	}
}

func TestPruneDropsOutOfOrderExpiredPoints(t *testing.T) {
	store := New(time.Minute)
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	store.Record("target", familiesWithSample("up", 2), now)
	timestamp := now.Add(-30 * time.Second).UnixMilli()
	store.Record("target", []model.MetricFamily{{Samples: []model.MetricSample{
		{Metric: "up", Value: 1, Timestamp: &timestamp},
	}}}, now)
	beforePrune := store.Series("target", "up", nil)
	if len(beforePrune) != 1 || len(beforePrune[0].Points) != 2 || beforePrune[0].Points[0].Value != 1 {
		t.Fatalf("series = %#v, want timestamped points returned chronologically", beforePrune)
	}
	store.Prune(now.Add(45 * time.Second))
	series := store.Series("target", "up", nil)
	if len(series) != 1 || len(series[0].Points) != 1 || series[0].Points[0].Value != 2 {
		t.Fatalf("series = %#v, want only unexpired point despite timestamp order", series)
	}
}

func TestRecordDiscardsAlreadyExpiredTimestamp(t *testing.T) {
	store := New(time.Minute)
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	timestamp := now.Add(-2 * time.Minute).UnixMilli()
	store.Record("target", []model.MetricFamily{{Samples: []model.MetricSample{
		{Metric: "up", Value: 1, Timestamp: &timestamp},
	}}}, now)
	if len(store.series) != 0 {
		t.Fatalf("stored series = %d, want no expired sample stored", len(store.series))
	}
}

func TestRecordPreservesCounterReset(t *testing.T) {
	store := New(time.Minute)
	first := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	store.Record("target-1", familiesWithSample("requests_total", 100), first)
	store.Record("target-1", familiesWithSample("requests_total", 5), first.Add(5*time.Second))

	series := store.Series("target-1", "requests_total", nil)
	if len(series) != 1 {
		t.Fatalf("series length = %d, want 1", len(series))
	}

	got := []float64{series[0].Points[0].Value, series[0].Points[1].Value}
	if !reflect.DeepEqual(got, []float64{100, 5}) {
		t.Fatalf("points = %#v, want raw reset values [100 5]", got)
	}
}

func TestRecordWithIdentityContinuesAcrossPublicTargetIDs(t *testing.T) {
	store := New(time.Minute)
	first := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	store.RecordWithIdentity("compose:app/api/1", "old-container", familiesWithSample("up", 1), first)
	store.RecordWithIdentity("compose:app/api/1", "new-container", familiesWithSample("up", 2), first.Add(5*time.Second))

	series := store.SeriesFor("compose:app/api/1", "new-container", "up", nil)
	if len(series) != 1 {
		t.Fatalf("series length = %d, want 1", len(series))
	}
	if series[0].TargetID != "new-container" {
		t.Fatalf("target id = %q, want new-container", series[0].TargetID)
	}
	if len(series[0].Points) != 2 || series[0].Points[0].Value != 1 || series[0].Points[1].Value != 2 {
		t.Fatalf("points = %#v, want retained old and new points", series[0].Points)
	}
}

func TestSeriesBatchForFiltersMetricsAndTime(t *testing.T) {
	store := New(10 * time.Minute)
	first := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	store.Record("target-1", []model.MetricFamily{
		{Name: "a", Samples: []model.MetricSample{{Metric: "a", Labels: map[string]string{}, Value: 1}}},
		{Name: "b", Samples: []model.MetricSample{{Metric: "b", Labels: map[string]string{}, Value: 2}}},
	}, first)
	store.Record("target-1", []model.MetricFamily{
		{Name: "a", Samples: []model.MetricSample{{Metric: "a", Labels: map[string]string{}, Value: 3}}},
		{Name: "b", Samples: []model.MetricSample{{Metric: "b", Labels: map[string]string{}, Value: 4}}},
	}, first.Add(time.Minute))

	start := first.Add(30 * time.Second)
	series := store.SeriesBatchFor("target-1", "target-1", []string{"b", "a"}, &start, nil, nil)
	if len(series) != 2 {
		t.Fatalf("series length = %d, want 2", len(series))
	}
	if series[0].Metric != "a" || series[0].Points[0].Value != 3 {
		t.Fatalf("first series = %#v, want a at value 3", series[0])
	}
	if series[1].Metric != "b" || series[1].Points[0].Value != 4 {
		t.Fatalf("second series = %#v, want b at value 4", series[1])
	}
}

func TestSeriesBatchForAtSelectsLastPointAtOrBeforeInstant(t *testing.T) {
	store := New(10 * time.Minute)
	first := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	store.Record("target-1", familiesWithSample("up", 1), first)
	store.Record("target-1", familiesWithSample("up", 2), first.Add(time.Minute))
	at := first.Add(30 * time.Second)

	series := store.SeriesBatchFor("target-1", "target-1", []string{"up"}, nil, nil, &at)
	if len(series) != 1 || len(series[0].Points) != 1 || series[0].Points[0].Value != 1 {
		t.Fatalf("series = %#v, want one point with value 1", series)
	}
}

func familiesWithSample(metric string, value float64) []model.MetricFamily {
	return []model.MetricFamily{
		{
			Name: metric,
			Type: model.MetricTypeUntyped,
			Samples: []model.MetricSample{
				{Metric: metric, Labels: map[string]string{}, Value: value},
			},
		},
	}
}
