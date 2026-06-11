package quality

import (
	"fmt"
	"sort"
	"strings"

	"metriclens/backend/internal/histogram"
	"metriclens/backend/internal/model"
)

var highCardinalityLabels = map[string]struct{}{
	"user_id":    {},
	"email":      {},
	"request_id": {},
	"session_id": {},
	"uuid":       {},
	"token":      {},
	"ip":         {},
}

func Analyze(families []model.MetricFamily) []model.MetricQualityIssue {
	issues := make([]model.MetricQualityIssue, 0)

	for _, family := range families {
		if !family.HasHelp {
			issues = append(issues, model.MetricQualityIssue{
				Severity:   model.MetricQualityInfo,
				Metric:     family.Name,
				Message:    "metric is missing HELP text",
				Suggestion: "Add a HELP line that explains what this metric measures.",
			})
		}
		if !family.HasType {
			issues = append(issues, model.MetricQualityIssue{
				Severity:   model.MetricQualityWarning,
				Metric:     family.Name,
				Message:    "metric is missing TYPE",
				Suggestion: "Add a TYPE line so tools can classify the metric correctly.",
			})
		}
		if family.Type == model.MetricTypeCounter && !strings.HasSuffix(family.Name, "_total") {
			issues = append(issues, model.MetricQualityIssue{
				Severity:   model.MetricQualityWarning,
				Metric:     family.Name,
				Message:    "counter metric does not end in _total",
				Suggestion: "Rename counters to end in _total to follow Prometheus conventions.",
			})
		}
		issues = append(issues, highCardinalityIssues(family)...)
	}

	issues = append(issues, histogramIssues(families)...)
	return issues
}

func highCardinalityIssues(family model.MetricFamily) []model.MetricQualityIssue {
	seen := map[string]struct{}{}
	issues := make([]model.MetricQualityIssue, 0)

	for _, sample := range family.Samples {
		for label := range sample.Labels {
			if _, suspicious := highCardinalityLabels[label]; !suspicious {
				continue
			}
			if _, duplicate := seen[label]; duplicate {
				continue
			}
			seen[label] = struct{}{}
			issues = append(issues, model.MetricQualityIssue{
				Severity:   model.MetricQualityWarning,
				Metric:     family.Name,
				Message:    fmt.Sprintf("label %q is likely high-cardinality", label),
				Suggestion: "Avoid per-user, per-request, or secret-like labels; put those values in logs or traces instead.",
			})
		}
	}

	sort.Slice(issues, func(i, j int) bool {
		return issues[i].Message < issues[j].Message
	})
	return issues
}

func histogramIssues(families []model.MetricFamily) []model.MetricQualityIssue {
	groups := histogram.Group(families)

	bases := make([]string, 0, len(groups))
	for base := range groups {
		bases = append(bases, base)
	}
	sort.Strings(bases)

	issues := make([]model.MetricQualityIssue, 0)
	for _, base := range bases {
		parts := groups[base]
		if !parts.Present() {
			continue
		}

		if missing := parts.Missing(); len(missing) > 0 {
			issues = append(issues, model.MetricQualityIssue{
				Severity:   model.MetricQualityWarning,
				Metric:     base,
				Message:    "histogram is missing " + strings.Join(missing, ", ") + " samples",
				Suggestion: "Expose matching _bucket, _sum, and _count samples for histograms.",
			})
		}

		if parts.SampledBuckets && !parts.HasLe {
			issues = append(issues, model.MetricQualityIssue{
				Severity:   model.MetricQualityWarning,
				Metric:     base,
				Message:    "histogram bucket samples are missing the le label",
				Suggestion: "Expose one _bucket sample per upper bound with an le label.",
			})
		} else if parts.SampledBuckets && !parts.HasInf {
			issues = append(issues, model.MetricQualityIssue{
				Severity:   model.MetricQualityWarning,
				Metric:     base,
				Message:    "histogram is missing the le=\"+Inf\" bucket",
				Suggestion: "Expose an le=\"+Inf\" bucket equal to the total count so quantiles can be computed.",
			})
		}
	}

	return issues
}
