package main

import (
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := config{
		Namespace: "color-demo", Selector: "app=color-backend", Target: "http://color-backend/api/color",
		BackendPort: "8080", Listen: ":8081", StaticDir: "./web", RPS: 20, Window: 100, Tick: 500 * time.Millisecond,
	}
	if cfg != want {
		t.Fatalf("got %+v\nwant %+v", cfg, want)
	}
}

func TestLoadConfigRejectsBadNumbers(t *testing.T) {
	for _, vars := range []map[string]string{
		{"PROBE_RPS": "0"}, {"PROBE_RPS": "x"}, {"WINDOW_SIZE": "-1"}, {"PUSH_INTERVAL": "often"},
	} {
		if _, err := loadConfig(env(vars)); err == nil {
			t.Errorf("expected error for %v", vars)
		}
	}
}
