package diagnosis

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"sort"
	"strings"
	"time"

	"metriclens/backend/internal/classifier"
	"metriclens/backend/internal/model"
	"metriclens/backend/internal/quality"
)

const (
	DefaultWindow = 5 * time.Minute
	DefaultLimit  = 10
	MaxLimit      = 50
)

// Source is the small read-only surface needed to build a report.
type Source interface {
	Targets() []model.Target
	TargetMetrics(string) (model.TargetMetricsResponse, bool)
	TargetSeries(string, string, map[string]string) []model.Series
}

type lifecycleSource interface {
	LifecycleEvents(start, end time.Time) []model.LifecycleEvent
}

type Window struct {
	Start      string `json:"start"`
	End        string `json:"end"`
	DurationMs int64  `json:"durationMs"`
}

type TargetSummary struct {
	Total   int `json:"total"`
	Up      int `json:"up"`
	Down    int `json:"down"`
	Healthy int `json:"healthy"`
}

type Report struct {
	Status   string        `json:"status"`
	Window   Window        `json:"window"`
	Targets  TargetSummary `json:"targets"`
	Findings []Finding     `json:"findings"`
	Omitted  int           `json:"omitted"`
	From     *MarkerRef    `json:"from,omitempty"`
	To       *MarkerRef    `json:"to,omitempty"`
}

type MarkerRef struct {
	ID          string `json:"id"`
	CreatedAt   string `json:"createdAt"`
	Name        string `json:"name,omitempty"`
	ClientRunID string `json:"clientRunId,omitempty"`
}

type Finding struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Signal   string `json:"signal"`
	TargetID string `json:"targetId,omitempty"`
	Service  string `json:"service,omitempty"`
	Metric   string `json:"metric,omitempty"`
	Message  string `json:"message"`

	Delta   *float64 `json:"delta,omitempty"`
	Rate    *float64 `json:"rate,omitempty"`
	Current *float64 `json:"current,omitempty"`
	Change  *float64 `json:"change,omitempty"`
	Peak    *float64 `json:"peak,omitempty"`

	Suggestion     string              `json:"suggestion,omitempty"`
	Evidence       *EvidenceDescriptor `json:"evidence,omitempty"`
	score          float64             `json:"-"`
	stableIdentity string              `json:"-"`
}

type EvidenceDescriptor struct {
	TargetID string   `json:"targetId"`
	Metrics  []string `json:"metrics"`
	Start    string   `json:"start"`
	End      string   `json:"end"`
}

// BuildOptions controls report filtering. Empty slices preserve Build's
// original unfiltered behavior.
type BuildOptions struct {
	Severities  []string
	Services    []string
	ChangedOnly bool
}

// Build creates a compact, deterministic report over the exact requested
// window. The source is queried only for current target metadata and selected
// classifier metrics; raw series are never included in the result.
func Build(source Source, now time.Time, window time.Duration, limit int) Report {
	return BuildWithOptions(source, now, window, limit, BuildOptions{})
}

