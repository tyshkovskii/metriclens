package model

import (
	"encoding/json"
	"math"
)

type SeriesPoint struct {
	TS    string  `json:"ts"`
	Value float64 `json:"value"`
}

// MarshalJSON uses null for non-finite samples so clients can display a gap.
func (point SeriesPoint) MarshalJSON() ([]byte, error) {
	var value *float64
	if !math.IsNaN(point.Value) && !math.IsInf(point.Value, 0) {
		value = &point.Value
	}
	return json.Marshal(struct {
		TS    string   `json:"ts"`
		Value *float64 `json:"value"`
	}{TS: point.TS, Value: value})
}

type Series struct {
	TargetID string            `json:"targetId"`
	Metric   string            `json:"metric"`
	Labels   map[string]string `json:"labels"`
	Points   []SeriesPoint     `json:"points"`
}
