package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"metriclens/backend/internal/classifier"
	"metriclens/backend/internal/diagnosis"
	"metriclens/backend/internal/model"
	"metriclens/backend/internal/quality"
	"metriclens/backend/internal/storage"
	"metriclens/backend/internal/web"
)

const Version = "0.3.0"

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
	configMu   sync.RWMutex
	config     Config
	now        func() time.Time
	markers    *markerStore
}

type ContainerLister interface {
	ListContainers(context.Context) ([]model.DiscoveredContainer, error)
}

type TargetStore interface {
	Targets() []model.Target
	TargetMetrics(string) (model.TargetMetricsResponse, bool)
	// TargetSeries returns stored series for a metric. A nil labels map
	// matches every series of the metric; a non-nil map (even an empty one)
	// matches only the series whose label set is exactly equal.
	TargetSeries(targetID, metric string, labels map[string]string) []model.Series
	LastError() error
}

// ScrapeIntervalSetter is implemented by the background scraper so the UI can
// change its cadence without restarting the process.
type ScrapeIntervalSetter interface {
	SetInterval(time.Duration) error
}

func NewServer(containers ContainerLister, targets TargetStore, config Config) *Server {
	s := &Server{mux: http.NewServeMux(), containers: containers, targets: targets, config: config, now: time.Now, markers: newMarkerStore()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	startedAt := time.Now()
	s.mux.ServeHTTP(recorder, r)
	if recorder.status >= http.StatusBadRequest {
		log.Printf("request failed: status=%d duration=%s", recorder.status, time.Since(startedAt).Round(time.Millisecond))
	}
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /openapi.json", s.handleOpenAPI)
	s.mux.HandleFunc("GET /llms.txt", s.handleLLMs)
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/version", s.handleVersion)
	s.mux.HandleFunc("GET /api/config", s.handleConfig)
	s.mux.HandleFunc("PUT /api/config", s.handleConfigUpdate)
	s.mux.HandleFunc("GET /api/capabilities", s.handleCapabilities)
	s.mux.HandleFunc("GET /api/report", s.handleReport)
	s.mux.HandleFunc("GET /api/readiness", s.handleReadiness)
	s.mux.HandleFunc("POST /api/markers", s.handleCreateMarker)
	s.mux.HandleFunc("GET /api/markers", s.handleListMarkers)
	s.mux.HandleFunc("GET /api/compare", s.handleCompare)
	s.mux.HandleFunc("GET /api/containers", s.handleContainers)
	s.mux.HandleFunc("GET /api/targets", s.handleTargets)
	s.mux.HandleFunc("GET /api/targets/{targetId}/metrics", s.handleTargetMetrics)
	s.mux.HandleFunc("GET /api/targets/{targetId}/series", s.handleTargetSeries)
	s.mux.HandleFunc("GET /api/targets/{targetId}/series/batch", s.handleTargetSeriesBatch)
	s.mux.HandleFunc("GET /api/targets/{targetId}/panels", s.handleTargetPanels)
	s.mux.HandleFunc("GET /api/targets/{targetId}/quality", s.handleTargetQuality)
	s.mux.HandleFunc("GET /api", s.handleAPINotFound)
	s.mux.HandleFunc("GET /api/", s.handleAPINotFound)

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
	s.configMu.RLock()
	config := s.config
	s.configMu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]int64{
		"scrapeIntervalMs": config.ScrapeInterval.Milliseconds(),
		"retentionMs":      config.Retention.Milliseconds(),
	})
}

