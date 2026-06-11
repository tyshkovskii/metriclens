package classifier

import (
	"fmt"
	"sort"
	"strings"

	"metriclens/backend/internal/histogram"
	"metriclens/backend/internal/model"
)

// SeriesLookup returns the stored series for a metric so classification can
// use observed behavior over time, not just the latest scrape. A nil lookup
// disables behavioral evidence.
type SeriesLookup func(metric string) []model.Series

// behaviorMinDeltas is the minimum number of consecutive sample pairs needed
// before observed behavior is trusted as classification evidence.
const behaviorMinDeltas = 4

// labelClass distinguishes status-like labels (which enable error-rate
// panels) from other HTTP/RPC request labels.
type labelClass int

const (
	labelClassRequest labelClass = iota
	labelClassStatus
)

var httpLabelNames = map[string]labelClass{
	"method":        labelClassRequest,
	"route":         labelClassRequest,
	"path":          labelClassRequest,
	"handler":       labelClassRequest,
	"endpoint":      labelClassRequest,
	"uri":           labelClassRequest,
	"grpc_method":   labelClassRequest,
	"grpc_service":  labelClassRequest,
	"status":        labelClassStatus,
	"code":          labelClassStatus,
	"status_code":   labelClassStatus,
	"response_code": labelClassStatus,
	"grpc_code":     labelClassStatus,
}

var errorTokens = map[string]struct{}{
	"error": {}, "errors": {}, "err": {},
	"fail": {}, "failed": {}, "failure": {}, "failures": {},
	"drop": {}, "dropped": {}, "drops": {},
	"reject": {}, "rejected": {}, "rejects": {},
	"timeout": {}, "timeouts": {},
}

// wellKnown maps standard Go/process runtime metrics to curated panels.
var wellKnown = map[string]struct {
	kind  model.PanelKind
	title string
	unit  string
}{
	"go_goroutines":                 {model.PanelKindGauge, "Goroutines", ""},
	"go_threads":                    {model.PanelKindGauge, "OS threads", ""},
	"go_memstats_alloc_bytes":       {model.PanelKindGauge, "Heap allocated", "bytes"},
	"process_open_fds":              {model.PanelKindGauge, "Open file descriptors", ""},
	"process_resident_memory_bytes": {model.PanelKindGauge, "Resident memory", "bytes"},
	"process_virtual_memory_bytes":  {model.PanelKindGauge, "Virtual memory", "bytes"},
	"process_cpu_seconds_total":     {model.PanelKindCounterRate, "CPU usage", "cores"},
}

func Classify(families []model.MetricFamily, history SeriesLookup) []model.SuggestedPanel {
	histograms := histogram.Group(families)
	panels := make([]model.SuggestedPanel, 0)
	addedHistograms := map[string]struct{}{}

	for _, family := range families {
		labelNames := sampleLabelNames(family)

		switch {
		case isInfoMetric(family):
			// *_info metrics carry data in labels and a constant value of 1;
			// charting them is noise.
		case isSummaryFamily(family, labelNames):
			panels = append(panels, summaryPanels(family)...)
		case isCounter(family, history):
			panels = append(panels, counterPanels(family, labelNames, history)...)
		case family.Type == model.MetricTypeGauge:
			panels = append(panels, gaugePanel(family.Name, 0.85, "metric declares TYPE gauge"))
		default:
			if panel, ok := behaviorPanel(family, histograms, history); ok {
				panels = append(panels, panel)
			}
		}

		if parts, ok := histograms[family.Name]; ok && parts.Present() {
			panels = append(panels, histogramPanel(family.Name, parts))
			addedHistograms[family.Name] = struct{}{}
		}
	}

	bases := make([]string, 0, len(histograms))
	for base := range histograms {
		if _, ok := addedHistograms[base]; ok {
			continue
		}
		bases = append(bases, base)
	}
	sort.Strings(bases)
	for _, base := range bases {
		parts := histograms[base]
		if parts.Present() {
			panels = append(panels, histogramPanel(base, parts))
		}
	}

	return panels
}

