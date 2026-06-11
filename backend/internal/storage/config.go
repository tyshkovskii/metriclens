package storage

import (
	"fmt"
	"os"
	"time"
)

const (
	retentionEnv     = "metriclens_RETENTION"
	DefaultRetention = 15 * time.Minute
)

func RetentionFromEnv() (time.Duration, error) {
	value := os.Getenv(retentionEnv)
	if value == "" {
		return DefaultRetention, nil
	}

	retention, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", retentionEnv, value, err)
	}
	if retention <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be positive", retentionEnv, value)
	}
	return retention, nil
}
