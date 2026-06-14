package prober

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"metriclens/backend/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestProbeFindsPrometheusEndpoint(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "http://api:8080/metrics" {
				return response(http.StatusNotFound, "not found"), nil
			}
			return response(http.StatusOK, "# HELP requests_total Requests\n"), nil
		}),
	}
	prober := New(client)
	prober.now = func() time.Time { return time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC) }

	targets := prober.Probe(context.Background(), []model.DiscoveredContainer{
		{
			ID:             "abc123",
			Name:           "api-1",
			State:          model.ContainerStateRunning,
			ComposeService: "api",
			ExposedPorts:   []int{8080},
			Labels:         map[string]string{},
		},
	})

	if len(targets) != 1 {
		t.Fatalf("targets length = %d, want 1", len(targets))
	}
	if targets[0].Status != model.TargetStatusUp {
		t.Fatalf("status = %q, want up", targets[0].Status)
	}
	if targets[0].URL != "http://api:8080/metrics" {
		t.Fatalf("url = %q, want http://api:8080/metrics", targets[0].URL)
	}
	if targets[0].LastError != "" {
		t.Fatalf("lastError = %q, want empty", targets[0].LastError)
	}
	if targets[0].DiscoveredAt != "2026-06-06T12:00:00Z" {
		t.Fatalf("discoveredAt = %q, want fixed time", targets[0].DiscoveredAt)
	}
}

func TestProbeMarksDownWhenNoEndpointIsPrometheus(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return response(http.StatusOK, "<html>not metrics</html>"), nil
		}),
	}
	prober := New(client)

	targets := prober.Probe(context.Background(), []model.DiscoveredContainer{
		{
			ID:             "abc123",
			Name:           "api-1",
			State:          model.ContainerStateRunning,
			ComposeService: "api",
			ExposedPorts:   []int{8080},
			Labels:         map[string]string{},
		},
	})

	if len(targets) != 1 {
		t.Fatalf("targets length = %d, want 1", len(targets))
	}
	if targets[0].Status != model.TargetStatusDown {
		t.Fatalf("status = %q, want down", targets[0].Status)
	}
	if targets[0].URL != "" {
		t.Fatalf("url = %q, want empty", targets[0].URL)
	}
	if !strings.Contains(targets[0].LastError, "no Prometheus endpoint found") {
		t.Fatalf("lastError = %q, want no endpoint message", targets[0].LastError)
	}
}

func TestProbeSkipsNonRunningAndNonComposeContainers(t *testing.T) {
	prober := New(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatalf("unexpected request to %s", req.URL.String())
			return nil, errors.New("unexpected request")
		}),
	})

	targets := prober.Probe(context.Background(), []model.DiscoveredContainer{
		{
			ID:             "exited",
			State:          model.ContainerStateExited,
			ComposeService: "api",
		},
		{
			ID:    "non-compose",
			State: model.ContainerStateRunning,
		},
	})

	if len(targets) != 0 {
		t.Fatalf("targets length = %d, want 0", len(targets))
	}
}

func TestCandidateURLsPrioritizeLabelPortAndServiceHost(t *testing.T) {
	urls, configError := candidateURLs(model.DiscoveredContainer{
		Name:           "api-1",
		ComposeService: "api",
		ExposedPorts:   []int{8080, 9090},
		Labels: map[string]string{
			"metriclens.port": "9000",
			"metriclens.path": "/custom-metrics",
		},
	})

	if configError != "" {
		t.Fatalf("configError = %q, want empty", configError)
	}
	wantPrefix := []string{
		"http://api:9000/custom-metrics",
		"http://api:9000/metrics",
		"http://api:9000/actuator/prometheus",
		"http://api:9000/q/metrics",
	}
	if !reflect.DeepEqual(urls[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("url prefix = %#v, want %#v", urls[:len(wantPrefix)], wantPrefix)
	}
}

func TestCandidateURLsReportsInvalidPortLabel(t *testing.T) {
	urls, configError := candidateURLs(model.DiscoveredContainer{
		Name:           "api-1",
		ComposeService: "api",
		ExposedPorts:   []int{8080},
		Labels: map[string]string{
			"metriclens.port": "not-a-port",
		},
	})

	if configError == "" {
		t.Fatal("configError is empty, want invalid port message")
	}
	if urls[0] != "http://api:8080/metrics" {
		t.Fatalf("first url = %q, want exposed port first", urls[0])
	}
}

func TestCandidateURLsReportsInvalidPathLabel(t *testing.T) {
	urls, configError := candidateURLs(model.DiscoveredContainer{
		Name:           "api-1",
		ComposeService: "api",
		ExposedPorts:   []int{8080},
		Labels: map[string]string{
			"metriclens.path": "metrics",
		},
	})

	if configError == "" {
		t.Fatal("configError is empty, want invalid path message")
	}
	if urls[0] != "http://api:8080/metrics" {
		t.Fatalf("first url = %q, want default metrics path first", urls[0])
	}
}

func response(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