func BuildWithOptions(source Source, now time.Time, window time.Duration, limit int, options BuildOptions) Report {
	if window <= 0 {
		window = DefaultWindow
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	now = now.UTC()
	start := now.Add(-window)
	report := Report{
		Status:   "ok",
		Window:   Window{Start: start.Format(time.RFC3339Nano), End: now.Format(time.RFC3339Nano), DurationMs: window.Milliseconds()},
		Findings: []Finding{},
	}

	targets := append([]model.Target(nil), source.Targets()...)
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].ServiceName != targets[j].ServiceName {
			return targets[i].ServiceName < targets[j].ServiceName
		}
		if targets[i].ContainerName != targets[j].ContainerName {
			return targets[i].ContainerName < targets[j].ContainerName
		}
		return targets[i].ID < targets[j].ID
	})
	for _, target := range targets {
		if !options.allowsService(target.ServiceName) {
			continue
		}
		report.Targets.Total++
		if target.Status == model.TargetStatusUp {
			report.Targets.Up++
			if target.LastError == "" {
				report.Targets.Healthy++
			}
		} else {
			report.Targets.Down++
		}

		if target.Status != model.TargetStatusUp || target.LastError != "" {
			message := strings.TrimSpace(target.LastError)
			if message == "" {
				message = "target is down"
			}
			report.Findings = append(report.Findings, Finding{
				Severity: "error",
				Signal:   "scrape_error",
				TargetID: target.ID,
				Service:  target.ServiceName,
				Message:  message,
				score:    1_000,
			})
		}

		metrics, ok := source.TargetMetrics(target.ID)
		if !ok {
			continue
		}
		for _, issue := range quality.Analyze(metrics.Families) {
			if issue.Severity != model.MetricQualityWarning {
				continue
			}
			report.Findings = append(report.Findings, Finding{
				Severity:   "warning",
				Signal:     "quality_warning",
				TargetID:   target.ID,
				Service:    target.ServiceName,
				Metric:     issue.Metric,
				Message:    issue.Message,
				Suggestion: issue.Suggestion,
				score:      500,
			})
		}

		history := func(metric string) []model.Series {
			return boundedSeries(source.TargetSeries(target.ID, metric, nil), start, now)
		}
		for _, observation := range selectedObservations(metrics.Families, history) {
			series := history(observation.metric)
			if len(series) == 0 {
				series = fallbackSeries(metrics.Families, observation.metric, now)
			}
			if observation.kind == model.PanelKindGauge {
				if finding, ok := gaugeFinding(target, observation.metric, series); ok {
					report.Findings = append(report.Findings, finding)
				}
				continue
			}
			if finding, ok := counterFinding(target, observation.metric, series); ok {
				report.Findings = append(report.Findings, finding)
			}
		}
	}
	if events, ok := source.(lifecycleSource); ok {
		for _, event := range events.LifecycleEvents(start, now) {
			report.Findings = append(report.Findings, lifecycleFinding(event))
		}
	}
	for index := range report.Findings {
		normalizeFinding(&report.Findings[index], report.Window)
	}
	report.Findings = filterFindings(report.Findings, options)

	sort.SliceStable(report.Findings, func(i, j int) bool {
		left, right := report.Findings[i], report.Findings[j]
		if severityRank(left.Severity) != severityRank(right.Severity) {
			return severityRank(left.Severity) < severityRank(right.Severity)
		}
		if left.score != right.score {
			return left.score > right.score
		}
		if left.Service != right.Service {
			return left.Service < right.Service
		}
		if left.Metric != right.Metric {
			return left.Metric < right.Metric
		}
		if left.TargetID != right.TargetID {
			return left.TargetID < right.TargetID
		}
		return left.Message < right.Message
	})
	if len(report.Findings) > limit {
		report.Omitted = len(report.Findings) - limit
		report.Findings = report.Findings[:limit]
	}
	for _, finding := range report.Findings {
		if finding.Severity == "error" {
			report.Status = "error"
			break
		}
		if finding.Severity == "warning" {
			report.Status = "warning"
		}
	}
	return report
}

func (options BuildOptions) allowsSeverity(severity string) bool {
	if len(options.Severities) == 0 {
		return true
	}
	return containsString(options.Severities, severity)
}

func (options BuildOptions) allowsService(service string) bool {
	if len(options.Services) == 0 {
		return true
	}
	return containsString(options.Services, service)
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func filterFindings(findings []Finding, options BuildOptions) []Finding {
	filtered := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		if !options.allowsSeverity(finding.Severity) || !options.allowsService(finding.Service) {
			continue
		}
		if options.ChangedOnly && !findingChanged(finding) {
			continue
		}
		filtered = append(filtered, finding)
	}
	return filtered
}

