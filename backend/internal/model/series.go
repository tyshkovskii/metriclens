package model

type SeriesPoint struct {
	TS    string  `json:"ts"`
	Value float64 `json:"value"`
}

type Series struct {
	TargetID string            `json:"targetId"`
	Metric   string            `json:"metric"`
	Labels   map[string]string `json:"labels"`
	Points   []SeriesPoint     `json:"points"`
}
