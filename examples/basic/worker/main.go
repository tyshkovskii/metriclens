// Command worker is a second example service exposing Prometheus metrics so
// metriclens shows multiple targets. It reports a job counter and a queue-depth
// gauge, which drive the generated counter-rate and gauge panels.
package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	mu sync.Mutex

	jobsProcessed  = map[string]float64{} // status -> count
	queueDepth     float64
	residentMemory = 9_000_000.0
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "9090"
	}

	go simulate()

	http.HandleFunc("/metrics", handleMetrics)
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "example worker: metrics at /metrics")
	})

	log.Printf("example worker listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

// simulate processes a few fake jobs each second.
func simulate() {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		mu.Lock()
		for done := rng.Intn(5); done > 0; done-- {
			status := "success"
			if rng.Float64() < 0.1 {
				status = "error"
			}
			jobsProcessed[status]++
		}
		queueDepth = float64(rng.Intn(50))
		residentMemory = 8_500_000 + rng.Float64()*1_000_000
		mu.Unlock()
	}
}

func handleMetrics(w http.ResponseWriter, _ *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	var b strings.Builder

	b.WriteString("# HELP jobs_processed_total Total background jobs processed by the worker.\n")
	b.WriteString("# TYPE jobs_processed_total counter\n")
	for _, status := range []string{"error", "success"} {
		fmt.Fprintf(&b, "jobs_processed_total{status=%q} %d\n", status, int64(jobsProcessed[status]))
	}

	b.WriteString("# HELP worker_queue_depth Current number of queued jobs.\n")
	b.WriteString("# TYPE worker_queue_depth gauge\n")
	fmt.Fprintf(&b, "worker_queue_depth %d\n", int64(queueDepth))

	b.WriteString("# HELP process_resident_memory_bytes Resident memory size in bytes.\n")
	b.WriteString("# TYPE process_resident_memory_bytes gauge\n")
	fmt.Fprintf(&b, "process_resident_memory_bytes %d\n", int64(residentMemory))

	_, _ = w.Write([]byte(b.String()))
}
