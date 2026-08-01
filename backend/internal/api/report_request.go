package api

import (
	"errors"
	"net/url"
	"time"

	"metriclens/backend/internal/diagnosis"
)

type reportRequest struct {
	Window  time.Duration
	Limit   int
	Options diagnosis.BuildOptions
}

func (s *Server) parseReportRequest(values url.Values) (reportRequest, error) {
	options, err := parseReportOptions(values)
	if err != nil {
		return reportRequest{}, err
	}
	window := diagnosis.DefaultWindow
	retention := s.effectiveRetention()
	if window > retention {
		window = retention
	}
	if raw := values.Get("window"); raw != "" {
		parsedDuration, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsedDuration <= 0 {
			return reportRequest{}, errors.New("window query parameter must be a positive duration")
		}
		window = parsedDuration
	}
	if window > retention {
		return reportRequest{}, errors.New("window query parameter must not exceed retention")
	}
	limit, err := parseReportLimit(values.Get("limit"))
	if err != nil {
		return reportRequest{}, err
	}
	return reportRequest{Window: window, Limit: limit, Options: options}, nil
}
