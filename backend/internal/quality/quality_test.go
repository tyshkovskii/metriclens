package quality

import (
	"strings"
	"testing"

	"metriclens/backend/internal/model"
)

func TestAnalyzeDetectsMissingHelpAndType(t *testing.T) {
	issues := Analyze([]model.MetricFamily{
		{
			Name: "custom_metric",
			Type: model.MetricTypeUntyped,
		},
	})

	if !hasIssue(issues, "custom_metric", "missing HELP") {
		t.Fatalf("missing HELP issue not found in %#v", issues)
	}
	if !hasIssue(issues, "custom_metric", "missing TYPE") {
		t.Fatalf("missing TYPE issue not found in %#v", issues)
	}
}

func TestAnalyzeDoesNotFlagDeclaredUntypedAsMissingType(t *testing.T) {
	issues := Analyze([]model.MetricFamily{
		{
			Name:    "custom_metric",
			Type:    model.MetricTypeUntyped,
			HasHelp: true,
			HasType: true,
		},
	})

	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestAnalyzeDetectsCounterWithoutTotalSuffix(t *testing.T) {
	issues := Analyze([]model.MetricFamily{
		{
			Name:    "requests",
			Type:    model.MetricTypeCounter,
			HasHelp: true,
			HasType: true,
		},
	})

	if !hasIssue(issues, "requests", "does not end in _total") {
		t.Fatalf("counter naming issue not found in %#v", issues)
	}
}

func TestAnalyzeDetectsIncompleteHistogram(t *testing.T) {
	issues := Analyze([]model.MetricFamily{
		{
			Name:    "http_request_duration_seconds",
			Type:    model.MetricTypeHistogram,
			HasHelp: true,
			HasType: true,
			Samples: []model.MetricSample{
				{Metric: "http_request_duration_seconds_bucket", Labels: map[string]string{"le": "0.5"}, Value: 10},
			},
		},
	})

	if !hasIssue(issues, "http_request_duration_seconds", "missing _sum, _count") {
		t.Fatalf("histogram issue not found in %#v", issues)
	}
}

func TestAnalyzeAllowsCompleteHistogram(t *testing.T) {
	issues := Analyze([]model.MetricFamily{
		{
			Name:    "http_request_duration_seconds",
			Type:    model.MetricTypeHistogram,
			HasHelp: true,
			HasType: true,
			Samples: []model.MetricSample{
				{Metric: "http_request_duration_seconds_bucket", Labels: map[string]string{"le": "0.5"}, Value: 10},
				{Metric: "http_request_duration_seconds_bucket", Labels: map[string]string{"le": "+Inf"}, Value: 12},
				{Metric: "http_request_duration_seconds_sum", Labels: map[string]string{}, Value: 4.2},
				{Metric: "http_request_duration_seconds_count", Labels: map[string]string{}, Value: 12},
			},
		},
	})

	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestAnalyzeDetectsBucketsWithoutLeLabel(t *testing.T) {
	issues := Analyze([]model.MetricFamily{
		{
			Name:    "http_request_duration_seconds",
			Type:    model.MetricTypeHistogram,
			HasHelp: true,
			HasType: true,
			Samples: []model.MetricSample{
				{Metric: "http_request_duration_seconds_bucket", Labels: map[string]string{}, Value: 10},
				{Metric: "http_request_duration_seconds_sum", Labels: map[string]string{}, Value: 4.2},
				{Metric: "http_request_duration_seconds_count", Labels: map[string]string{}, Value: 12},
			},
		},
	})

	if !hasIssue(issues, "http_request_duration_seconds", "missing the le label") {
		t.Fatalf("missing le issue not found in %#v", issues)
	}
}

func TestAnalyzeDetectsMissingInfBucket(t *testing.T) {
	issues := Analyze([]model.MetricFamily{
		{
			Name:    "http_request_duration_seconds",
			Type:    model.MetricTypeHistogram,
			HasHelp: true,
			HasType: true,
			Samples: []model.MetricSample{
				{Metric: "http_request_duration_seconds_bucket", Labels: map[string]string{"le": "0.5"}, Value: 10},
				{Metric: "http_request_duration_seconds_sum", Labels: map[string]string{}, Value: 4.2},
				{Metric: "http_request_duration_seconds_count", Labels: map[string]string{}, Value: 12},
			},
		},
	})

	if !hasIssue(issues, "http_request_duration_seconds", `le="+Inf"`) {
		t.Fatalf("missing +Inf bucket issue not found in %#v", issues)
	}
}

func TestAnalyzeDetectsHighCardinalityLabels(t *testing.T) {
	issues := Analyze([]model.MetricFamily{
		{
			Name:    "http_requests_total",
			Type:    model.MetricTypeCounter,
			HasHelp: true,
			HasType: true,
			Samples: []model.MetricSample{
				{
					Metric: "http_requests_total",
					Labels: map[string]string{"method": "GET", "request_id": "abc"},
					Value:  1,
				},
			},
		},
	})

	if !hasIssue(issues, "http_requests_total", "request_id") {
		t.Fatalf("high-cardinality label issue not found in %#v", issues)
	}
}

func hasIssue(issues []model.MetricQualityIssue, metric, messagePart string) bool {
	for _, issue := range issues {
		if issue.Metric == metric && strings.Contains(issue.Message, messagePart) {
			return true
		}
	}
	return false
}