func (s *Server) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	setter, ok := s.targets.(ScrapeIntervalSetter)
	if !ok {
		writeError(w, http.StatusNotImplemented, "scrape interval cannot be changed at runtime")
		return
	}

	var request struct {
		ScrapeIntervalMs int64 `json:"scrapeIntervalMs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "scrapeIntervalMs must be a positive integer")
		return
	}
	maxIntervalMs := int64(^uint64(0)>>1) / int64(time.Millisecond)
	if request.ScrapeIntervalMs <= 0 || request.ScrapeIntervalMs > maxIntervalMs {
		writeError(w, http.StatusBadRequest, "scrapeIntervalMs must be a positive integer")
		return
	}

	interval := time.Duration(request.ScrapeIntervalMs) * time.Millisecond
	if err := setter.SetInterval(interval); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.configMu.Lock()
	s.config.ScrapeInterval = interval
	config := s.config
	s.configMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]int64{
		"scrapeIntervalMs": config.ScrapeInterval.Milliseconds(),
		"retentionMs":      config.Retention.Milliseconds(),
	})
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	options, err := parseReportOptions(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.configMu.RLock()
	retention := s.config.Retention
	s.configMu.RUnlock()
	if retention <= 0 {
		retention = storage.DefaultRetention
	}

	window := diagnosis.DefaultWindow
	if window > retention {
		window = retention
	}
	if raw := r.URL.Query().Get("window"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "window query parameter must be a positive duration")
			return
		}
		window = parsed
	}
	if window > retention {
		writeError(w, http.StatusBadRequest, "window query parameter must not exceed retention")
		return
	}

	limit := diagnosis.DefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit query parameter must be a positive integer")
			return
		}
		if parsed > diagnosis.MaxLimit {
			writeError(w, http.StatusBadRequest, "limit query parameter exceeds maximum")
			return
		}
		limit = parsed
	}

	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	writeJSON(w, http.StatusOK, diagnosis.BuildWithOptions(s.targets, now, window, limit, options))
}

func (s *Server) handleContainers(w http.ResponseWriter, r *http.Request) {
	containers, err := s.containers.ListContainers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, containers)
}

func (s *Server) handleTargets(w http.ResponseWriter, r *http.Request) {
	targets := s.targets.Targets()
	if len(targets) == 0 {
		if err := s.targets.LastError(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, targets)
}

func (s *Server) handleTargetMetrics(w http.ResponseWriter, r *http.Request) {
	response, ok := s.targets.TargetMetrics(r.PathValue("targetId"))
	if !ok {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleTargetSeries(w http.ResponseWriter, r *http.Request) {
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		writeError(w, http.StatusBadRequest, "metric query parameter is required")
		return
	}

	var labels map[string]string
	if value := r.URL.Query().Get("labels"); value != "" {
		decoded, err := decodeLabels(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "labels query parameter must be a JSON object")
			return
		}
		labels = decoded
	}

	writeJSON(w, http.StatusOK, s.targets.TargetSeries(r.PathValue("targetId"), metric, labels))
}

type batchTargetStore interface {
	TargetSeriesBatch(targetID string, metrics []string, start, end, at *time.Time) []model.Series
}

func (s *Server) handleTargetSeriesBatch(w http.ResponseWriter, r *http.Request) {
	metrics, err := parseMetricNames(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	maxPoints, err := parseBatchMaxPoints(r.URL.Query().Get("maxPoints"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	bounds, err := parseSeriesBounds(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	targetID := r.PathValue("targetId")
	var series []model.Series
	if batch, ok := s.targets.(batchTargetStore); ok {
		series = batch.TargetSeriesBatch(targetID, metrics, bounds.start, bounds.end, bounds.at)
	} else {
		series = make([]model.Series, 0)
		for _, metric := range metrics {
			for _, item := range s.targets.TargetSeries(targetID, metric, nil) {
				item.Points = filterSeriesPoints(item.Points, bounds.start, bounds.end, bounds.at)
				if len(item.Points) > 0 {
					series = append(series, item)
				}
			}
		}
	}
	series, stats := limitBatchSeries(series, maxPoints)
	w.Header().Set("X-MetricLens-Series-Count", strconv.Itoa(stats.seriesCount))
	w.Header().Set("X-MetricLens-Point-Count", strconv.Itoa(stats.pointCount))
	w.Header().Set("X-MetricLens-Series-Truncated", strconv.FormatBool(stats.seriesTruncated))
	w.Header().Set("X-MetricLens-Points-Truncated", strconv.FormatBool(stats.pointsTruncated))
	writeJSON(w, http.StatusOK, series)
}

func parseMetricNames(values url.Values) ([]string, error) {
	raw := append([]string(nil), values["metrics"]...)
	raw = append(raw, values["metric"]...)
	seen := map[string]struct{}{}
	metrics := make([]string, 0, len(raw))
	for _, value := range raw {
		for _, metric := range strings.Split(value, ",") {
			metric = strings.TrimSpace(metric)
			if metric == "" {
				return nil, errors.New("metrics query parameter must contain at least one metric name")
			}
			if _, ok := seen[metric]; ok {
				continue
			}
			seen[metric] = struct{}{}
			metrics = append(metrics, metric)
		}
	}
	if len(metrics) == 0 {
		return nil, errors.New("metrics query parameter is required")
	}
	if len(metrics) > MaxBatchMetrics {
		return nil, fmt.Errorf("metrics query parameter supports at most %d metric names", MaxBatchMetrics)
	}
	sort.Strings(metrics)
	return metrics, nil
}

type seriesBounds struct {
	start *time.Time
	end   *time.Time
	at    *time.Time
}

func parseSeriesBounds(r *http.Request) (seriesBounds, error) {
	query := r.URL.Query()
	start, startSet, err := parseOptionalTime(query.Get("start"), "start")
	if err != nil {
		return seriesBounds{}, err
	}
	end, endSet, err := parseOptionalTime(query.Get("end"), "end")
	if err != nil {
		return seriesBounds{}, err
	}
	at, atSet, err := parseOptionalTime(query.Get("at"), "at")
	if err != nil {
		return seriesBounds{}, err
	}
	if atSet && (startSet || endSet) {
		return seriesBounds{}, errors.New("at cannot be combined with start or end")
	}
	if startSet && endSet && start.After(end) {
		return seriesBounds{}, errors.New("start must not be after end")
	}
	bounds := seriesBounds{}
	if startSet {
		bounds.start = &start
	}
	if endSet {
		bounds.end = &end
	}
	if atSet {
		bounds.at = &at
	}
	return bounds, nil
}

func parseOptionalTime(value, name string) (time.Time, bool, error) {
	if value == "" {
		return time.Time{}, false, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("%s query parameter must be RFC3339", name)
	}
	parsed = parsed.UTC()
	return parsed, true, nil
}

func filterSeriesPoints(points []model.SeriesPoint, start, end, at *time.Time) []model.SeriesPoint {
	if at != nil {
		var selected *model.SeriesPoint
		var selectedTime time.Time
		for i := range points {
			pointTime, err := time.Parse(time.RFC3339Nano, points[i].TS)
			if err != nil || pointTime.After(*at) {
				continue
			}
			if selected == nil || pointTime.After(selectedTime) {
				point := points[i]
				selected = &point
				selectedTime = pointTime
			}
		}
		if selected == nil {
			return nil
		}
		return []model.SeriesPoint{*selected}
	}
	filtered := make([]model.SeriesPoint, 0, len(points))
	for _, point := range points {
		pointTime, err := time.Parse(time.RFC3339Nano, point.TS)
		if err != nil {
			continue
		}
		if start != nil && pointTime.Before(*start) {
			continue
		}
		if end != nil && pointTime.After(*end) {
			continue
		}
		filtered = append(filtered, point)
	}
	return filtered
}

func labelsKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)
	var builder strings.Builder
	for _, name := range names {
		builder.WriteString(name)
		builder.WriteByte('=')
		builder.WriteString(labels[name])
		builder.WriteByte('\xff')
	}
	return builder.String()
}

// handleTargetPanels serves classifier-based panel suggestions. The bundled
// frontend consumes the chart kinds it can render today and falls back to its
// local heuristic for raw metric charts that are not represented here.
func (s *Server) handleTargetPanels(w http.ResponseWriter, r *http.Request) {
	response, ok := s.targets.TargetMetrics(r.PathValue("targetId"))
	if !ok {
		writeError(w, http.StatusNotFound, "target not found")
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
		writeError(w, http.StatusNotFound, "target not found")
		return
	}

	writeJSON(w, http.StatusOK, quality.Analyze(response.Families))
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "api endpoint not found")
}

// writeError is the single place that decides the API error body shape.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

func decodeLabels(value string) (map[string]string, error) {
	labels := map[string]string{}
	if err := json.Unmarshal([]byte(value), &labels); err != nil {
		return nil, err
	}
	return labels, nil
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}
