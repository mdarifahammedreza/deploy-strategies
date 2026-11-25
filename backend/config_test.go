package main

import (
	"testing"
	"time"
)

// env turns a map into a getenv-style function so tests never touch real env vars.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig(env(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Config{Version: "v1", Color: "royalblue", Port: "8080"}
	if cfg != want {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadConfigReadsEnv(t *testing.T) {
	cfg, err := LoadConfig(env(map[string]string{
		"VERSION": "v3", "COLOR": "darkorange", "PORT": "9090",
		"STARTUP_DELAY": "5s", "ERROR_RATE": "0.3", "NEVER_READY": "true",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Config{Version: "v3", Color: "darkorange", Port: "9090",
		StartupDelay: 5 * time.Second, ErrorRate: 0.3, NeverReady: true}
	if cfg != want {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadConfigRejectsBadValues(t *testing.T) {
	cases := map[string]map[string]string{
		"error rate above 1":   {"ERROR_RATE": "1.5"},
		"error rate not num":   {"ERROR_RATE": "abc"},
		"delay not duration":   {"STARTUP_DELAY": "soon"},
		"never ready not bool": {"NEVER_READY": "maybe"},
	}
	for name, vars := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadConfig(env(vars)); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}
