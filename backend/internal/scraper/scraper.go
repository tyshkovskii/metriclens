package scraper

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"metriclens/backend/internal/model"
	"metriclens/backend/internal/promtext"
)

const (
	DefaultInterval = 5 * time.Second

	defaultHTTPTimeout = 2 * time.Second
	maxScrapeBody      = 10 * 1024 * 1024
)

type ContainerLister interface {
	ListContainers(context.Context) ([]model.DiscoveredContainer, error)
}

type TargetProber interface {
	Probe(context.Context, []model.DiscoveredContainer) []model.Target
}

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type SeriesStore interface {
	Record(targetID string, families []model.MetricFamily, scrapedAt time.Time)
	// Series returns stored series for a metric. A nil labels map matches
	// every series of the metric; a non-nil map (even an empty one) matches
	// only the series whose label set is exactly equal.
	Series(targetID, metric string, labels map[string]string) []model.Series
}

type Scraper struct {
	containers ContainerLister
	prober     TargetProber
	client     HTTPClient
	series     SeriesStore
	interval   time.Duration
	now        func() time.Time

	mu        sync.RWMutex
	targets   map[string]model.Target
	families  map[string][]model.MetricFamily
	lastError error
}

func New(containers ContainerLister, prober TargetProber, client HTTPClient, series SeriesStore, interval time.Duration) *Scraper {
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Scraper{
		containers: containers,
		prober:     prober,
		client:     client,
		series:     series,
		interval:   interval,
		now:        time.Now,
		targets:    map[string]model.Target{},
		families:   map[string][]model.MetricFamily{},
	}
}

func (s *Scraper) Start(ctx context.Context) {
	go func() {
		if err := s.RunOnce(ctx); err != nil {
			log.Printf("initial scrape failed: %v", err)
		}

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.RunOnce(ctx); err != nil {
					log.Printf("scrape failed: %v", err)
				}
			}
		}
	}()
}

func (s *Scraper) RunOnce(ctx context.Context) error {
	containers, err := s.containers.ListContainers(ctx)
	if err != nil {
		s.setLastError(err)
		return err
	}

	probedTargets := s.prober.Probe(ctx, containers)
	nextTargets := make(map[string]model.Target, len(probedTargets))
	nextFamilies := map[string][]model.MetricFamily{}

	for _, target := range probedTargets {
		if target.Status == model.TargetStatusUp && target.URL != "" {
			scrapedTarget, families, ok := s.scrapeTarget(ctx, target)
			target = scrapedTarget
			if ok {
				nextFamilies[target.ID] = families
			}
		}
		nextTargets[target.ID] = target
	}

	s.mu.Lock()
	for targetID, families := range s.families {
		if _, ok := nextTargets[targetID]; ok {
			if _, refreshed := nextFamilies[targetID]; !refreshed {
				nextFamilies[targetID] = families
			}
		}
	}
	for targetID, families := range nextFamilies {
		s.families[targetID] = families
	}
	for targetID := range s.families {
		if _, ok := nextTargets[targetID]; !ok {
			delete(s.families, targetID)
		}
	}
	s.targets = nextTargets
	s.lastError = nil
	s.mu.Unlock()

	return nil
}

func (s *Scraper) Targets() []model.Target {
	s.mu.RLock()
	defer s.mu.RUnlock()

	targets := make([]model.Target, 0, len(s.targets))
	for _, target := range s.targets {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].ServiceName == targets[j].ServiceName {
			return targets[i].ContainerName < targets[j].ContainerName
		}
		return targets[i].ServiceName < targets[j].ServiceName
	})
	return targets
}

func (s *Scraper) TargetMetrics(targetID string) (model.TargetMetricsResponse, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	target, ok := s.targets[targetID]
	if !ok {
		return model.TargetMetricsResponse{}, false
	}

	families := append([]model.MetricFamily(nil), s.families[targetID]...)
	if families == nil {
		families = []model.MetricFamily{}
	}
	return model.TargetMetricsResponse{
		Target:   target,
		Families: families,
	}, true
}

func (s *Scraper) TargetSeries(targetID, metric string, labels map[string]string) []model.Series {
	if s.series == nil {
		return []model.Series{}
	}
	return s.series.Series(targetID, metric, labels)
}

func (s *Scraper) LastError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastError
}

func (s *Scraper) scrapeTarget(ctx context.Context, target model.Target) (model.Target, []model.MetricFamily, bool) {
	startedAt := time.Now()
	resp, err := s.get(ctx, target.URL)
	duration := time.Since(startedAt)
	scrapedAt := s.now().UTC()

	target.LastScrapeAt = scrapedAt.Format(time.RFC3339)
	target.LastScrapeDuration = duration.String()

	if err != nil {
		target.Status = model.TargetStatusDown
		target.LastError = err.Error()
		return target, nil, false
	}

	families, err := promtext.Parse(strings.NewReader(resp))
	if err != nil {
		target.Status = model.TargetStatusDown
		target.LastError = fmt.Sprintf("parse metrics from %s: %v", target.URL, err)
		return target, nil, false
	}

	if s.series != nil {
		s.series.Record(target.ID, families, scrapedAt)
	}

	target.Status = model.TargetStatusUp
	target.LastError = ""
	return target, families, true
}

func (s *Scraper) get(ctx context.Context, url string) (string, error) {
	// #nosec G107 -- metric endpoints are intentionally discovered from local Docker metadata.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create scrape request for %s: %w", url, err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("scrape %s failed: %w", url, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("close scrape response body: %v", closeErr)
		}
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("scrape %s returned HTTP %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxScrapeBody))
	if err != nil {
		return "", fmt.Errorf("read scrape response from %s: %w", url, err)
	}
	return string(body), nil
}

func (s *Scraper) setLastError(err error) {
	s.mu.Lock()
	s.lastError = err
	s.mu.Unlock()
}
