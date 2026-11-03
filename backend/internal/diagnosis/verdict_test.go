package diagnosis

import (
	"math"
	"strings"
	"testing"
	"time"

	"metriclens/backend/internal/model"
)

func TestBucketP95InterpolatesLikeHistogramQuantile(t *testing.T) {
	bounds := []float64{0.1, 0.5, 1, math.Inf(1)}
	value, beyond := bucketP95(bounds, []float64{10, 90, 99, 100})
	if beyond || math.Abs(value-(0.5+0.5*5.0/9.0)) > 1e-9 {
		t.Fatalf("p95 = %v beyond %v, want interpolation inside (0.5, 1]", value, beyond)
	}
	value, beyond = bucketP95(bounds, []float64{0, 0, 0, 100})
	if !beyond || value != 1 {
		t.Fatalf("p95 = %v beyond %v, want the highest finite bound reported as a lower bound", value, beyond)
	}
}

// verdictSource serves one API target whose request counter and latency
// histogram grow by the given amounts in the baseline window [t0, t1] and the
// compared window [t1, t2].
func verdictSource(t0, t1, t2 time.Time, baseline, compared map[string]float64) fakeSource {
	target := model.Target{ID: "api-1", ServiceName: "api", Status: model.TargetStatusUp}
	counter := func(metric string, labels map[string]string, key string) model.Series {
		return model.Series{TargetID: target.ID, Metric: metric, Labels: labels, Points: []model.SeriesPoint{
			{TS: t0.Format(time.RFC3339Nano), Value: 0},
			{TS: t1.Format(time.RFC3339Nano), Value: baseline[key]},
			{TS: t2.Format(time.RFC3339Nano), Value: baseline[key] + compared[key]},
		}}
	}
	requests := map[string]string{"method": "GET", "route": "/orders"}
	withStatus := func(status string) map[string]string {
		labels := map[string]string{"status": status}
		for name, value := range requests {
			labels[name] = value
		}
		return labels
	}
	series := []model.Series{
		counter("http_requests_total", withStatus("200"), "ok"),
		counter("http_requests_total", withStatus("500"), "errors"),
	}
	for _, le := range []string{"0.1", "0.5", "1", "+Inf"} {
		series = append(series, counter("http_request_duration_seconds_bucket", map[string]string{"le": le}, "le "+le))
	}
	families := []model.MetricFamily{
		{Name: "http_requests_total", Type: model.MetricTypeCounter, HasHelp: true, HasType: true, Samples: []model.MetricSample{
			{Metric: "http_requests_total", Labels: withStatus("200"), Value: 1},
			{Metric: "http_requests_total", Labels: withStatus("500"), Value: 1},
		}},
		{Name: "http_request_duration_seconds", Type: model.MetricTypeHistogram, HasHelp: true, HasType: true, Samples: []model.MetricSample{
			{Metric: "http_request_duration_seconds_bucket", Labels: map[string]string{"le": "0.1"}, Value: 1},
			{Metric: "http_request_duration_seconds_bucket", Labels: map[string]string{"le": "+Inf"}, Value: 1},
			{Metric: "http_request_duration_seconds_sum", Labels: map[string]string{}, Value: 1},
			{Metric: "http_request_duration_seconds_count", Labels: map[string]string{}, Value: 1},
		}},
	}
	return fakeSource{
		targets: []model.Target{target},
		metrics: map[string]model.TargetMetricsResponse{target.ID: {Target: target, Families: families}},
		series:  map[string][]model.Series{target.ID: series},
	}
}

func verdicts(report Report) map[string]Finding {
	found := map[string]Finding{}
	for _, finding := range report.Findings {
		if finding.Verdict != "" {
			found[finding.Signal] = finding
		}
	}
	return found
}

