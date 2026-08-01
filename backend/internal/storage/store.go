package storage

import (
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	"metriclens/backend/internal/model"
)

const DefaultRetention = 15 * time.Minute

type Store struct {
	mu        sync.RWMutex
	retention time.Duration
	series    map[string]*storedSeries
}

type storedSeries struct {
	historyID string
	targetID  string
	metric    string
	labels    map[string]string
	points    []storedPoint
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
	s.RecordWithIdentity(targetID, targetID, families, scrapedAt)
}

// RecordWithIdentity stores samples under an internal stable identity while
// retaining the current public target ID for API responses.
func (s *Store) RecordWithIdentity(historyID, targetID string, families []model.MetricFamily, scrapedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if historyID == "" {
		historyID = targetID
	}
	scrapedAt = scrapedAt.UTC()
	cutoff := scrapedAt.Add(-s.retention)
	for _, family := range families {
		for _, sample := range family.Samples {
			pointTime := sampleTime(sample, scrapedAt)
			key := seriesKey(historyID, sample.Metric, sample.Labels)
			series, ok := s.series[key]
			if !ok {
				series = &storedSeries{
					historyID: historyID,
					targetID:  targetID,
					metric:    sample.Metric,
					labels:    maps.Clone(sample.Labels),
				}
				s.series[key] = series
			}
			series.targetID = targetID
			series.points = append(series.points, storedPoint{ts: pointTime, value: sample.Value})
			series.trim(cutoff)
		}
	}
}

// Series returns stored series for a metric. A nil labels map matches every
// series of the metric; a non-nil map (even an empty one) matches only the
// series whose label set is exactly equal.
func (s *Store) Series(targetID, metric string, labels map[string]string) []model.Series {
	return s.SeriesFor(targetID, targetID, metric, labels)
}

// SeriesFor reads a metric by stable identity and presents the requested
// current public target ID in each returned series.
func (s *Store) SeriesFor(historyID, targetID, metric string, labels map[string]string) []model.Series {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if metric == "" {
		return []model.Series{}
	}
	if historyID == "" {
		historyID = targetID
	}

	if labels != nil {
		series, ok := s.series[seriesKey(historyID, metric, labels)]
		if !ok {
			return []model.Series{}
		}
		return []model.Series{series.toModel(targetID)}
	}

	matches := make([]model.Series, 0)
	for _, series := range s.series {
		if series.historyID == historyID && series.metric == metric {
			matches = append(matches, series.toModel(targetID))
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

func (s *storedSeries) toModel(targetID string) model.Series {
	points := make([]model.SeriesPoint, 0, len(s.points))
	for _, point := range s.points {
		points = append(points, model.SeriesPoint{
			TS:    point.ts.UTC().Format(time.RFC3339Nano),
			Value: point.value,
		})
	}
	return model.Series{
		TargetID: targetID,
		Metric:   s.metric,
		Labels:   maps.Clone(s.labels),
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
