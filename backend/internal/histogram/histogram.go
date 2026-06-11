// Package histogram groups Prometheus metric families and their component
// samples (_bucket, _sum, _count) by the histogram base name they belong to.
//
// Both panel classification and metric quality analysis need this same view of
// the data, so the grouping rules live here once rather than being duplicated
// in each caller.
package histogram

import (
	"strings"

	"metriclens/backend/internal/model"
)

// partSuffixes are the component suffixes of a Prometheus histogram, in the
// canonical order used when reporting them.
var partSuffixes = []string{"_bucket", "_sum", "_count"}

// Parts records which component samples a histogram base name exposes and
// whether any family for that name was explicitly declared TYPE histogram.
type Parts struct {
	Typed     bool
	HasBucket bool
	HasSum    bool
	HasCount  bool
	// SampledBuckets is set only when actual _bucket samples were seen, as
	// opposed to a family merely named *_bucket; le-label checks only make
	// sense for samples.
	SampledBuckets bool
	HasLe          bool
	HasInf         bool
}

// Present reports whether the base name looks like a histogram at all: it was
// either declared TYPE histogram or exposes bucket samples.
func (p Parts) Present() bool {
	return p.Typed || p.HasBucket
}

// Missing lists the component suffixes that are not exposed, in canonical order.
func (p Parts) Missing() []string {
	missing := make([]string, 0, len(partSuffixes))
	if !p.HasBucket {
		missing = append(missing, "_bucket")
	}
	if !p.HasSum {
		missing = append(missing, "_sum")
	}
	if !p.HasCount {
		missing = append(missing, "_count")
	}
	return missing
}

// Group buckets the given families and all of their samples by histogram base
// name. A base name appears in the result if any family or sample references
// it, even when the histogram is incomplete.
func Group(families []model.MetricFamily) map[string]Parts {
	groups := map[string]Parts{}
	for _, family := range families {
		if family.Type == model.MetricTypeHistogram {
			parts := groups[family.Name]
			parts.Typed = true
			groups[family.Name] = parts
		}
		recordName(groups, family.Name)
		for _, sample := range family.Samples {
			recordSample(groups, sample.Metric, sample.Labels)
		}
	}
	return groups
}

// SplitName splits a histogram component metric name into its base name and
// component suffix (e.g. "request_duration_seconds_bucket" ->
// "request_duration_seconds", "_bucket"). ok is false for names that are not
// histogram components.
func SplitName(metric string) (base, suffix string, ok bool) {
	for _, candidate := range partSuffixes {
		if strings.HasSuffix(metric, candidate) {
			return strings.TrimSuffix(metric, candidate), candidate, true
		}
	}
	return "", "", false
}

func recordName(groups map[string]Parts, metric string) {
	base, suffix, ok := SplitName(metric)
	if !ok {
		return
	}

	parts := groups[base]
	markSuffix(&parts, suffix)
	groups[base] = parts
}

func recordSample(groups map[string]Parts, metric string, labels map[string]string) {
	base, suffix, ok := SplitName(metric)
	if !ok {
		return
	}

	parts := groups[base]
	markSuffix(&parts, suffix)
	if suffix == "_bucket" {
		parts.SampledBuckets = true
		if le, ok := labels["le"]; ok {
			parts.HasLe = true
			if le == "+Inf" {
				parts.HasInf = true
			}
		}
	}
	groups[base] = parts
}

func markSuffix(parts *Parts, suffix string) {
	switch suffix {
	case "_bucket":
		parts.HasBucket = true
	case "_sum":
		parts.HasSum = true
	case "_count":
		parts.HasCount = true
	}
}
