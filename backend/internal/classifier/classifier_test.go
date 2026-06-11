package classifier

import (
	"reflect"
	"strconv"
	"testing"

	"metriclens/backend/internal/model"
)

func TestClassifyCounter(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "jobs_processed",
			Type: model.MetricTypeCounter,
		},
	}, nil)

	panel, ok := findPanel(panels, model.PanelKindCounterRate, "jobs_processed")
	if !ok {
		t.Fatalf("counter_rate panel not found in %#v", panels)
	}
	if panel.Confidence <= 0 || panel.Confidence >= 1 {
		t.Fatalf("confidence = %v, want between 0 and 1", panel.Confidence)
	}
	if panel.Reason == "" {
		t.Fatal("reason is empty")
	}
}

func TestClassifyCounterByName(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "queue_jobs_total",
			Type: model.MetricTypeUntyped,
		},
	}, nil)

	if _, ok := findPanel(panels, model.PanelKindCounterRate, "queue_jobs_total"); !ok {
		t.Fatalf("counter_rate panel not found in %#v", panels)
	}
}

func TestClassifyGauge(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "queue_depth",
			Type: model.MetricTypeGauge,
		},
	}, nil)

	if _, ok := findPanel(panels, model.PanelKindGauge, "queue_depth"); !ok {
		t.Fatalf("gauge panel not found in %#v", panels)
	}
}

func TestDeclaredGaugeBeatsTotalSuffix(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "cache_items_total",
			Type: model.MetricTypeGauge,
		},
	}, nil)

	if _, ok := findPanel(panels, model.PanelKindGauge, "cache_items_total"); !ok {
		t.Fatalf("gauge panel not found in %#v", panels)
	}
	if _, ok := findPanel(panels, model.PanelKindCounterRate, "cache_items_total"); ok {
		t.Fatalf("counter_rate panel should not exist for a declared gauge, got %#v", panels)
	}
}

func TestClassifyGaugeUnitTitle(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "queue_oldest_age_seconds",
			Type: model.MetricTypeGauge,
		},
	}, nil)

	panel, ok := findPanel(panels, model.PanelKindGauge, "queue_oldest_age_seconds")
	if !ok {
		t.Fatalf("gauge panel not found in %#v", panels)
	}
	if panel.Title != "Seconds over time" {
		t.Fatalf("title = %q, want Seconds over time", panel.Title)
	}
	if panel.Unit != "seconds" {
		t.Fatalf("unit = %q, want seconds", panel.Unit)
	}
}

func TestClassifyWellKnownMetric(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{Name: "go_goroutines", Type: model.MetricTypeGauge},
		{Name: "process_cpu_seconds_total", Type: model.MetricTypeCounter},
	}, nil)

	gauge, ok := findPanel(panels, model.PanelKindGauge, "go_goroutines")
	if !ok {
		t.Fatalf("gauge panel not found in %#v", panels)
	}
	if gauge.Title != "Goroutines" {
		t.Fatalf("title = %q, want Goroutines", gauge.Title)
	}

	cpu, ok := findPanel(panels, model.PanelKindCounterRate, "process_cpu_seconds_total")
	if !ok {
		t.Fatalf("counter_rate panel not found in %#v", panels)
	}
	if cpu.Title != "CPU usage" || cpu.Unit != "cores" {
		t.Fatalf("panel = %q/%q, want CPU usage/cores", cpu.Title, cpu.Unit)
	}
}

func TestClassifyErrorCounterTitle(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "jobs_failed_total",
			Type: model.MetricTypeCounter,
		},
	}, nil)

	panel, ok := findPanel(panels, model.PanelKindCounterRate, "jobs_failed_total")
	if !ok {
		t.Fatalf("counter_rate panel not found in %#v", panels)
	}
	if panel.Title != "Error rate" {
		t.Fatalf("title = %q, want Error rate", panel.Title)
	}
}

func TestClassifyInfoMetricSkipped(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "build_info",
			Type: model.MetricTypeGauge,
			Samples: []model.MetricSample{
				{Metric: "build_info", Labels: map[string]string{"version": "1.2.3"}, Value: 1},
			},
		},
	}, nil)

	if len(panels) != 0 {
		t.Fatalf("panels = %#v, want none for an _info metric", panels)
	}
}

func TestClassifyHistogram(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "http_request_duration_seconds",
			Type: model.MetricTypeHistogram,
			Samples: []model.MetricSample{
				{Metric: "http_request_duration_seconds_bucket", Labels: map[string]string{"le": "0.1"}, Value: 10},
				{Metric: "http_request_duration_seconds_sum", Labels: map[string]string{}, Value: 3.2},
				{Metric: "http_request_duration_seconds_count", Labels: map[string]string{}, Value: 12},
			},
		},
	}, nil)

	panel, ok := findPanel(panels, model.PanelKindHistogramLatency, "http_request_duration_seconds")
	if !ok {
		t.Fatalf("histogram_latency panel not found in %#v", panels)
	}
	if panel.Title != "Latency p95" {
		t.Fatalf("title = %q, want Latency p95", panel.Title)
	}
	if panel.Unit != "seconds" {
		t.Fatalf("unit = %q, want seconds", panel.Unit)
	}
}

