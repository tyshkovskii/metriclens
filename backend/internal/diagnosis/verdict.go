package diagnosis

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"metriclens/backend/internal/classifier"
	"metriclens/backend/internal/model"
)

// Verdict thresholds. Each window needs minVerdictEvents requests or
// observations, a pooled two-proportion z-test must reach verdictZ, and the
// shift must also be large enough to matter, so sampling noise in short local
// runs is reported as unchanged rather than as a regression.
const (
	minVerdictEvents        = 50
	verdictZ                = 3.0
	minErrorRatioChange     = 0.01
	minLatencyRelativeShift = 0.10
)

type timeRange struct {
	start, end time.Time
}

// errorRatioFinding compares the share of server-error requests in the
// baseline and compared windows of one request counter.
func errorRatioFinding(target model.Target, metric string, series []model.Series, baseline, compared timeRange) Finding {
	finding := Finding{Signal: "error_ratio", TargetID: target.ID, Service: target.ServiceName, Metric: metric}
	baseErrors, baseTotal, baseOK := requestCounts(series, baseline)
	errors, total, ok := requestCounts(series, compared)
	if !baseOK || !ok || baseTotal < minVerdictEvents || total < minVerdictEvents {
		return inconclusiveFinding(finding, fmt.Sprintf("server error ratio inconclusive: %.0f baseline and %.0f compared requests with finite samples; each window needs at least %d", baseTotal, total, minVerdictEvents))
	}
	before, after := baseErrors/baseTotal, errors/total
	z := twoProportionZ(baseErrors, baseTotal, errors, total)
	outcome := verdict(z, after-before >= minErrorRatioChange, before-after >= minErrorRatioChange)
	finding.Baseline, finding.Candidate = floatPtr(before), floatPtr(after)
	finding.Message = fmt.Sprintf("server error ratio %.1f%% before, %.1f%% after (%.0f and %.0f requests): %s", before*100, after*100, baseTotal, total, outcome)
	return withVerdict(finding, outcome)
}

// latencyFinding compares p95 of one histogram between the baseline and
// compared windows. Significance comes from the share of observations above
// the finite bucket bound nearest the baseline p95, so interpolation noise
// inside one coarse bucket cannot produce a verdict on its own.
func latencyFinding(target model.Target, metric, unit string, buckets []model.Series, baseline, compared timeRange) Finding {
	finding := Finding{Signal: "latency_p95", TargetID: target.ID, Service: target.ServiceName, Metric: metric, evidenceMetrics: histogramSampleNames(metric)}
	baseCounts, baseOK := bucketCounts(buckets, baseline)
	counts, ok := bucketCounts(buckets, compared)
	bounds := slices.Sorted(maps.Keys(baseCounts))
	if !baseOK || !ok || !sameBounds(bounds, counts) || len(bounds) < 2 || !math.IsInf(bounds[len(bounds)-1], 1) {
		return inconclusiveFinding(finding, "p95 inconclusive: bucket samples are non-finite, missing, or use different bounds in the two windows")
	}
	baseCumulative, cumulative := cumulativeCounts(bounds, baseCounts), cumulativeCounts(bounds, counts)
	baseTotal, total := baseCumulative[len(bounds)-1], cumulative[len(bounds)-1]
	if baseTotal < minVerdictEvents || total < minVerdictEvents {
		return inconclusiveFinding(finding, fmt.Sprintf("p95 inconclusive: %.0f baseline and %.0f compared observations; each window needs at least %d", baseTotal, total, minVerdictEvents))
	}
	before, beforeBeyond := bucketP95(bounds, baseCumulative)
	after, afterBeyond := bucketP95(bounds, cumulative)
	if beforeBeyond && afterBeyond {
		return inconclusiveFinding(finding, fmt.Sprintf("p95 inconclusive: above the largest finite bucket bound %s in both windows", formatQuantile(before, false, unit)))
	}
	split := tailBound(bounds, baseCumulative)
	baseSlow, slow := baseTotal-baseCumulative[split], total-cumulative[split]
	z := twoProportionZ(baseSlow, baseTotal, slow, total)
	// A p95 past the largest finite bound is only a lower bound, so crossing
	// that bound counts as a meaningful shift in its direction.
	worse := after >= before*(1+minLatencyRelativeShift) || (afterBeyond && !beforeBeyond)
	better := after <= before*(1-minLatencyRelativeShift) || (beforeBeyond && !afterBeyond)
	outcome := verdict(z, worse, better)
	finding.Baseline, finding.Candidate = floatPtr(before), floatPtr(after)
	finding.Message = fmt.Sprintf("p95 %s before, %s after; share above %s went from %.1f%% to %.1f%% (%.0f and %.0f observations): %s",
		formatQuantile(before, beforeBeyond, unit), formatQuantile(after, afterBeyond, unit), formatQuantile(bounds[split], false, unit),
		baseSlow/baseTotal*100, slow/total*100, baseTotal, total, outcome)
	return withVerdict(finding, outcome)
}

// verdict requires both a significant z statistic and a practically
// meaningful shift in the same direction.
func verdict(z float64, worse, better bool) string {
	switch {
	case z >= verdictZ && worse:
		return "regressed"
	case z <= -verdictZ && better:
		return "improved"
	default:
		return "unchanged"
	}
}

