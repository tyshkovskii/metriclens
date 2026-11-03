package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"metriclens/backend/internal/diagnosis"
	"metriclens/backend/internal/storage"
)

var (
	errPositiveLimit       = errors.New("limit query parameter must be a positive integer")
	errMaximumLimit        = errors.New("limit query parameter exceeds maximum")
	errUnknownBaselineFrom = errors.New("unknown or expired baselineFrom marker")
	errUnknownBaselineTo   = errors.New("unknown or expired baselineTo marker")
)

// MaxMarkerFieldRunes bounds optional marker names and client run IDs.
const MaxMarkerFieldRunes = 128

func (s *Server) handleCreateMarker(w http.ResponseWriter, r *http.Request) {
	request, err := decodeMarkerRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := s.currentTime()
	writeJSON(w, http.StatusCreated, s.markers.create(now, s.effectiveRetention(), request.Name, request.ClientRunID, diagnosis.QualityWarnings(s.targets)))
}

func (s *Server) handleListMarkers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.markers.list(s.currentTime(), s.effectiveRetention()))
}

func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	options, err := parseReportOptions(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fromID := r.URL.Query().Get("from")
	if fromID == "" {
		writeError(w, http.StatusBadRequest, "from query parameter is required")
		return
	}
	now := s.currentTime()
	retention := s.effectiveRetention()
	from, ok := s.markers.get(fromID, now, retention)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown or expired from marker")
		return
	}

	toTime := now
	var toMarker *storedMarker
	if toID := r.URL.Query().Get("to"); toID != "" {
		to, ok := s.markers.get(toID, now, retention)
		if !ok {
			writeError(w, http.StatusNotFound, "unknown or expired to marker")
			return
		}
		toTime = to.time
		toMarker = &to
	}
	if !from.time.Before(toTime) {
		writeError(w, http.StatusBadRequest, "from marker must be before to marker")
		return
	}

	limit, err := parseReportLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	options.BaselineStart, options.BaselineEnd, err = s.compareBaseline(from, toTime, r.URL.Query().Get("baselineFrom"), r.URL.Query().Get("baselineTo"), now, retention)
	if errors.Is(err, errUnknownBaselineFrom) || errors.Is(err, errUnknownBaselineTo) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	options.BaselineQualityWarnings = from.qualityWarnings
	if toMarker != nil {
		options.EndQualityWarnings = toMarker.qualityWarnings
	}
	report := diagnosis.BuildWithOptions(s.targets, toTime, toTime.Sub(from.time), limit, options)
	report.From = markerRef(from.Marker)
	if toMarker != nil {
		report.To = markerRef(toMarker.Marker)
	}
	writeJSON(w, http.StatusOK, report)
}

// compareBaseline resolves the window a comparison is judged against: the
// baseline markers when given, otherwise the equal-length window before from.
// With only baselineFrom, the baseline runs until from, which fits an earlier
// run of the same workload.
func (s *Server) compareBaseline(from storedMarker, toTime time.Time, baselineFromID, baselineToID string, now time.Time, retention time.Duration) (time.Time, time.Time, error) {
	baselineFromID, baselineToID = strings.TrimSpace(baselineFromID), strings.TrimSpace(baselineToID)
	if baselineFromID == "" {
		if baselineToID != "" {
			return time.Time{}, time.Time{}, errors.New("baselineTo requires baselineFrom")
		}
		return from.time.Add(-toTime.Sub(from.time)), from.time, nil
	}
	baselineFrom, ok := s.markers.get(baselineFromID, now, retention)
	if !ok {
		return time.Time{}, time.Time{}, errUnknownBaselineFrom
	}
	end := from.time
	if baselineToID != "" {
		baselineTo, ok := s.markers.get(baselineToID, now, retention)
		if !ok {
			return time.Time{}, time.Time{}, errUnknownBaselineTo
		}
		end = baselineTo.time
	}
	if !baselineFrom.time.Before(end) {
		return time.Time{}, time.Time{}, errors.New("baselineFrom marker must be before baselineTo or from")
	}
	return baselineFrom.time, end, nil
}

func markerRef(marker Marker) *diagnosis.MarkerRef {
	return &diagnosis.MarkerRef{
		ID: marker.ID, CreatedAt: marker.CreatedAt, Name: marker.Name, ClientRunID: marker.ClientRunID,
	}
}

func (s *Server) currentTime() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func (s *Server) effectiveRetention() time.Duration {
	s.configMu.RLock()
	retention := s.config.Retention
	s.configMu.RUnlock()
	if retention <= 0 {
		return storage.DefaultRetention
	}
	return retention
}

func parseReportLimit(raw string) (int, error) {
	if raw == "" {
		return diagnosis.DefaultLimit, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0, errPositiveLimit
	}
	if parsed > diagnosis.MaxLimit {
		return 0, errMaximumLimit
	}
	return parsed, nil
}

type markerRequest struct {
	Name        string `json:"name"`
	ClientRunID string `json:"clientRunId"`
}

func decodeMarkerRequest(r *http.Request) (markerRequest, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxMarkerFieldRunes*32+1))
	if err != nil {
		return markerRequest{}, errors.New("marker body could not be read")
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return markerRequest{}, nil
	}
	var request markerRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return markerRequest{}, errors.New("marker body must be a JSON object")
	}
	request.Name = strings.TrimSpace(request.Name)
	request.ClientRunID = strings.TrimSpace(request.ClientRunID)
	if err := validateMarkerField("name", request.Name); err != nil {
		return markerRequest{}, err
	}
	if err := validateMarkerField("clientRunId", request.ClientRunID); err != nil {
		return markerRequest{}, err
	}
	return request, nil
}

func validateMarkerField(name, value string) error {
	if utf8.RuneCountInString(value) > MaxMarkerFieldRunes {
		return fmt.Errorf("%s must be at most %d characters", name, MaxMarkerFieldRunes)
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("%s must not contain control characters", name)
	}
	return nil
}
