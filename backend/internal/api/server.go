package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"metriclens/backend/internal/classifier"
	"metriclens/backend/internal/model"
	"metriclens/backend/internal/quality"
	"metriclens/backend/internal/web"
)

const Version = "0.1.0"

// Config is the effective runtime configuration, exposed to the frontend via
// /api/config so UI timing (live window, poll cadence, staleness) follows the
// backend settings instead of hardcoding their defaults.
type Config struct {
	ScrapeInterval time.Duration
	Retention      time.Duration
}

type Server struct {
	mux        *http.ServeMux
	containers ContainerLister
	targets    TargetStore
	config     Config
}

type ContainerLister interface {
	ListContainers(context.Context) ([]model.DiscoveredContainer, error)
}

type TargetStore interface {
	Targets() []model.Target
	TargetMetrics(string) (model.TargetMetricsResponse, bool)
	TargetSeries(targetID, metric string, labels map[string]string) []model.Series
	LastError() error
}

func NewServer(containers ContainerLister, targets TargetStore, config Config) *Server {
	s := &Server{mux: http.NewServeMux(), containers: containers, targets: targets, config: config}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/version", s.handleVersion)
	s.mux.HandleFunc("GET /api/config", s.handleConfig)
	s.mux.HandleFunc("GET /api/containers", s.handleContainers)
	s.mux.HandleFunc("GET /api/targets", s.handleTargets)
	s.mux.HandleFunc("GET /api/targets/{targetId}/metrics", s.handleTargetMetrics)
	s.mux.HandleFunc("GET /api/targets/{targetId}/series", s.handleTargetSeries)
	s.mux.HandleFunc("GET /api/targets/{targetId}/panels", s.handleTargetPanels)
	s.mux.HandleFunc("GET /api/targets/{targetId}/quality", s.handleTargetQuality)

	// Catch-all: serve the embedded frontend. The /api/* patterns above are
	// more specific, so they take precedence in Go's ServeMux.
	s.mux.Handle("GET /", web.Handler())
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"name":    "metriclens",
		"version": Version,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]int64{
		"scrapeIntervalMs": s.config.ScrapeInterval.Milliseconds(),
		"retentionMs":      s.config.Retention.Milliseconds(),
	})
}

func (s *Server) handleContainers(w http.ResponseWriter, r *http.Request) {
	containers, err := s.containers.ListContainers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, containers)
}

func (s *Server) handleTargets(w http.ResponseWriter, r *http.Request) {
	targets := s.targets.Targets()
	if len(targets) == 0 {
		if err := s.targets.LastError(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, targets)
}

func (s *Server) handleTargetMetrics(w http.ResponseWriter, r *http.Request) {
	response, ok := s.targets.TargetMetrics(r.PathValue("targetId"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "target not found"})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleTargetSeries(w http.ResponseWriter, r *http.Request) {
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "metric query parameter is required"})
		return
	}

	labels, err := decodeLabels(r.URL.Query().Get("labels"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "labels query parameter must be a JSON object"})
		return
	}

	writeJSON(w, http.StatusOK, s.targets.TargetSeries(r.PathValue("targetId"), metric, labels))
}

// handleTargetPanels serves full classifier-based panel suggestions. The
// bundled frontend does not consume this endpoint — it picks chart kinds with
// the intentionally simpler rule in frontend/src/lib/series.ts (chartKind);
// /panels exists for external consumers and a future richer dashboard. Keep
// that split in mind before changing either side's classification rules.
func (s *Server) handleTargetPanels(w http.ResponseWriter, r *http.Request) {
	response, ok := s.targets.TargetMetrics(r.PathValue("targetId"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "target not found"})
		return
	}

	targetID := r.PathValue("targetId")
	writeJSON(w, http.StatusOK, classifier.Classify(response.Families, func(metric string) []model.Series {
		return s.targets.TargetSeries(targetID, metric, nil)
	}))
}

func (s *Server) handleTargetQuality(w http.ResponseWriter, r *http.Request) {
	response, ok := s.targets.TargetMetrics(r.PathValue("targetId"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "target not found"})
		return
	}

	writeJSON(w, http.StatusOK, quality.Analyze(response.Families))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeLabels(value string) (map[string]string, error) {
	if value == "" {
		return nil, nil
	}

	labels := map[string]string{}
	if err := json.Unmarshal([]byte(value), &labels); err != nil {
		return nil, err
	}
	return labels, nil
}
