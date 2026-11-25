package main

import (
	"fmt"
	"strconv"
	"time"
)

// config is every setting of the dashboard, read from environment variables.
type config struct {
	Namespace   string        // namespace to watch
	Selector    string        // label selector of backend pods
	Target      string        // URL the traffic prober calls (the Service today, an ALB later)
	BackendPort string        // port the identity prober uses on each pod IP
	Listen      string        // address of the dashboard's own HTTP server
	StaticDir   string        // folder with the built Vite frontend
	RPS         int           // traffic probe requests per second
	Window      int           // how many recent requests the traffic bar is based on
	Tick        time.Duration // how often a snapshot is pushed to browsers
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		Namespace:   or(getenv("NAMESPACE"), "color-demo"),
		Selector:    or(getenv("POD_SELECTOR"), "app=color-backend"),
		Target:      or(getenv("TARGET_URL"), "http://color-backend/api/color"),
		BackendPort: or(getenv("BACKEND_PORT"), "8080"),
		Listen:      or(getenv("LISTEN"), ":8081"),
		StaticDir:   or(getenv("STATIC_DIR"), "./web"),
	}
	var err error
	if cfg.RPS, err = positiveInt(getenv("PROBE_RPS"), 20); err != nil {
		return config{}, fmt.Errorf("PROBE_RPS: %w", err)
	}
	if cfg.Window, err = positiveInt(getenv("WINDOW_SIZE"), 100); err != nil {
		return config{}, fmt.Errorf("WINDOW_SIZE: %w", err)
	}
	if cfg.Tick, err = time.ParseDuration(or(getenv("PUSH_INTERVAL"), "500ms")); err != nil {
		return config{}, fmt.Errorf("PUSH_INTERVAL: %w", err)
	}
	return cfg, nil
}

func or(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func positiveInt(v string, fallback int) (int, error) {
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("must be greater than 0, got %d", n)
	}
	return n, nil
}