// counterPanels suggests panels for a counter: HTTP/gRPC request and error
// rates when the labels look request-shaped, a plain rate panel otherwise.
// The plain panel is suppressed when an HTTP panel covers the same metric, so
// the same rate is never charted twice.
func counterPanels(family model.MetricFamily, labelNames []string, history SeriesLookup) []model.SuggestedPanel {
	if score, evidence := httpEvidence(family.Name, labelNames); score >= 2 {
		return httpPanels(family.Name, labelNames, score, evidence)
	}

	title := "Rate over time"
	confidence, reason := counterConfidence(family)
	unit := rateUnit(family.Name)
	if family.Type != model.MetricTypeCounter {
		if b := observe(history, family.Name); b.counterLike() {
			confidence = 0.8
			reason = fmt.Sprintf("%s and its value never decreased across %d recent samples", reason, b.deltas)
		}
	}
	if isErrorCounter(family.Name) {
		title = "Error rate"
	}
	if known, ok := wellKnown[family.Name]; ok && known.kind == model.PanelKindCounterRate {
		title, unit = known.title, known.unit
		confidence, reason = 0.95, "well-known runtime metric"
	}

	return []model.SuggestedPanel{{
		ID:         panelID(model.PanelKindCounterRate, family.Name),
		Title:      title,
		Kind:       model.PanelKindCounterRate,
		Metric:     family.Name,
		Confidence: confidence,
		Reason:     reason,
		Unit:       unit,
	}}
}

func gaugePanel(metric string, confidence float64, reason string) model.SuggestedPanel {
	unit := metricUnit(metric)
	title := unitTitle(unit)
	if known, ok := wellKnown[metric]; ok && known.kind == model.PanelKindGauge {
		title, unit = known.title, known.unit
		confidence, reason = 0.95, "well-known runtime metric"
	}

	return model.SuggestedPanel{
		ID:         panelID(model.PanelKindGauge, metric),
		Title:      title,
		Kind:       model.PanelKindGauge,
		Metric:     metric,
		Confidence: confidence,
		Reason:     reason,
		Unit:       unit,
	}
}

func httpPanels(metric string, labelNames []string, score int, evidence string) []model.SuggestedPanel {
	protocol := "HTTP"
	if strings.HasPrefix(metric, "grpc_") {
		protocol = "gRPC"
	}
	confidence := 0.66 + 0.06*float64(score)
	if confidence > 0.9 {
		confidence = 0.9
	}

	panels := []model.SuggestedPanel{{
		ID:         panelID(model.PanelKindHTTPRate, metric),
		Title:      protocol + " request rate",
		Kind:       model.PanelKindHTTPRate,
		Metric:     metric,
		Confidence: confidence,
		Reason:     evidence,
		Labels:     labelNames,
	}}
	if hasStatusLabel(labelNames) {
		panels = append(panels, model.SuggestedPanel{
			ID:         panelID(model.PanelKindHTTPErrorRate, metric),
			Title:      protocol + " error rate",
			Kind:       model.PanelKindHTTPErrorRate,
			Metric:     metric,
			Confidence: confidence - 0.06,
			Reason:     evidence + "; status/code label enables error filtering",
			Labels:     labelNames,
		})
	}
	return panels
}

func summaryPanels(family model.MetricFamily) []model.SuggestedPanel {
	confidence, reason := 0.85, "metric declares TYPE summary"
	if family.Type != model.MetricTypeSummary {
		confidence, reason = 0.6, "samples expose a quantile label"
	}

	title, unit := distributionTitle(family.Name, "quantiles")
	panels := []model.SuggestedPanel{{
		ID:         panelID(model.PanelKindSummaryQuantiles, family.Name),
		Title:      title,
		Kind:       model.PanelKindSummaryQuantiles,
		Metric:     family.Name,
		Confidence: confidence,
		Reason:     reason,
		Unit:       unit,
	}}

	countMetric := family.Name + "_count"
	if hasSampleMetric(family, countMetric) {
		panels = append(panels, model.SuggestedPanel{
			ID:         panelID(model.PanelKindCounterRate, countMetric),
			Title:      "Throughput",
			Kind:       model.PanelKindCounterRate,
			Metric:     countMetric,
			Confidence: confidence,
			Reason:     "rate of the summary's _count samples",
		})
	}
	return panels
}

