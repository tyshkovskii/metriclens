package api

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"metriclens/backend/internal/model"
)

const (
	// DefaultReadinessTimeout is used when readiness callers omit timeout.
	DefaultReadinessTimeout = time.Second
	// MaxReadinessTimeout bounds a readiness wait even for untrusted callers.
	MaxReadinessTimeout   = 2 * time.Minute
	readinessPollInterval = 10 * time.Millisecond
)

type readinessService struct {
	Service      string `json:"service"`
	Ready        bool   `json:"ready"`
	State        string `json:"state"`
	TargetCount  int    `json:"targetCount"`
	ReadyTargets int    `json:"readyTargets"`
}

type readinessResponse struct {
	Ready    bool               `json:"ready"`
	Services []readinessService `json:"services"`
	WaitedMs int64              `json:"waitedMs"`
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	services, err := parseServiceNames(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	timeout, err := parseReadinessTimeout(r.URL.Query().Get("timeout"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	started := time.Now()
	result := readinessResponse{Services: []readinessService{}}
	for {
		result.Services, result.Ready = s.readinessSnapshot(services)
		if result.Ready || timeout == 0 {
			break
		}
		remaining := timeout - time.Since(started)
		if remaining <= 0 {
			break
		}
		wait := readinessPollInterval
		if remaining < wait {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-r.Context().Done():
			if !timer.Stop() {
				<-timer.C
			}
			result.WaitedMs = time.Since(started).Milliseconds()
			writeError(w, http.StatusRequestTimeout, "readiness request canceled before services became ready")
			return
		case <-timer.C:
		}
	}
	result.WaitedMs = time.Since(started).Milliseconds()
	status := http.StatusOK
	if !result.Ready {
		status = http.StatusRequestTimeout
	}
	writeJSON(w, status, result)
}

func parseServiceNames(r *http.Request) ([]string, error) {
	query := r.URL.Query()
	raw := append([]string(nil), query["service"]...)
	raw = append(raw, query["services"]...)
	seen := map[string]struct{}{}
	services := make([]string, 0, len(raw))
	for _, value := range raw {
		for _, service := range strings.Split(value, ",") {
			service = strings.TrimSpace(service)
			if service == "" {
				return nil, errors.New("service query parameter must contain at least one service")
			}
			if _, ok := seen[service]; ok {
				continue
			}
			seen[service] = struct{}{}
			services = append(services, service)
		}
	}
	if len(services) == 0 {
		return nil, errors.New("service query parameter is required")
	}
	sort.Strings(services)
	return services, nil
}

func parseReadinessTimeout(raw string) (time.Duration, error) {
	if raw == "" {
		return DefaultReadinessTimeout, nil
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout < 0 {
		return 0, errors.New("timeout query parameter must be a non-negative duration")
	}
	if timeout > MaxReadinessTimeout {
		return 0, errors.New("timeout query parameter must not exceed 2m")
	}
	return timeout, nil
}

func (s *Server) readinessSnapshot(services []string) ([]readinessService, bool) {
	targets := s.targets.Targets()
	result := make([]readinessService, 0, len(services))
	ready := true
	for _, service := range services {
		state := readinessService{Service: service, State: "missing"}
		for _, target := range targets {
			if target.ServiceName != service {
				continue
			}
			state.TargetCount++
			if target.Status == model.TargetStatusUp && successfulScrape(target.LastScrapeAt) {
				state.ReadyTargets++
			}
		}
		if state.TargetCount > 0 {
			switch {
			case state.ReadyTargets == state.TargetCount:
				state.State = "ready"
				state.Ready = true
			case hasDownTarget(targets, service):
				state.State = "down"
			default:
				state.State = "pending"
			}
		}
		if !state.Ready {
			ready = false
		}
		result = append(result, state)
	}
	return result, ready
}

func successfulScrape(value string) bool {
	if value == "" {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func hasDownTarget(targets []model.Target, service string) bool {
	for _, target := range targets {
		if target.ServiceName == service && target.Status != model.TargetStatusUp {
			return true
		}
	}
	return false
}
