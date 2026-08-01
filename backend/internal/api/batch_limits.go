package api

import (
	"errors"
	"sort"
	"strconv"
	"time"

	"metriclens/backend/internal/model"
)

// Batch-series limits keep an agent's evidence request bounded while leaving
// the response shape unchanged. The maxPoints query defaults to
// DefaultBatchMaxPoints and is capped at MaxBatchPointsPerSeries.
const (
	MaxBatchMetrics         = 10
	MaxBatchSeriesPerMetric = 20
	MaxBatchSeriesTotal     = 50
	MaxBatchPointsPerSeries = 200
	MaxBatchPointsTotal     = 500
	DefaultBatchMaxPoints   = 100
)

type batchSeriesStats struct {
	seriesCount     int
	pointCount      int
	seriesTruncated bool
	pointsTruncated bool
}

func parseBatchMaxPoints(raw string) (int, error) {
	if raw == "" {
		return DefaultBatchMaxPoints, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0, errors.New("maxPoints query parameter must be a positive integer")
	}
	if parsed > MaxBatchPointsPerSeries {
		return MaxBatchPointsPerSeries, nil
	}
	return parsed, nil
}

func limitBatchSeries(series []model.Series, maxPoints int) ([]model.Series, batchSeriesStats) {
	ordered := append([]model.Series(nil), series...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Metric != ordered[j].Metric {
			return ordered[i].Metric < ordered[j].Metric
		}
		return labelsKey(ordered[i].Labels) < labelsKey(ordered[j].Labels)
	})

	limited := make([]model.Series, 0, minInt(len(ordered), MaxBatchSeriesTotal))
	stats := batchSeriesStats{}
	metricCounts := map[string]int{}
	for _, item := range ordered {
		points := sortedBatchPoints(item.Points)
		if len(points) == 0 {
			continue
		}
		if metricCounts[item.Metric] >= MaxBatchSeriesPerMetric {
			stats.seriesTruncated = true
			continue
		}
		if len(limited) >= MaxBatchSeriesTotal {
			stats.seriesTruncated = true
			continue
		}
		metricCounts[item.Metric]++
		if len(points) > maxPoints {
			stats.pointsTruncated = true
		}
		remaining := MaxBatchPointsTotal - stats.pointCount
		if remaining <= 0 {
			stats.pointsTruncated = stats.pointsTruncated || len(points) > 0
			stats.seriesTruncated = true
			continue
		}
		pointLimit := maxPoints
		if pointLimit > remaining {
			pointLimit = remaining
			if len(points) > pointLimit {
				stats.pointsTruncated = true
			}
		}
		item.Points = downsampleBatchPoints(points, pointLimit)
		if len(item.Points) == 0 {
			continue
		}
		limited = append(limited, item)
		stats.pointCount += len(item.Points)
	}
	stats.seriesCount = len(limited)
	return limited, stats
}

func sortedBatchPoints(points []model.SeriesPoint) []model.SeriesPoint {
	ordered := append([]model.SeriesPoint(nil), points...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339Nano, ordered[i].TS)
		right, rightErr := time.Parse(time.RFC3339Nano, ordered[j].TS)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.Before(right)
		}
		if ordered[i].TS != ordered[j].TS {
			return ordered[i].TS < ordered[j].TS
		}
		return ordered[i].Value < ordered[j].Value
	})
	return ordered
}

func downsampleBatchPoints(points []model.SeriesPoint, limit int) []model.SeriesPoint {
	if limit <= 0 || len(points) == 0 {
		return nil
	}
	if len(points) <= limit {
		return append([]model.SeriesPoint(nil), points...)
	}
	if limit == 1 {
		return []model.SeriesPoint{points[len(points)-1]}
	}
	result := make([]model.SeriesPoint, 0, limit)
	for index := 0; index < limit; index++ {
		position := index * (len(points) - 1) / (limit - 1)
		result = append(result, points[position])
	}
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