func histogramPanel(metric string, parts histogram.Parts) model.SuggestedPanel {
	title, unit := distributionTitle(metric, "p95")
	return model.SuggestedPanel{
		ID:         panelID(model.PanelKindHistogramLatency, metric),
		Title:      title,
		Kind:       model.PanelKindHistogramLatency,
		Metric:     metric,
		Confidence: histogramConfidence(parts),
		Reason:     histogramReason(parts),
		Unit:       unit,
	}
}

// behaviorPanel classifies an untyped metric from how its stored values moved
// over recent scrapes: never decreasing means counter-like, moving both ways
// means gauge. Histogram components are left to the histogram panel.
func behaviorPanel(family model.MetricFamily, histograms map[string]histogram.Parts, history SeriesLookup) (model.SuggestedPanel, bool) {
	if family.Type != model.MetricTypeUntyped && family.Type != "" {
		return model.SuggestedPanel{}, false
	}
	if base, _, ok := histogram.SplitName(family.Name); ok && histograms[base].Present() {
		return model.SuggestedPanel{}, false
	}

	b := observe(history, family.Name)
	switch {
	case b.counterLike():
		return model.SuggestedPanel{
			ID:         panelID(model.PanelKindCounterRate, family.Name),
			Title:      "Rate over time",
			Kind:       model.PanelKindCounterRate,
			Metric:     family.Name,
			Confidence: 0.6,
			Reason:     fmt.Sprintf("untyped metric only increased across %d recent samples", b.deltas),
			Unit:       rateUnit(family.Name),
		}, true
	case b.gaugeLike(1):
		reason := fmt.Sprintf("untyped metric moved both up and down across %d recent samples", b.deltas)
		return gaugePanel(family.Name, 0.65, reason), true
	}
	return model.SuggestedPanel{}, false
}

type behavior struct {
	deltas    int
	increases int
	decreases int
}

func observe(history SeriesLookup, metric string) behavior {
	var b behavior
	if history == nil {
		return b
	}
	for _, series := range history(metric) {
		for i := 1; i < len(series.Points); i++ {
			b.deltas++
			switch delta := series.Points[i].Value - series.Points[i-1].Value; {
			case delta > 0:
				b.increases++
			case delta < 0:
				b.decreases++
			}
		}
	}
	return b
}

func (b behavior) counterLike() bool {
	return b.deltas >= behaviorMinDeltas && b.decreases == 0 && b.increases > 0
}

func (b behavior) gaugeLike(minDecreases int) bool {
	return b.deltas >= behaviorMinDeltas && b.decreases >= minDecreases
}

func histogramConfidence(parts histogram.Parts) float64 {
	switch {
	case parts.Typed && parts.HasBucket:
		return 0.85
	case parts.Typed, parts.HasBucket && parts.HasSum && parts.HasCount:
		return 0.75
	default:
		return 0.65
	}
}

func histogramReason(parts histogram.Parts) string {
	if parts.Typed && parts.HasBucket {
		return "metric declares TYPE histogram and has bucket samples"
	}
	if parts.Typed {
		return "metric declares TYPE histogram"
	}
	if parts.HasSum && parts.HasCount {
		return "metric has histogram bucket, sum, and count samples"
	}
	return "metric has histogram bucket samples"
}

// isCounter reports whether a family should be charted as a rate. A declared
// type always wins; the _total naming convention only applies to untyped
// metrics, and observed decreases (beyond a single counter reset) override
// the name.
func isCounter(family model.MetricFamily, history SeriesLookup) bool {
	if family.Type == model.MetricTypeCounter {
		return true
	}
	if family.Type != model.MetricTypeUntyped && family.Type != "" {
		return false
	}
	if !strings.HasSuffix(family.Name, "_total") {
		return false
	}
	return !observe(history, family.Name).gaugeLike(2)
}

