package api

import (
	_ "embed"
	"log"
	"net/http"
	"sort"

	"metriclens/backend/internal/diagnosis"
	"metriclens/backend/internal/storage"
)

//go:embed openapi.json
var openAPIDocument []byte

//go:embed llms.txt
var llmsDocument []byte

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(openAPIDocument); err != nil {
		log.Printf("write OpenAPI document: %v", err)
	}
}

func (s *Server) handleLLMs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(llmsDocument); err != nil {
		log.Printf("write llms.txt document: %v", err)
	}
}

type capabilitiesResponse struct {
	Name             string             `json:"name"`
	Version          string             `json:"version"`
	ScrapeIntervalMs int64              `json:"scrapeIntervalMs"`
	RetentionMs      int64              `json:"retentionMs"`
	Features         []string           `json:"features"`
	Limits           capabilitiesLimits `json:"limits"`
	Links            capabilitiesLinks  `json:"links"`
}

type capabilitiesLimits struct {
	ReportFindings        int   `json:"reportFindings"`
	MarkerFieldChars      int   `json:"markerFieldChars"`
	ReadinessTimeoutMs    int64 `json:"readinessTimeoutMs"`
	BatchMetrics          int   `json:"batchMetrics"`
	BatchSeriesPerMetric  int   `json:"batchSeriesPerMetric"`
	BatchSeriesTotal      int   `json:"batchSeriesTotal"`
	BatchPointsPerSeries  int   `json:"batchPointsPerSeries"`
	BatchPointsTotal      int   `json:"batchPointsTotal"`
	BatchDefaultMaxPoints int   `json:"batchDefaultMaxPoints"`
}

type capabilitiesLinks struct {
	OpenAPI string `json:"openapi"`
	LLMs    string `json:"llms"`
}

var implementedFeatures = []string{
	"batch-evidence",
	"diagnostic-filters",
	"lifecycle-events",
	"named-markers",
	"readiness",
	"time-travel",
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	s.configMu.RLock()
	config := s.config
	s.configMu.RUnlock()
	retention := config.Retention
	if retention <= 0 {
		retention = storage.DefaultRetention
	}
	features := append([]string(nil), implementedFeatures...)
	sort.Strings(features)
	writeJSON(w, http.StatusOK, capabilitiesResponse{
		Name:             "metriclens",
		Version:          Version,
		ScrapeIntervalMs: config.ScrapeInterval.Milliseconds(),
		RetentionMs:      retention.Milliseconds(),
		Features:         features,
		Limits: capabilitiesLimits{
			ReportFindings:        diagnosis.MaxLimit,
			MarkerFieldChars:      MaxMarkerFieldRunes,
			ReadinessTimeoutMs:    MaxReadinessTimeout.Milliseconds(),
			BatchMetrics:          MaxBatchMetrics,
			BatchSeriesPerMetric:  MaxBatchSeriesPerMetric,
			BatchSeriesTotal:      MaxBatchSeriesTotal,
			BatchPointsPerSeries:  MaxBatchPointsPerSeries,
			BatchPointsTotal:      MaxBatchPointsTotal,
			BatchDefaultMaxPoints: DefaultBatchMaxPoints,
		},
		Links: capabilitiesLinks{OpenAPI: "/openapi.json", LLMs: "/llms.txt"},
	})
}
