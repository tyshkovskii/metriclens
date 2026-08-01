package api

import (
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"metriclens/backend/internal/diagnosis"
)

var errInvalidReportFilter = errors.New("report filter contains an invalid value")

func parseReportOptions(values url.Values) (diagnosis.BuildOptions, error) {
	severities, err := parseReportFilterList(values, "severity")
	if err != nil {
		return diagnosis.BuildOptions{}, err
	}
	for _, severity := range severities {
		switch severity {
		case "info", "warning", "error":
		default:
			return diagnosis.BuildOptions{}, errors.New("severity must be info, warning, or error")
		}
	}
	services, err := parseReportFilterList(values, "service", "services")
	if err != nil {
		return diagnosis.BuildOptions{}, err
	}
	changedOnly, err := parseChangedOnly(values["changedOnly"])
	if err != nil {
		return diagnosis.BuildOptions{}, err
	}
	return diagnosis.BuildOptions{Severities: severities, Services: services, ChangedOnly: changedOnly}, nil
}

func parseReportFilterList(values url.Values, keys ...string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0)
	for _, key := range keys {
		for _, raw := range values[key] {
			for _, value := range strings.Split(raw, ",") {
				value = strings.TrimSpace(value)
				if value == "" {
					return nil, errInvalidReportFilter
				}
				if _, ok := seen[value]; ok {
					continue
				}
				seen[value] = struct{}{}
				result = append(result, value)
			}
		}
	}
	sort.Strings(result)
	return result, nil
}

func parseChangedOnly(values []string) (bool, error) {
	if len(values) == 0 {
		return false, nil
	}
	var parsedValue bool
	for index, value := range values {
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return false, errors.New("changedOnly must be a boolean")
		}
		if index == 0 {
			parsedValue = parsed
			continue
		}
		if parsed != parsedValue {
			return false, errors.New("changedOnly values must agree")
		}
	}
	return parsedValue, nil
}
