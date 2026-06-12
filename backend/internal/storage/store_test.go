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
