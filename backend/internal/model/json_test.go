package model

import (
	"encoding/json"
	"math"
	"testing"
)

func TestMetricValuesEncodeFiniteNumbersAndNonFiniteGaps(t *testing.T) {
	for _, value := range []float64{0, -3.5, 42, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, item := range []any{
			MetricSample{Metric: "queue_depth", Labels: map[string]string{"queue": "checkout"}, Value: value},
			SeriesPoint{TS: "2026-11-02T12:00:00Z", Value: value},
		} {
			encoded, err := json.Marshal(item)
			if err != nil {
				t.Fatalf("encode %T value %v: %v", item, value, err)
			}
			var result map[string]any
			if err := json.Unmarshal(encoded, &result); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			got, exists := result["value"]
			if !exists {
				t.Fatal("value field omitted")
			}
			if math.IsNaN(value) || math.IsInf(value, 0) {
				if got != nil {
					t.Fatalf("non-finite value = %v, want null", got)
				}
			} else if got != value {
				t.Fatalf("finite value = %v, want %v", got, value)
			}
			if sample, ok := item.(MetricSample); ok && (result["metric"] != sample.Metric || result["labels"] == nil) {
				t.Fatalf("metric metadata lost: %s", encoded)
			}
			if point, ok := item.(SeriesPoint); ok && result["ts"] != point.TS {
				t.Fatalf("timestamp lost: %s", encoded)
			}
		}
	}
}
