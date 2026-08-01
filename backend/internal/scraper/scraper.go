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

type stableSeriesStore interface {
	RecordWithIdentity(historyID, targetID string, families []model.MetricFamily, scrapedAt time.Time)
	SeriesFor(historyID, targetID, metric string, labels map[string]string) []model.Series
	SeriesBatchFor(historyID, targetID string, metrics []string, start, end, at *time.Time) []model.Series
}

type retentionProvider interface {
	Retention() time.Duration
}

type Scraper struct {
	containers      ContainerLister
	prober          TargetProber
	client          HTTPClient
	series          SeriesStore
	interval        time.Duration
	intervalUpdates chan time.Duration
	now             func() time.Time
	retention       time.Duration

	mu         sync.RWMutex
	targets    map[string]model.Target
	families   map[string][]model.MetricFamily
	historyIDs map[string]string
	events     []model.LifecycleEvent
	lastError  error
}

func New(containers ContainerLister, prober TargetProber, client HTTPClient, series SeriesStore, interval time.Duration) *Scraper {
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	if interval <= 0 {
		interval = DefaultInterval
	}
	retention := 15 * time.Minute
	if provider, ok := series.(retentionProvider); ok && provider.Retention() > 0 {
		retention = provider.Retention()
	}
	return &Scraper{
		containers:      containers,
		prober:          prober,
		client:          client,
		series:          series,
		interval:        interval,
		intervalUpdates: make(chan time.Duration, 1),
		now:             time.Now,
		retention:       retention,
		targets:         map[string]model.Target{},
		families:        map[string][]model.MetricFamily{},
		historyIDs:      map[string]string{},
		events:          []model.LifecycleEvent{},
	}
}

func (s *Scraper) Start(ctx context.Context) {
	go func() {
		if err := s.RunOnce(ctx); err != nil {
			log.Printf("initial scrape failed: %v", err)
		}

		ticker := time.NewTicker(s.currentInterval())
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case interval := <-s.intervalUpdates:
				ticker.Stop()
				ticker = time.NewTicker(interval)
			case <-ticker.C:
				if err := s.RunOnce(ctx); err != nil {
					log.Printf("scrape failed: %v", err)
				}
			}
		}
	}()
}

// SetInterval changes the cadence used by the background scraper. The next
// tick uses the new duration; an in-flight scrape is allowed to finish.
func (s *Scraper) SetInterval(interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("scrape interval must be positive")
	}

	s.mu.Lock()
	s.interval = interval
	s.mu.Unlock()

	// Keep only the newest pending update if the API is called repeatedly while
	// the scraper goroutine is busy with a scrape.
	select {
	case s.intervalUpdates <- interval:
	default:
		select {
		case <-s.intervalUpdates:
		default:
		}
		s.intervalUpdates <- interval
	}
	return nil
}

func (s *Scraper) currentInterval() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.interval
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
	nextHistoryIDs := make(map[string]string, len(probedTargets))

	for _, target := range probedTargets {
		if target.HistoryID == "" {
			target.HistoryID = target.ID
		}
		nextHistoryIDs[target.ID] = target.HistoryID
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
	s.recordLifecycleEventsLocked(nextTargets, s.now().UTC())
	s.targets = nextTargets
	s.historyIDs = nextHistoryIDs
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

// LifecycleEvents returns retained target lifecycle changes in the requested
// inclusive time range. Events are snapshots, so disappeared targets remain
// diagnosable after they leave Targets().
func (s *Scraper) LifecycleEvents(start, end time.Time) []model.LifecycleEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]model.LifecycleEvent, 0)
	for _, event := range s.events {
		timestamp, err := time.Parse(time.RFC3339Nano, event.At)
		if err != nil || timestamp.Before(start) || timestamp.After(end) {
			continue
		}
		result = append(result, event)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].At != result[j].At {
			return result[i].At < result[j].At
		}
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].TargetID < result[j].TargetID
	})
	return result
}

