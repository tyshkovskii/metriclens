package model

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