func TestBuildReportsErrorRatioAndLatencyVerdicts(t *testing.T) {
	t0 := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	t1, t2 := t0.Add(time.Minute), t0.Add(2*time.Minute)
	healthy := map[string]float64{"ok": 990, "errors": 10, "le 0.1": 400, "le 0.5": 940, "le 1": 990, "le +Inf": 1000}
	degraded := map[string]float64{"ok": 700, "errors": 300, "le 0.1": 100, "le 0.5": 500, "le 1": 900, "le +Inf": 1000}
	options := BuildOptions{BaselineStart: t0, BaselineEnd: t1}

	report := BuildWithOptions(verdictSource(t0, t1, t2, healthy, degraded), t2, time.Minute, MaxLimit, options)
	found := verdicts(report)
	errorRatio, latency := found["error_ratio"], found["latency_p95"]
	if errorRatio.Verdict != "regressed" || errorRatio.Severity != "warning" || *errorRatio.Baseline != 0.01 || *errorRatio.Candidate != 0.3 {
		t.Fatalf("error ratio = %#v, want regressed warning from 1%% to 30%%", errorRatio)
	}
	if latency.Verdict != "regressed" || math.Abs(*latency.Baseline-0.6) > 1e-9 || *latency.Candidate != 1 || !strings.Contains(latency.Message, "at least 1 seconds") {
		t.Fatalf("latency = %#v, want regressed p95 0.6 to at least 1 second", latency)
	}
	if report.Status != "warning" || report.Baseline == nil || report.Baseline.Start != t0.Format(time.RFC3339Nano) {
		t.Fatalf("report status %q baseline %#v, want warning with the baseline window", report.Status, report.Baseline)
	}
	if latency.Evidence == nil || strings.Join(latency.Evidence.Metrics, ",") != "http_request_duration_seconds_bucket,http_request_duration_seconds_count,http_request_duration_seconds_sum" || latency.Evidence.Start != t0.Format(time.RFC3339Nano) {
		t.Fatalf("latency evidence = %#v, want histogram sample names over both windows", latency.Evidence)
	}

	degraded["errors"] = 310
	repeated := verdicts(BuildWithOptions(verdictSource(t0, t1, t2, healthy, degraded), t2, time.Minute, MaxLimit, options))
	if repeated["error_ratio"].ID != errorRatio.ID || repeated["error_ratio"].Message == errorRatio.Message {
		t.Fatalf("error ratio IDs %q and %q, want one identity for the same verdict despite new values", errorRatio.ID, repeated["error_ratio"].ID)
	}

	steady := BuildWithOptions(verdictSource(t0, t1, t2, healthy, healthy), t2, time.Minute, MaxLimit, options)
	if found := verdicts(steady); found["error_ratio"].Verdict != "unchanged" || found["latency_p95"].Verdict != "unchanged" || steady.Status != "ok" {
		t.Fatalf("steady verdicts = %#v, want unchanged", found)
	}
	options.ChangedOnly = true
	if found := verdicts(BuildWithOptions(verdictSource(t0, t1, t2, healthy, healthy), t2, time.Minute, MaxLimit, options)); len(found) != 0 {
		t.Fatalf("changedOnly verdicts = %#v, want unchanged verdicts omitted", found)
	}

	quiet := map[string]float64{"ok": 9, "errors": 1, "le 0.1": 5, "le 0.5": 9, "le 1": 10, "le +Inf": 10}
	sparse := BuildWithOptions(verdictSource(t0, t1, t2, healthy, quiet), t2, time.Minute, MaxLimit, BuildOptions{BaselineStart: t0, BaselineEnd: t1})
	if found := verdicts(sparse); found["error_ratio"].Verdict != "inconclusive" || found["latency_p95"].Verdict != "inconclusive" || found["error_ratio"].Candidate != nil {
		t.Fatalf("sparse verdicts = %#v, want inconclusive without values", found)
	}
}

func TestLatencyVerdictIsInconclusiveWithoutComparableBuckets(t *testing.T) {
	t0 := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	t1, t2 := t0.Add(time.Minute), t0.Add(2*time.Minute)
	baseline, compared := timeRange{start: t0, end: t1}, timeRange{start: t1, end: t2}
	bucket := func(le string, points ...model.SeriesPoint) model.Series {
		return model.Series{Metric: "request_seconds_bucket", Labels: map[string]string{"le": le}, Points: points}
	}
	at := func(ts time.Time, value float64) model.SeriesPoint {
		return model.SeriesPoint{TS: ts.Format(time.RFC3339Nano), Value: value}
	}
	// The le="0.5" bucket is replaced by le="0.25" after the baseline.
	relayout := []model.Series{
		bucket("0.5", at(t0, 0), at(t1, 900)),
		bucket("0.25", at(t2, 500)),
		bucket("+Inf", at(t0, 0), at(t1, 1000), at(t2, 2000)),
	}
	if finding := latencyFinding(model.Target{}, "request_seconds", "seconds", relayout, baseline, compared); finding.Verdict != "inconclusive" {
		t.Fatalf("relayout verdict = %#v, want inconclusive", finding)
	}
	// Both windows have their p95 past the largest finite bucket.
	overflow := []model.Series{
		bucket("1", at(t0, 0), at(t1, 10), at(t2, 20)),
		bucket("+Inf", at(t0, 0), at(t1, 100), at(t2, 300)),
	}
	if finding := latencyFinding(model.Target{}, "request_seconds", "seconds", overflow, baseline, compared); finding.Verdict != "inconclusive" {
		t.Fatalf("overflow verdict = %#v, want inconclusive", finding)
	}
}

func TestHistogramQualityWarningsRequestSampleEvidence(t *testing.T) {
	target := model.Target{ID: "api-1", ServiceName: "api", Status: model.TargetStatusUp}
	source := fakeSource{
		targets: []model.Target{target},
		metrics: map[string]model.TargetMetricsResponse{target.ID: {Families: []model.MetricFamily{{
			Name: "upload_size_bytes", Type: model.MetricTypeHistogram, HasHelp: true, HasType: true,
			Samples: []model.MetricSample{{Metric: "upload_size_bytes_bucket", Labels: map[string]string{"le": "1024"}, Value: 1}},
		}}}},
	}
	report := BuildWithOptions(source, time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC), time.Minute, MaxLimit, BuildOptions{Severities: []string{"warning"}})
	if len(report.Findings) == 0 {
		t.Fatal("expected histogram quality warnings")
	}
	for _, finding := range report.Findings {
		if finding.Evidence == nil || strings.Join(finding.Evidence.Metrics, ",") != "upload_size_bytes_bucket,upload_size_bytes_count,upload_size_bytes_sum" {
			t.Fatalf("finding %q evidence = %#v, want stored histogram sample names", finding.Message, finding.Evidence)
		}
	}
}

func TestBuildWithoutBaselineHasNoVerdicts(t *testing.T) {
	t0 := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	counts := map[string]float64{"ok": 990, "errors": 10, "le 0.1": 400, "le 0.5": 940, "le 1": 990, "le +Inf": 1000}
	report := Build(verdictSource(t0, t0.Add(time.Minute), t0.Add(2*time.Minute), counts, counts), t0.Add(2*time.Minute), time.Minute, MaxLimit)
	if found := verdicts(report); len(found) != 0 || report.Baseline != nil {
		t.Fatalf("verdicts = %#v baseline %#v, want none without a baseline window", found, report.Baseline)
	}
}