func findingChanged(finding Finding) bool {
	switch finding.Signal {
	case "counter":
		return finding.Delta != nil && *finding.Delta != 0
	case "gauge":
		return finding.Change != nil && *finding.Change != 0
	default:
		return true
	}
}

func normalizeFinding(finding *Finding, window Window) {
	finding.Message = boundedDiagnostic(finding.Message)
	finding.Suggestion = boundedDiagnostic(finding.Suggestion)
	if finding.Metric != "" && finding.TargetID != "" {
		finding.Evidence = &EvidenceDescriptor{
			TargetID: finding.TargetID,
			Metrics:  []string{finding.Metric},
			Start:    window.Start,
			End:      window.End,
		}
	}
	identity := finding.stableIdentity
	if identity == "" {
		identity = strings.Join([]string{finding.Severity, finding.Signal, finding.TargetID, finding.Service, finding.Metric, finding.Message}, "\x00")
	}
	hash := sha256.Sum256([]byte(identity))
	finding.ID = "finding-" + hex.EncodeToString(hash[:])
}

func boundedDiagnostic(value string) string {
	runes := []rune(value)
	if len(runes) <= 512 {
		return string(runes)
	}
	return string(runes[:511]) + "…"
}

type selectedObservation struct {
	metric string
	kind   model.PanelKind
}

func selectedObservations(families []model.MetricFamily, history classifier.SeriesLookup) []selectedObservation {
	panels := classifier.Classify(families, history)
	seen := map[string]struct{}{}
	selected := make([]selectedObservation, 0, len(panels))
	for _, panel := range panels {
		kind := panel.Kind
		if kind != model.PanelKindGauge && kind != model.PanelKindCounterRate && kind != model.PanelKindHTTPRate && kind != model.PanelKindHTTPErrorRate {
			continue
		}
		if _, ok := seen[panel.Metric]; ok {
			continue
		}
		seen[panel.Metric] = struct{}{}
		if kind == model.PanelKindHTTPRate || kind == model.PanelKindHTTPErrorRate {
			kind = model.PanelKindCounterRate
		}
		selected = append(selected, selectedObservation{metric: panel.Metric, kind: kind})
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].metric != selected[j].metric {
			return selected[i].metric < selected[j].metric
		}
		return selected[i].kind < selected[j].kind
	})
	return selected
}

func boundedSeries(series []model.Series, start, end time.Time) []model.Series {
	result := make([]model.Series, 0, len(series))
	for _, item := range series {
		points := make([]model.SeriesPoint, 0, len(item.Points))
		var baseline *model.SeriesPoint
		var baselineTime time.Time
		for _, point := range item.Points {
			ts, err := time.Parse(time.RFC3339Nano, point.TS)
			if err != nil || ts.After(end) {
				continue
			}
			if ts.Before(start) {
				if baseline == nil || ts.After(baselineTime) {
					pointCopy := point
					baseline = &pointCopy
					baselineTime = ts
				}
				continue
			}
			points = append(points, point)
		}
		if baseline != nil {
			points = append([]model.SeriesPoint{*baseline}, points...)
		}
		if len(points) > 0 {
			item.Points = points
			result = append(result, item)
		}
	}
	return result
}

func lifecycleFinding(event model.LifecycleEvent) Finding {
	severity := "info"
	message := "target appeared"
	score := 10.0
	signal := "lifecycle"
	switch event.Kind {
	case model.LifecycleEventAppeared:
		// First discovery is informational.
	case model.LifecycleEventRecreated:
		message = "target recreated"
	case model.LifecycleEventDisappeared:
		severity = "error"
		message = "target disappeared"
		score = 900
	case model.LifecycleEventStatusTransition:
		if event.To == model.TargetStatusDown {
			severity = "error"
			score = 950
		} else {
			severity = "warning"
			score = 400
		}
		message = "target status changed from " + string(event.From) + " to " + string(event.To)
	}
	if event.Error != "" {
		message += ": " + event.Error
	}
	return Finding{
		Severity:       severity,
		Signal:         signal,
		TargetID:       event.TargetID,
		Service:        event.ServiceName,
		Message:        message,
		score:          score,
		stableIdentity: strings.Join([]string{string(event.Kind), event.At, event.TargetID, event.ServiceName, event.ContainerName, string(event.From), string(event.To), message}, "\x00"),
	}
}