func TestClassifySizeHistogramTitle(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "http_response_size_bytes",
			Type: model.MetricTypeHistogram,
			Samples: []model.MetricSample{
				{Metric: "http_response_size_bytes_bucket", Labels: map[string]string{"le": "1024"}, Value: 10},
			},
		},
	}, nil)

	panel, ok := findPanel(panels, model.PanelKindHistogramLatency, "http_response_size_bytes")
	if !ok {
		t.Fatalf("histogram_latency panel not found in %#v", panels)
	}
	if panel.Title != "Size p95" {
		t.Fatalf("title = %q, want Size p95", panel.Title)
	}
}

func TestClassifyHistogramFromSplitFamilies(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{Name: "request_duration_seconds_bucket", Type: model.MetricTypeUntyped},
		{Name: "request_duration_seconds_sum", Type: model.MetricTypeUntyped},
		{Name: "request_duration_seconds_count", Type: model.MetricTypeUntyped},
	}, nil)

	if _, ok := findPanel(panels, model.PanelKindHistogramLatency, "request_duration_seconds"); !ok {
		t.Fatalf("histogram_latency panel not found in %#v", panels)
	}
}

func TestClassifySummary(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "rpc_duration_seconds",
			Type: model.MetricTypeSummary,
			Samples: []model.MetricSample{
				{Metric: "rpc_duration_seconds", Labels: map[string]string{"quantile": "0.5"}, Value: 0.02},
				{Metric: "rpc_duration_seconds", Labels: map[string]string{"quantile": "0.95"}, Value: 0.1},
				{Metric: "rpc_duration_seconds_sum", Labels: map[string]string{}, Value: 4.2},
				{Metric: "rpc_duration_seconds_count", Labels: map[string]string{}, Value: 120},
			},
		},
	}, nil)

	quantiles, ok := findPanel(panels, model.PanelKindSummaryQuantiles, "rpc_duration_seconds")
	if !ok {
		t.Fatalf("summary_quantiles panel not found in %#v", panels)
	}
	if quantiles.Title != "Latency quantiles" {
		t.Fatalf("title = %q, want Latency quantiles", quantiles.Title)
	}

	throughput, ok := findPanel(panels, model.PanelKindCounterRate, "rpc_duration_seconds_count")
	if !ok {
		t.Fatalf("throughput panel not found in %#v", panels)
	}
	if throughput.Title != "Throughput" {
		t.Fatalf("title = %q, want Throughput", throughput.Title)
	}
}

func TestClassifyUntypedSummaryByQuantileLabel(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "task_latency_seconds",
			Type: model.MetricTypeUntyped,
			Samples: []model.MetricSample{
				{Metric: "task_latency_seconds", Labels: map[string]string{"quantile": "0.99"}, Value: 0.3},
			},
		},
	}, nil)

	panel, ok := findPanel(panels, model.PanelKindSummaryQuantiles, "task_latency_seconds")
	if !ok {
		t.Fatalf("summary_quantiles panel not found in %#v", panels)
	}
	if panel.Confidence >= 0.85 {
		t.Fatalf("confidence = %v, want lower than a declared summary", panel.Confidence)
	}
}

func TestClassifyHTTPRateAndErrorRate(t *testing.T) {
	panels := Classify([]model.MetricFamily{
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
	}, nil)

	ratePanel, ok := findPanel(panels, model.PanelKindHTTPRate, "http_requests_total")
	if !ok {
		t.Fatalf("http_rate panel not found in %#v", panels)
	}
	if !reflect.DeepEqual(ratePanel.Labels, []string{"method", "route", "status"}) {
		t.Fatalf("labels = %#v, want sorted HTTP labels", ratePanel.Labels)
	}
	if _, ok := findPanel(panels, model.PanelKindHTTPErrorRate, "http_requests_total"); !ok {
		t.Fatalf("http_error_rate panel not found in %#v", panels)
	}
	if _, ok := findPanel(panels, model.PanelKindCounterRate, "http_requests_total"); ok {
		t.Fatalf("generic counter_rate should be suppressed by http_rate, got %#v", panels)
	}
}