func counterConfidence(family model.MetricFamily) (float64, string) {
	if family.Type == model.MetricTypeCounter {
		return 0.9, "metric declares TYPE counter"
	}
	return 0.72, "metric name looks counter-like because it ends with _total"
}

// httpEvidence scores how request-shaped a counter looks: one point for an
// HTTP/gRPC-style name plus one per known request/status label.
func httpEvidence(metric string, labelNames []string) (int, string) {
	score := 0
	parts := make([]string, 0, 2)
	if strings.HasPrefix(metric, "http_") || strings.HasPrefix(metric, "grpc_") || strings.Contains(metric, "request") {
		score++
		parts = append(parts, "request-style name")
	}

	matched := make([]string, 0, len(labelNames))
	for _, label := range labelNames {
		if _, ok := httpLabelNames[label]; ok {
			matched = append(matched, label)
		}
	}
	if len(matched) > 0 {
		score += len(matched)
		parts = append(parts, "labels "+strings.Join(matched, ", "))
	}

	return score, "counter with " + strings.Join(parts, " and ")
}

func hasStatusLabel(labelNames []string) bool {
	for _, label := range labelNames {
		if class, ok := httpLabelNames[label]; ok && class == labelClassStatus {
			return true
		}
	}
	return false
}

func isErrorCounter(metric string) bool {
	for _, token := range strings.Split(metric, "_") {
		if _, ok := errorTokens[token]; ok {
			return true
		}
	}
	return false
}

func isSummaryFamily(family model.MetricFamily, labelNames []string) bool {
	if family.Type == model.MetricTypeSummary {
		return true
	}
	if family.Type != model.MetricTypeUntyped && family.Type != "" {
		return false
	}
	for _, label := range labelNames {
		if label == "quantile" {
			return true
		}
	}
	return false
}

// isInfoMetric detects *_info metrics: gauges whose value is always 1 and
// whose payload lives in labels (build_info, go_info, ...).
func isInfoMetric(family model.MetricFamily) bool {
	if !strings.HasSuffix(family.Name, "_info") {
		return false
	}
	if family.Type != model.MetricTypeGauge && family.Type != model.MetricTypeUntyped && family.Type != "" {
		return false
	}
	if len(family.Samples) == 0 {
		return false
	}
	for _, sample := range family.Samples {
		if sample.Value != 1 {
			return false
		}
	}
	return true
}

func hasSampleMetric(family model.MetricFamily, metric string) bool {
	for _, sample := range family.Samples {
		if sample.Metric == metric {
			return true
		}
	}
	return false
}

// metricUnit derives a unit from Prometheus naming conventions.
func metricUnit(metric string) string {
	for _, unit := range []string{"bytes", "seconds", "ratio", "percent", "celsius"} {
		if strings.HasSuffix(metric, "_"+unit) {
			return unit
		}
	}
	return ""
}

// rateUnit is the unit of a counter's per-second rate, e.g. bytes/s for a
// *_bytes_total counter.
func rateUnit(metric string) string {
	if unit := metricUnit(strings.TrimSuffix(metric, "_total")); unit != "" {
		return unit + "/s"
	}
	return ""
}

func unitTitle(unit string) string {
	if unit == "" {
		return "Value over time"
	}
	return strings.ToUpper(unit[:1]) + unit[1:] + " over time"
}

// distributionTitle titles a histogram/summary panel from what the base name
// says it measures: durations become latency, byte sizes become size.
func distributionTitle(metric, suffix string) (string, string) {
	unit := metricUnit(metric)
	switch {
	case strings.Contains(metric, "duration") || strings.Contains(metric, "latency") || unit == "seconds":
		return "Latency " + suffix, unit
	case unit == "bytes" || strings.Contains(metric, "size"):
		return "Size " + suffix, unit
	default:
		return strings.ToUpper(suffix[:1]) + suffix[1:] + " over time", unit
	}
}

func sampleLabelNames(family model.MetricFamily) []string {
	seen := map[string]struct{}{}
	for _, sample := range family.Samples {
		for label := range sample.Labels {
			seen[label] = struct{}{}
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func panelID(kind model.PanelKind, metric string) string {
	return fmt.Sprintf("%s:%s", kind, metric)
}
