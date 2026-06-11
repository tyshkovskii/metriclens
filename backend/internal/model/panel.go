package model

type PanelKind string

const (
	PanelKindCounterRate      PanelKind = "counter_rate"
	PanelKindGauge            PanelKind = "gauge"
	PanelKindHistogramLatency PanelKind = "histogram_latency"
	PanelKindHTTPRate         PanelKind = "http_rate"
	PanelKindHTTPErrorRate    PanelKind = "http_error_rate"
	PanelKindSummaryQuantiles PanelKind = "summary_quantiles"
)

type SuggestedPanel struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Kind       PanelKind `json:"kind"`
	Metric     string    `json:"metric"`
	Confidence float64   `json:"confidence"`
	Reason     string    `json:"reason"`
	Labels     []string  `json:"labels,omitempty"`
	Unit       string    `json:"unit,omitempty"`
}