func TestClassifyHTTPByNameAndSingleLabel(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "http_requests_total",
			Type: model.MetricTypeCounter,
			Samples: []model.MetricSample{
				{Metric: "http_requests_total", Labels: map[string]string{"status": "200"}, Value: 10},
			},
		},
	}, nil)

	if _, ok := findPanel(panels, model.PanelKindHTTPRate, "http_requests_total"); !ok {
		t.Fatalf("http_rate panel not found in %#v", panels)
	}
	if _, ok := findPanel(panels, model.PanelKindHTTPErrorRate, "http_requests_total"); !ok {
		t.Fatalf("http_error_rate panel not found in %#v", panels)
	}
}

func TestClassifyGRPCRate(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "grpc_server_handled_total",
			Type: model.MetricTypeCounter,
			Samples: []model.MetricSample{
				{
					Metric: "grpc_server_handled_total",
					Labels: map[string]string{"grpc_method": "GetUser", "grpc_code": "OK"},
					Value:  10,
				},
			},
		},
	}, nil)

	ratePanel, ok := findPanel(panels, model.PanelKindHTTPRate, "grpc_server_handled_total")
	if !ok {
		t.Fatalf("http_rate panel not found in %#v", panels)
	}
	if ratePanel.Title != "gRPC request rate" {
		t.Fatalf("title = %q, want gRPC request rate", ratePanel.Title)
	}
	if _, ok := findPanel(panels, model.PanelKindHTTPErrorRate, "grpc_server_handled_total"); !ok {
		t.Fatalf("http_error_rate panel not found in %#v", panels)
	}
}

func TestClassifyUnknownMetric(t *testing.T) {
	panels := Classify([]model.MetricFamily{
		{
			Name: "custom_value",
			Type: model.MetricTypeUntyped,
		},
	}, nil)

	if len(panels) != 0 {
		t.Fatalf("panels = %#v, want none", panels)
	}
}

func TestClassifyUntypedCounterFromBehavior(t *testing.T) {
	history := historyOf("custom_value", 1, 2, 2, 5, 9, 12)
	panels := Classify([]model.MetricFamily{
		{Name: "custom_value", Type: model.MetricTypeUntyped},
	}, history)

	panel, ok := findPanel(panels, model.PanelKindCounterRate, "custom_value")
	if !ok {
		t.Fatalf("counter_rate panel not found in %#v", panels)
	}
	if panel.Reason == "" {
		t.Fatal("reason is empty")
	}
}

func TestClassifyUntypedGaugeFromBehavior(t *testing.T) {
	history := historyOf("custom_value", 5, 9, 3, 7, 2, 8)
	panels := Classify([]model.MetricFamily{
		{Name: "custom_value", Type: model.MetricTypeUntyped},
	}, history)

	if _, ok := findPanel(panels, model.PanelKindGauge, "custom_value"); !ok {
		t.Fatalf("gauge panel not found in %#v", panels)
	}
	if _, ok := findPanel(panels, model.PanelKindCounterRate, "custom_value"); ok {
		t.Fatalf("counter_rate panel should not exist for varying values, got %#v", panels)
	}
}

func TestBehaviorOverridesTotalSuffix(t *testing.T) {
	history := historyOf("swap_total", 100, 80, 120, 90, 110)
	panels := Classify([]model.MetricFamily{
		{Name: "swap_total", Type: model.MetricTypeUntyped},
	}, history)

	if _, ok := findPanel(panels, model.PanelKindGauge, "swap_total"); !ok {
		t.Fatalf("gauge panel not found in %#v", panels)
	}
	if _, ok := findPanel(panels, model.PanelKindCounterRate, "swap_total"); ok {
		t.Fatalf("counter_rate panel should not exist for a decreasing _total metric, got %#v", panels)
	}
}

func TestSingleDecreaseStaysCounter(t *testing.T) {
	// One decrease looks like a counter reset, not a gauge.
	history := historyOf("requests_total", 10, 20, 30, 2, 8, 15)
	panels := Classify([]model.MetricFamily{
		{Name: "requests_total", Type: model.MetricTypeUntyped},
	}, history)

	if _, ok := findPanel(panels, model.PanelKindCounterRate, "requests_total"); !ok {
		t.Fatalf("counter_rate panel not found in %#v", panels)
	}
}

func historyOf(metric string, values ...float64) SeriesLookup {
	points := make([]model.SeriesPoint, 0, len(values))
	for index, value := range values {
		points = append(points, model.SeriesPoint{TS: "2026-01-01T00:00:0" + strconv.Itoa(index) + "Z", Value: value})
	}
	series := []model.Series{{Metric: metric, Labels: map[string]string{}, Points: points}}
	return func(name string) []model.Series {
		if name == metric {
			return series
		}
		return nil
	}
}

func findPanel(panels []model.SuggestedPanel, kind model.PanelKind, metric string) (model.SuggestedPanel, bool) {
	for _, panel := range panels {
		if panel.Kind == kind && panel.Metric == metric {
			return panel, true
		}
	}
	return model.SuggestedPanel{}, false
}