func withVerdict(finding Finding, outcome string) Finding {
	finding.Verdict = outcome
	finding.Severity = "info"
	switch outcome {
	case "regressed":
		finding.Severity = "warning"
		finding.score = 600
	case "improved":
		finding.score = 20
	case "inconclusive":
		finding.score = 5
	default:
		finding.score = 1
	}
	// Messages carry the measured values, so the identity uses the verdict
	// instead and stays the same across repeated comparisons.
	finding.stableIdentity = strings.Join([]string{finding.Signal, finding.TargetID, finding.Service, finding.Metric, outcome}, "\x00")
	return finding
}

func inconclusiveFinding(finding Finding, message string) Finding {
	finding.Message = message
	return withVerdict(finding, "inconclusive")
}

// requestCounts sums reset-aware request increases over a window and the
// part of them whose status label marks a server error.
func requestCounts(series []model.Series, window timeRange) (errors, total float64, ok bool) {
	for _, item := range series {
		increase, _, finite := seriesIncrease(item, window)
		if !finite {
			return 0, 0, false
		}
		total += increase
		if classifier.ServerError(item.Labels) {
			errors += increase
		}
	}
	return errors, total, true
}

// bucketCounts sums reset-aware histogram bucket increases over a window by
// upper bound, aggregating every other label. Only buckets sampled inside the
// window count, so a bucket layout change between windows is detectable.
func bucketCounts(buckets []model.Series, window timeRange) (map[float64]float64, bool) {
	counts := map[float64]float64{}
	for _, item := range buckets {
		bound, err := strconv.ParseFloat(item.Labels["le"], 64)
		if err != nil {
			continue
		}
		increase, sampled, finite := seriesIncrease(item, window)
		if !finite {
			return nil, false
		}
		if sampled {
			counts[bound] += increase
		}
	}
	return counts, true
}

// seriesIncrease is a counter's reset-aware increase over (start, end],
// measured from its last sample at or before start. Adjacent windows therefore
// split growth exactly, even when a sample lands on their shared boundary.
// sampled reports a sample inside the window; finite is false when a sample
// in that span is NaN or infinite.
func seriesIncrease(item model.Series, window timeRange) (increase float64, sampled, finite bool) {
	var points []parsedPoint
	for _, point := range parsedPoints(item.Points) {
		if point.ts.After(window.end) {
			break
		}
		if point.ts.After(window.start) {
			sampled = true
		} else {
			points = points[:0]
		}
		points = append(points, point)
	}
	for _, point := range points {
		if math.IsNaN(point.value) || math.IsInf(point.value, 0) {
			return 0, sampled, false
		}
	}
	return counterIncrease(points), sampled, true
}

func sameBounds(bounds []float64, counts map[float64]float64) bool {
	if len(bounds) != len(counts) {
		return false
	}
	for _, bound := range bounds {
		if _, ok := counts[bound]; !ok {
			return false
		}
	}
	return true
}

// cumulativeCounts keeps bucket counts monotonic, as histogram_quantile does,
// so a reset seen by one bucket series cannot invert the distribution.
func cumulativeCounts(bounds []float64, counts map[float64]float64) []float64 {
	cumulative := make([]float64, 0, len(bounds))
	for _, bound := range bounds {
		count := counts[bound]
		if len(cumulative) > 0 && count < cumulative[len(cumulative)-1] {
			count = cumulative[len(cumulative)-1]
		}
		cumulative = append(cumulative, count)
	}
	return cumulative
}

// bucketP95 interpolates linearly inside the bucket holding the 95th
// percentile rank, as PromQL's histogram_quantile does. A rank in the +Inf
// bucket yields the highest finite bound, and beyond reports that the true
// value is larger.
func bucketP95(bounds, cumulative []float64) (value float64, beyond bool) {
	rank := 0.95 * cumulative[len(cumulative)-1]
	lower, below := 0.0, 0.0
	for i, bound := range bounds {
		if cumulative[i] < rank {
			lower, below = bound, cumulative[i]
			continue
		}
		switch {
		case math.IsInf(bound, 1):
			return lower, true
		case i == 0 && bound <= 0, cumulative[i] == below:
			return bound, false
		}
		return lower + (bound-lower)*(rank-below)/(cumulative[i]-below), false
	}
	return lower, true
}

// tailBound picks the finite bound whose baseline share of observations at or
// below it is closest to 95%.
func tailBound(bounds, cumulative []float64) int {
	total := cumulative[len(cumulative)-1]
	best := 0
	for i := 1; i < len(bounds)-1; i++ {
		if math.Abs(cumulative[i]/total-0.95) < math.Abs(cumulative[best]/total-0.95) {
			best = i
		}
	}
	return best
}

// twoProportionZ is the pooled z statistic for a change in share from
// before/beforeTotal to after/afterTotal; it is zero when both are 0 or 1.
func twoProportionZ(before, beforeTotal, after, afterTotal float64) float64 {
	pooled := (before + after) / (beforeTotal + afterTotal)
	se := math.Sqrt(pooled * (1 - pooled) * (1/beforeTotal + 1/afterTotal))
	if se == 0 {
		return 0
	}
	return (after/afterTotal - before/beforeTotal) / se
}

func formatQuantile(value float64, beyond bool, unit string) string {
	text := strconv.FormatFloat(value, 'g', 3, 64)
	if unit != "" {
		text += " " + unit
	}
	if beyond {
		return "at least " + text
	}
	return text
}

// histogramSampleNames are the stored sample names of a histogram, which is
// what metric evidence requests must name.
func histogramSampleNames(base string) []string {
	return []string{base + "_bucket", base + "_count", base + "_sum"}
}
