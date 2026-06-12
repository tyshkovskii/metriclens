package main

import (
	"fmt"
	"os"
	"time"
)

// Environment configuration is read here in the composition root; library
// packages take plain durations and own only their defaults.
const (
	scrapeIntervalEnv = "metriclens_SCRAPE_INTERVAL"
	retentionEnv      = "metriclens_RETENTION"
)

func durationFromEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, value, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be positive", name, value)
	}
	return parsed, nil
}
