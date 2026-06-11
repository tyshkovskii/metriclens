package model

type MetricQualitySeverity string

const (
	MetricQualityInfo    MetricQualitySeverity = "info"
	MetricQualityWarning MetricQualitySeverity = "warning"
)

type MetricQualityIssue struct {
	Severity   MetricQualitySeverity `json:"severity"`
	Metric     string                `json:"metric"`
	Message    string                `json:"message"`
	Suggestion string                `json:"suggestion,omitempty"`
}
