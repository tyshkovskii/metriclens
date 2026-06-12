package promtext

import "testing"

func TestSniff(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "help", body: "# HELP requests_total Requests\n", want: true},
		{name: "type", body: "# TYPE requests_total counter\n", want: true},
		{name: "sample", body: "requests_total 123\n", want: true},
		{name: "labeled sample", body: "requests_total{method=\"GET\"} 123 1710000000000\n", want: true},
		{name: "html", body: "<html></html>", want: false},
		{name: "json", body: `{"status":"ok"}`, want: false},
		{name: "empty", body: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sniff([]byte(tt.body)); got != tt.want {
				t.Fatalf("Sniff() = %v, want %v", got, tt.want)
			}
		})
	}
}
