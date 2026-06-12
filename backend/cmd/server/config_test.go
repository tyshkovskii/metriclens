package main

import (
	"testing"
	"time"
)

func TestDurationFromEnv(t *testing.T) {
	t.Setenv(scrapeIntervalEnv, "250ms")

	parsed, err := durationFromEnv(scrapeIntervalEnv, time.Second)
	if err != nil {
		t.Fatalf("durationFromEnv() error = %v", err)
	}
	if parsed != 250*time.Millisecond {
		t.Fatalf("duration = %s, want 250ms", parsed)
	}
}

func TestDurationFromEnvUsesFallback(t *testing.T) {
	t.Setenv(retentionEnv, "")

	parsed, err := durationFromEnv(retentionEnv, 30*time.Second)
	if err != nil {
		t.Fatalf("durationFromEnv() error = %v", err)
	}
	if parsed != 30*time.Second {
		t.Fatalf("duration = %s, want fallback 30s", parsed)
	}
}

func TestDurationFromEnvRejectsInvalidValue(t *testing.T) {
	t.Setenv(scrapeIntervalEnv, "nope")

	if _, err := durationFromEnv(scrapeIntervalEnv, time.Second); err == nil {
		t.Fatal("durationFromEnv() error = nil, want error")
	}
}

func TestDurationFromEnvRejectsNonPositive(t *testing.T) {
	t.Setenv(scrapeIntervalEnv, "-5s")

	if _, err := durationFromEnv(scrapeIntervalEnv, time.Second); err == nil {
		t.Fatal("durationFromEnv() error = nil, want error")
	}
}
