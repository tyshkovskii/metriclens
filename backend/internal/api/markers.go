package api

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

type Marker struct {
	ID          string `json:"id"`
	CreatedAt   string `json:"createdAt"`
	Name        string `json:"name,omitempty"`
	ClientRunID string `json:"clientRunId,omitempty"`
}

type storedMarker struct {
	Marker
	time time.Time
}

type markerStore struct {
	mu      sync.Mutex
	nextID  uint64
	markers map[string]storedMarker
}

func newMarkerStore() *markerStore {
	return &markerStore{markers: map[string]storedMarker{}}
}

func (m *markerStore) create(now time.Time, retention time.Duration, name, clientRunID string) Marker {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(now, retention)
	m.nextID++
	marker := storedMarker{
		Marker: Marker{ID: fmt.Sprintf("marker-%d", m.nextID), CreatedAt: now.UTC().Format(time.RFC3339Nano), Name: name, ClientRunID: clientRunID},
		time:   now.UTC(),
	}
	m.markers[marker.ID] = marker
	return marker.Marker
}

func (m *markerStore) list(now time.Time, retention time.Duration) []Marker {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(now, retention)
	markers := make([]storedMarker, 0, len(m.markers))
	for _, marker := range m.markers {
		markers = append(markers, marker)
	}
	sort.Slice(markers, func(i, j int) bool {
		if markers[i].time.Equal(markers[j].time) {
			return markers[i].ID < markers[j].ID
		}
		return markers[i].time.Before(markers[j].time)
	})
	result := make([]Marker, 0, len(markers))
	for _, marker := range markers {
		result = append(result, marker.Marker)
	}
	return result
}

func (m *markerStore) get(id string, now time.Time, retention time.Duration) (storedMarker, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(now, retention)
	marker, ok := m.markers[id]
	return marker, ok
}

func (m *markerStore) expireLocked(now time.Time, retention time.Duration) {
	cutoff := now.UTC().Add(-retention)
	for id, marker := range m.markers {
		if marker.time.Before(cutoff) {
			delete(m.markers, id)
		}
	}
}
