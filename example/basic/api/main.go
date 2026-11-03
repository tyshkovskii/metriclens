// Command api is a tiny example service that exposes Prometheus metrics for
// metriclens to discover. It simulates HTTP traffic so counters grow and the
// generated rate/latency panels have something to plot.
package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// requestKey identifies a unique http_requests_total series.
type requestKey struct {
	method string
	route  string
	status string
}

var (
	mu sync.Mutex

	requests = map[requestKey]float64{}

	// Finite histogram bucket upper bounds (seconds), ascending.
	bucketBounds = []float64{0.1, 0.5, 1}
	bucketCounts = make([]float64, len(bucketBounds)) // cumulative per finite bucket
	totalCount   float64                              // == +Inf bucket and _count
	durationSum  float64

	residentMemory = 12345678.0
)

var routes = []string{"/users", "/orders", "/health"}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Seeded faults let an agent experiment produce a known regression:
	// ERROR_RATE is the share of 500 responses and LATENCY_MS is added to
	// every request. The defaults reproduce normal traffic.
	errorRate := envFloat("ERROR_RATE", 0.07)
	addedLatency := envFloat("LATENCY_MS", 0) / 1000
	go simulate(errorRate, addedLatency)

	http.HandleFunc("/metrics", handleMetrics)
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "example api: metrics at /metrics")
	})

	log.Printf("example api listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func envFloat(name string, fallback float64) float64 {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 {
		log.Fatalf("%s must be a non-negative number, got %q", name, raw)
	}
	return value
}

// simulate generates fake request traffic once per second.
func simulate(errorRate, addedLatency float64) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		mu.Lock()
		for n := 5 + rng.Intn(20); n > 0; n-- {
			route := routes[rng.Intn(len(routes))]
			status := "200"
			if rng.Float64() < errorRate {
				status = "500"
			}
			requests[requestKey{"GET", route, status}]++

			latency := addedLatency + rng.ExpFloat64()*0.2 // mean ~200ms plus any seeded latency
			durationSum += latency
			totalCount++
			for i, ub := range bucketBounds {
				if latency <= ub {
					bucketCounts[i]++
				}
			}
		}
		residentMemory = 12_000_000 + rng.Float64()*1_500_000
		mu.Unlock()
	}
}

func handleMetrics(w http.ResponseWriter, _ *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	var b strings.Builder

	b.WriteString("# HELP http_requests_total Total HTTP requests handled by the example API.\n")
	b.WriteString("# TYPE http_requests_total counter\n")
	keys := make([]requestKey, 0, len(requests))
	for k := range requests {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		return keys[i].status < keys[j].status
	})
	for _, k := range keys {
		fmt.Fprintf(&b, "http_requests_total{method=%q,route=%q,status=%q} %d\n",
			k.method, k.route, k.status, int64(requests[k]))
	}

	b.WriteString("# HELP http_request_duration_seconds Request latency in seconds.\n")
	b.WriteString("# TYPE http_request_duration_seconds histogram\n")
	for i, ub := range bucketBounds {
		fmt.Fprintf(&b, "http_request_duration_seconds_bucket{le=\"%g\"} %d\n", ub, int64(bucketCounts[i]))
	}
	fmt.Fprintf(&b, "http_request_duration_seconds_bucket{le=\"+Inf\"} %d\n", int64(totalCount))
	fmt.Fprintf(&b, "http_request_duration_seconds_sum %.3f\n", durationSum)
	fmt.Fprintf(&b, "http_request_duration_seconds_count %d\n", int64(totalCount))

	b.WriteString("# HELP process_resident_memory_bytes Resident memory size in bytes.\n")
	b.WriteString("# TYPE process_resident_memory_bytes gauge\n")
	fmt.Fprintf(&b, "process_resident_memory_bytes %d\n", int64(residentMemory))

	_, _ = w.Write([]byte(b.String()))
}
