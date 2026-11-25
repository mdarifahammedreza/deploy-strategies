package main

import (
	"fmt"
	"strconv"
	"time"
)

// Config holds every setting the color API reads from environment variables.
type Config struct {
	Version      string        // label shown on the dashboard, e.g. "v2"
	Color        string        // any CSS color, e.g. "mediumseagreen"
	Port         string        // TCP port to listen on
	StartupDelay time.Duration // /readyz answers 503 for this long after start
	ErrorRate    float64       // share (0..1) of /api/color calls that return HTTP 500
	NeverReady   bool          // if true /readyz never succeeds (simulates a broken release)
}

// LoadConfig builds a Config from a getenv function (os.Getenv in production).
func LoadConfig(getenv func(string) string) (Config, error) {
	cfg := Config{
		Version: valueOr(getenv("VERSION"), "v1"),
		Color:   valueOr(getenv("COLOR"), "royalblue"),
		Port:    valueOr(getenv("PORT"), "8080"),
	}

	delay, err := time.ParseDuration(valueOr(getenv("STARTUP_DELAY"), "0s"))
	if err != nil {
		return Config{}, fmt.Errorf("STARTUP_DELAY: %w", err)
	}
	cfg.StartupDelay = delay

	rate, err := strconv.ParseFloat(valueOr(getenv("ERROR_RATE"), "0"), 64)
	if err != nil || rate < 0 || rate > 1 {
		return Config{}, fmt.Errorf("ERROR_RATE must be a number between 0 and 1, got %q", getenv("ERROR_RATE"))
	}
	cfg.ErrorRate = rate

	never, err := strconv.ParseBool(valueOr(getenv("NEVER_READY"), "false"))
	if err != nil {
		return Config{}, fmt.Errorf("NEVER_READY: %w", err)
	}
	cfg.NeverReady = never

	return cfg, nil
}

// valueOr returns v, or fallback when v is empty.
func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
