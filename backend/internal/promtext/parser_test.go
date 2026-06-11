package promtext

import (
	"strings"
	"testing"

	"metriclens/backend/internal/model"
)

func TestParseHelpTypeAndSamples(t *testing.T) {
	families, err := Parse(strings.NewReader(`
# HELP http_requests_total Total HTTP requests.
# TYPE http_requests_total counter
http_requests_total{method="GET",route="/users",status="200"} 123
http_requests_total{method="GET",route="/users",status="500"} 3 1710000000000
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(families) != 1 {
		t.Fatalf("families length = %d, want 1", len(families))
	}

	family := families[0]
	if family.Name != "http_requests_total" {
		t.Fatalf("family name = %q, want http_requests_total", family.Name)
	}
	if family.Help != "Total HTTP requests." {
		t.Fatalf("help = %q, want description", family.Help)
	}
	if !family.HasHelp {
		t.Fatal("HasHelp = false, want true")
	}
	if family.Type != model.MetricTypeCounter {
		t.Fatalf("type = %q, want counter", family.Type)
	}
	if !family.HasType {
		t.Fatal("HasType = false, want true")
	}
	if len(family.Samples) != 2 {
		t.Fatalf("samples length = %d, want 2", len(family.Samples))
	}
	if family.Samples[0].Labels["method"] != "GET" {
		t.Fatalf("method label = %q, want GET", family.Samples[0].Labels["method"])
	}
	if family.Samples[0].Value != 123 {
		t.Fatalf("value = %v, want 123", family.Samples[0].Value)
	}
	if family.Samples[1].Timestamp == nil || *family.Samples[1].Timestamp != 1710000000000 {
		t.Fatalf("timestamp = %v, want 1710000000000", family.Samples[1].Timestamp)
	}
}

func TestParseSampleWithoutLabels(t *testing.T) {
	families, err := Parse(strings.NewReader("process_resident_memory_bytes 12345678\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(families) != 1 {
		t.Fatalf("families length = %d, want 1", len(families))
	}
	if families[0].Type != model.MetricTypeUntyped {
		t.Fatalf("type = %q, want untyped", families[0].Type)
	}
	if len(families[0].Samples[0].Labels) != 0 {
		t.Fatalf("labels = %#v, want empty", families[0].Samples[0].Labels)
	}
}

func TestParseIgnoresOtherComments(t *testing.T) {
	families, err := Parse(strings.NewReader(`
# This comment should be ignored.
# EOF
up 1
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(families) != 1 {
		t.Fatalf("families length = %d, want 1", len(families))
	}
	if families[0].Name != "up" {
		t.Fatalf("family name = %q, want up", families[0].Name)
	}
}

func TestParseHistogramSamplesUseBaseFamily(t *testing.T) {
	families, err := Parse(strings.NewReader(`
# HELP http_request_duration_seconds HTTP request latency.
# TYPE http_request_duration_seconds histogram
http_request_duration_seconds_bucket{le="0.1"} 10
http_request_duration_seconds_bucket{le="+Inf"} 55
http_request_duration_seconds_sum 12.3
http_request_duration_seconds_count 55
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(families) != 1 {
		t.Fatalf("families length = %d, want 1", len(families))
	}
	if families[0].Name != "http_request_duration_seconds" {
		t.Fatalf("family name = %q, want base metric", families[0].Name)
	}
	if families[0].Type != model.MetricTypeHistogram {
		t.Fatalf("type = %q, want histogram", families[0].Type)
	}
	if len(families[0].Samples) != 4 {
		t.Fatalf("samples length = %d, want 4", len(families[0].Samples))
	}
	if families[0].Samples[0].Metric != "http_request_duration_seconds_bucket" {
		t.Fatalf("sample metric = %q, want bucket metric", families[0].Samples[0].Metric)
	}
}

func TestParseEscapedLabelValue(t *testing.T) {
	families, err := Parse(strings.NewReader(`request_total{path="/users\"quoted\"",note="hello\nworld"} 1`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if families[0].Samples[0].Labels["path"] != `/users"quoted"` {
		t.Fatalf("path label = %q, want unescaped quote", families[0].Samples[0].Labels["path"])
	}
	if families[0].Samples[0].Labels["note"] != "hello\nworld" {
		t.Fatalf("note label = %q, want unescaped newline", families[0].Samples[0].Labels["note"])
	}
}

func TestParseRejectsInvalidSamples(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing value", body: "requests_total\n"},
		{name: "bad value", body: "requests_total nope\n"},
		{name: "bad metric", body: "9requests_total 1\n"},
		{name: "bad label", body: `requests_total{method=GET} 1`},
		{name: "trailing label comma", body: `requests_total{method="GET",} 1`},
		{name: "bad timestamp", body: "requests_total 1 now\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(tt.body)); err == nil {
				t.Fatal("Parse() error = nil, want error")
			}
		})
	}
}

func TestParseRejectsInvalidType(t *testing.T) {
	_, err := Parse(strings.NewReader("# TYPE requests_total unknown\n"))
	if err == nil {
		t.Fatal("Parse() error = nil, want error")
	}
}
