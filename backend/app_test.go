package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestApp builds an App whose "random" number is fixed, so error injection is deterministic.
func newTestApp(cfg Config, random float64) *App {
	return NewApp(cfg, "pod-a", func() float64 { return random })
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) ColorResponse {
	t.Helper()
	var body ColorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	return body
}

func TestColorReturnsVersionColorAndPod(t *testing.T) {
	app := newTestApp(Config{Version: "v2", Color: "mediumseagreen"}, 0.99)
	rec := get(app.Routes(), "/api/color")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decode(t, rec)
	if body.Version != "v2" || body.Color != "mediumseagreen" || body.Pod != "pod-a" || body.Error != "" {
		t.Fatalf("unexpected body %+v", body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestColorFailsWhenRandomBelowErrorRate(t *testing.T) {
	app := newTestApp(Config{Version: "v3", Color: "darkorange", ErrorRate: 0.3}, 0.1)
	rec := get(app.Routes(), "/api/color")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := decode(t, rec)
	if body.Version != "v3" || body.Error != "simulated failure" {
		t.Fatalf("unexpected body %+v", body)
	}
}

func TestColorSucceedsWhenRandomAboveErrorRate(t *testing.T) {
	app := newTestApp(Config{Version: "v3", ErrorRate: 0.3}, 0.5)
	if rec := get(app.Routes(), "/api/color"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReadyzFollowsReadiness(t *testing.T) {
	app := newTestApp(Config{}, 0)
	if rec := get(app.Routes(), "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("before ready: status = %d, want 503", rec.Code)
	}
	app.SetReady(true)
	if rec := get(app.Routes(), "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("after ready: status = %d, want 200", rec.Code)
	}
}

func TestHealthzAlwaysOK(t *testing.T) {
	app := newTestApp(Config{}, 0)
	if rec := get(app.Routes(), "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestIndexPagePaintsTheColor(t *testing.T) {
	app := newTestApp(Config{Version: "v2", Color: "mediumseagreen"}, 0.99)
	rec := get(app.Routes(), "/")
	body := rec.Body.String()
	if !strings.Contains(body, "background:mediumseagreen") || !strings.Contains(body, "v2") {
		t.Fatalf("index page missing color or version: %s", body)
	}
}
