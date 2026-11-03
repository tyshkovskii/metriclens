package model

import (
	"encoding/json"
	"math"
)

type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
	MetricTypeSummary   MetricType = "summary"
	MetricTypeUntyped   MetricType = "untyped"
)

type MetricFamily struct {
	Name    string         `json:"name"`
	Help    string         `json:"help,omitempty"`
	Type    MetricType     `json:"type"`
	Samples []MetricSample `json:"samples"`
	HasHelp bool           `json:"-"`
	HasType bool           `json:"-"`
}

type MetricSample struct {
	Metric    string            `json:"metric"`
	Labels    map[string]string `json:"labels"`
	Value     float64           `json:"value"`
	Timestamp *int64            `json:"timestamp,omitempty"`
}

// MarshalJSON preserves numeric values and represents Prometheus NaN/Inf as
// null, since JSON cannot encode non-finite numbers.
func (sample MetricSample) MarshalJSON() ([]byte, error) {
	type plain MetricSample
	var value *float64
	if !math.IsNaN(sample.Value) && !math.IsInf(sample.Value, 0) {
		value = &sample.Value
	}
	return json.Marshal(struct {
		plain
		Value *float64 `json:"value"`
	}{plain: plain(sample), Value: value})
}
