package scraper

import (
	"fmt"
	"os"
	"time"
)

const (
	scrapeIntervalEnv = "metriclens_SCRAPE_INTERVAL"
	DefaultInterval   = 5 * time.Second
)

func IntervalFromEnv() (time.Duration, error) {
	value := os.Getenv(scrapeIntervalEnv)
	if value == "" {
		return DefaultInterval, nil
	}

	interval, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", scrapeIntervalEnv, value, err)
	}
	if interval <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be positive", scrapeIntervalEnv, value)
	}
	return interval, nil
}