func (s *Scraper) recordLifecycleEventsLocked(next map[string]model.Target, at time.Time) {
	previous := make(map[string]model.Target, len(s.targets))
	for targetID, target := range s.targets {
		previous[targetHistoryID(targetID, target)] = target
	}
	seen := make(map[string]struct{}, len(next))
	at = at.UTC()
	for targetID, target := range next {
		identity := targetHistoryID(targetID, target)
		seen[identity] = struct{}{}
		old, ok := previous[identity]
		if !ok {
			s.events = append(s.events, model.LifecycleEvent{
				At:            at.Format(time.RFC3339Nano),
				Kind:          model.LifecycleEventAppeared,
				TargetID:      target.ID,
				ServiceName:   target.ServiceName,
				ContainerName: target.ContainerName,
				To:            target.Status,
				Error:         target.LastError,
				HistoryID:     identity,
			})
			continue
		}
		if old.ID != target.ID {
			s.events = append(s.events, model.LifecycleEvent{
				At:            at.Format(time.RFC3339Nano),
				Kind:          model.LifecycleEventRecreated,
				TargetID:      target.ID,
				ServiceName:   target.ServiceName,
				ContainerName: target.ContainerName,
				To:            target.Status,
				Error:         target.LastError,
				HistoryID:     identity,
			})
		}
		if old.Status != target.Status {
			s.events = append(s.events, model.LifecycleEvent{
				At:            at.Format(time.RFC3339Nano),
				Kind:          model.LifecycleEventStatusTransition,
				TargetID:      target.ID,
				ServiceName:   target.ServiceName,
				ContainerName: target.ContainerName,
				From:          old.Status,
				To:            target.Status,
				Error:         target.LastError,
				HistoryID:     identity,
			})
		}
	}
	for identity, target := range previous {
		if _, ok := seen[identity]; ok {
			continue
		}
		s.events = append(s.events, model.LifecycleEvent{
			At:            at.Format(time.RFC3339Nano),
			Kind:          model.LifecycleEventDisappeared,
			TargetID:      target.ID,
			ServiceName:   target.ServiceName,
			ContainerName: target.ContainerName,
			From:          target.Status,
			Error:         target.LastError,
			HistoryID:     identity,
		})
	}
	cutoff := at.Add(-s.retention)
	kept := s.events[:0]
	for _, event := range s.events {
		timestamp, err := time.Parse(time.RFC3339Nano, event.At)
		if err == nil && !timestamp.Before(cutoff) {
			kept = append(kept, event)
		}
	}
	s.events = kept
}

func targetHistoryID(targetID string, target model.Target) string {
	if target.HistoryID != "" {
		return target.HistoryID
	}
	return targetID
}

func (s *Scraper) TargetSeries(targetID, metric string, labels map[string]string) []model.Series {
	if s.series == nil {
		return []model.Series{}
	}
	s.mu.RLock()
	historyID := s.historyIDs[targetID]
	s.mu.RUnlock()
	if historyID == "" {
		historyID = targetID
	}
	if stable, ok := s.series.(stableSeriesStore); ok {
		return stable.SeriesFor(historyID, targetID, metric, labels)
	}
	return s.series.Series(targetID, metric, labels)
}

// TargetSeriesBatch returns selected metric history for a bounded time range.
// A nil at/start/end leaves that bound unset; at takes precedence in storage
// and returns one last point per matching series.
func (s *Scraper) TargetSeriesBatch(targetID string, metrics []string, start, end, at *time.Time) []model.Series {
	if s.series == nil {
		return []model.Series{}
	}
	s.mu.RLock()
	historyID := s.historyIDs[targetID]
	s.mu.RUnlock()
	if historyID == "" {
		historyID = targetID
	}
	if stable, ok := s.series.(stableSeriesStore); ok {
		return stable.SeriesBatchFor(historyID, targetID, metrics, start, end, at)
	}

	result := make([]model.Series, 0)
	for _, metric := range metrics {
		for _, series := range s.series.Series(targetID, metric, nil) {
			points := filterSeriesPoints(series.Points, start, end, at)
			if len(points) == 0 {
				continue
			}
			series.Points = points
			result = append(result, series)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Metric != result[j].Metric {
			return result[i].Metric < result[j].Metric
		}
		return labelsKey(result[i].Labels) < labelsKey(result[j].Labels)
	})
	return result
}

func filterSeriesPoints(points []model.SeriesPoint, start, end, at *time.Time) []model.SeriesPoint {
	if at != nil {
		var selected *model.SeriesPoint
		for i := range points {
			pointTime, err := time.Parse(time.RFC3339Nano, points[i].TS)
			if err != nil || pointTime.After(*at) {
				continue
			}
			point := points[i]
			if selected == nil {
				selected = &point
				continue
			}
			selectedTime, err := time.Parse(time.RFC3339Nano, selected.TS)
			if err != nil || pointTime.After(selectedTime) {
				selected = &point
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
		if stable, ok := s.series.(stableSeriesStore); ok {
			stable.RecordWithIdentity(target.HistoryID, target.ID, families, scrapedAt)
		} else {
			s.series.Record(target.ID, families, scrapedAt)
		}
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
