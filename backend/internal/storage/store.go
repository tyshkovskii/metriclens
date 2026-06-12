package storage

import (
	"sort"
	"strings"
	"sync"
	"time"

	"metriclens/backend/internal/model"
)

type Store struct {
	mu        sync.RWMutex
	retention time.Duration
	series    map[string]*storedSeries
}

type storedSeries struct {
	targetID string
	metric   string
	labels   map[string]string
	points   []storedPoint
}

type storedPoint struct {
	ts    time.Time
	value float64
}

func New(retention time.Duration) *Store {
	if retention <= 0 {
		retention = DefaultRetention
	}
	return &Store{
		retention: retention,
		series:    map[string]*storedSeries{},
	}
}

func (s *Store) Record(targetID string, families []model.MetricFamily, scrapedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	scrapedAt = scrapedAt.UTC()
	cutoff := scrapedAt.Add(-s.retention)
	for _, family := range families {
		for _, sample := range family.Samples {
			pointTime := sampleTime(sample, scrapedAt)
			key := seriesKey(targetID, sample.Metric, sample.Labels)
			series, ok := s.series[key]
			if !ok {
				series = &storedSeries{
					targetID: targetID,
					metric:   sample.Metric,
					labels:   copyLabels(sample.Labels),
				}
				s.series[key] = series
			}
			series.points = append(series.points, storedPoint{ts: pointTime, value: sample.Value})
			series.trim(cutoff)
		}
	}
}

// Series returns stored series for a metric. A nil labels map matches every
// series of the metric; a non-nil map (even an empty one) matches only the
// series whose label set is exactly equal.
func (s *Store) Series(targetID, metric string, labels map[string]string) []model.Series {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if metric == "" {
		return []model.Series{}
	}

	if labels != nil {
		series, ok := s.series[seriesKey(targetID, metric, labels)]
		if !ok {
			return []model.Series{}
		}
		return []model.Series{series.toModel()}
	}

	matches := make([]model.Series, 0)
	for _, series := range s.series {
		if series.targetID == targetID && series.metric == metric {
			matches = append(matches, series.toModel())
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return labelsKey(matches[i].Labels) < labelsKey(matches[j].Labels)
	})
	return matches
}

func (s *storedSeries) trim(cutoff time.Time) {
	firstKept := 0
	for firstKept < len(s.points) && s.points[firstKept].ts.Before(cutoff) {
		firstKept++
	}
	if firstKept > 0 {
		s.points = append([]storedPoint(nil), s.points[firstKept:]...)
	}
}

func (s *storedSeries) toModel() model.Series {
	points := make([]model.SeriesPoint, 0, len(s.points))
	for _, point := range s.points {
		points = append(points, model.SeriesPoint{
			TS:    point.ts.UTC().Format(time.RFC3339Nano),
			Value: point.value,
		})
	}
	return model.Series{
		TargetID: s.targetID,
		Metric:   s.metric,
		Labels:   copyLabels(s.labels),
		Points:   points,
	}
}

func sampleTime(sample model.MetricSample, scrapedAt time.Time) time.Time {
	if sample.Timestamp == nil {
		return scrapedAt.UTC()
	}
	return time.UnixMilli(*sample.Timestamp).UTC()
}

func seriesKey(targetID, metric string, labels map[string]string) string {
	return targetID + "\xff" + metric + "\xff" + labelsKey(labels)
}

func labelsKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}

	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)

	var builder strings.Builder
	for _, name := range names {
		builder.WriteString(name)
		builder.WriteByte('=')
		builder.WriteString(labels[name])
		builder.WriteByte('\xff')
	}
	return builder.String()
}

func copyLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return map[string]string{}
	}

	copied := make(map[string]string, len(labels))
	for name, value := range labels {
		copied[name] = value
	}
	return copied
}
