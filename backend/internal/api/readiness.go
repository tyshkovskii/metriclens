package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"metriclens/backend/internal/model"
)

const (
	// DefaultReadinessTimeout is used when readiness callers omit timeout. It
	// spans several default scrape intervals because readiness needs a scrape
	// that completes after the wait starts.
	DefaultReadinessTimeout = 30 * time.Second
	// MaxReadinessTimeout bounds a readiness wait even for untrusted callers.
	MaxReadinessTimeout = 2 * time.Minute
	// HTTPWriteTimeout leaves enough time for the longest readiness request to
	// complete before the production HTTP server writes its response.
	HTTPWriteTimeout = MaxReadinessTimeout + 5*time.Second
	// readinessPollInterval stays short next to the scrape interval a wait
	// spans, without copying the target list every few milliseconds.
	readinessPollInterval = 100 * time.Millisecond
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

	result, err := s.waitForReadiness(r.Context(), services, timeout)
	if err != nil {
		writeError(w, http.StatusRequestTimeout, "readiness request canceled before services became ready")
		return
	}
	status := http.StatusOK
	if !result.Ready {
		status = http.StatusRequestTimeout
	}
	writeJSON(w, status, result)
}

func (s *Server) waitForReadiness(ctx context.Context, services []string, timeout time.Duration) (readinessResponse, error) {
	started := time.Now()
	// Earlier scrapes may describe containers that have since stopped or been
	// recreated, so only a scrape completed after the wait starts counts.
	scrapedAfter := s.currentTime()
	result := readinessResponse{Services: []readinessService{}}
	for {
		result.Services, result.Ready = s.readinessSnapshot(services, scrapedAfter)
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
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return readinessResponse{}, ctx.Err()
		case <-timer.C:
		}
	}
	result.WaitedMs = time.Since(started).Milliseconds()
	return result, nil
}

func parseServiceNames(r *http.Request) ([]string, error) {
	query := r.URL.Query()
	raw := append([]string(nil), query["service"]...)
	raw = append(raw, query["services"]...)
	return parseServiceList(raw)
}

func parseServiceList(raw []string) ([]string, error) {
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

func (s *Server) readinessSnapshot(services []string, scrapedAfter time.Time) ([]readinessService, bool) {
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
			if target.Status == model.TargetStatusUp && scrapedSince(target.LastScrapeAt, scrapedAfter) {
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

// scrapedSince reports whether a scrape completed at or after since.
// LastScrapeAt is truncated to whole seconds, so this comparison can delay
// readiness but never accepts a scrape that finished before since.
func scrapedSince(value string, since time.Time) bool {
	scrapedAt, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && !scrapedAt.Before(since)
}

func hasDownTarget(targets []model.Target, service string) bool {
	for _, target := range targets {
		if target.ServiceName == service && target.Status != model.TargetStatusUp {
			return true
		}
	}
	return false
}