func fallbackSeries(families []model.MetricFamily, metric string, now time.Time) []model.Series {
	for _, family := range families {
		if family.Name != metric {
			continue
		}
		series := make([]model.Series, 0, len(family.Samples))
		for _, sample := range family.Samples {
			series = append(series, model.Series{
				Metric: metric,
				Labels: sample.Labels,
				Points: []model.SeriesPoint{{TS: now.Format(time.RFC3339Nano), Value: sample.Value}},
			})
		}
		return series
	}
	return nil
}

func counterFinding(target model.Target, metric string, series []model.Series) (Finding, bool) {
	var delta, current float64
	var first, last time.Time
	valid := false
	for _, item := range series {
		points := parsedPoints(item.Points)
		if len(points) == 0 {
			continue
		}
		current += points[len(points)-1].value
		if !valid || points[0].ts.Before(first) {
			first = points[0].ts
		}
		if !valid || points[len(points)-1].ts.After(last) {
			last = points[len(points)-1].ts
		}
		for i := 1; i < len(points); i++ {
			change := points[i].value - points[i-1].value
			if change >= 0 {
				delta += change
			} else {
				// A decrease is a counter reset; the post-reset value is the
				// amount accumulated since the reset.
				delta += points[i].value
			}
		}
		valid = true
	}
	if !valid {
		return Finding{}, false
	}
	rate := 0.0
	if elapsed := last.Sub(first).Seconds(); elapsed > 0 {
		rate = delta / elapsed
	}
	return Finding{
		Severity: "info",
		Signal:   "counter",
		TargetID: target.ID,
		Service:  target.ServiceName,
		Metric:   metric,
		Message:  "counter delta and rate over window",
		Delta:    floatPtr(delta),
		Rate:     floatPtr(rate),
		score:    math.Abs(rate),
	}, true
}

func gaugeFinding(target model.Target, metric string, series []model.Series) (Finding, bool) {
	var current, change, peak float64
	var latest time.Time
	valid := false
	for _, item := range series {
		points := parsedPoints(item.Points)
		if len(points) == 0 {
			continue
		}
		first, last := points[0], points[len(points)-1]
		change += last.value - first.value
		if !valid || last.ts.After(latest) {
			latest = last.ts
			current = last.value
		} else if last.ts.Equal(latest) {
			current += last.value
		}
		if !valid {
			peak = points[0].value
		}
		for _, point := range points[1:] {
			if point.value > peak {
				peak = point.value
			}
		}
		valid = true
	}
	if !valid {
		return Finding{}, false
	}
	return Finding{
		Severity: "info",
		Signal:   "gauge",
		TargetID: target.ID,
		Service:  target.ServiceName,
		Metric:   metric,
		Message:  "gauge current, change, and peak over window",
		Current:  floatPtr(current),
		Change:   floatPtr(change),
		Peak:     floatPtr(peak),
		score:    math.Abs(change),
	}, true
}

type parsedPoint struct {
	ts    time.Time
	value float64
}

func parsedPoints(points []model.SeriesPoint) []parsedPoint {
	parsed := make([]parsedPoint, 0, len(points))
	for _, point := range points {
		ts, err := time.Parse(time.RFC3339Nano, point.TS)
		if err != nil {
			continue
		}
		parsed = append(parsed, parsedPoint{ts: ts, value: point.Value})
	}
	sort.SliceStable(parsed, func(i, j int) bool { return parsed[i].ts.Before(parsed[j].ts) })
	return parsed
}

func floatPtr(value float64) *float64 { return &value }

func severityRank(value string) int {
	switch value {
	case "error":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}
