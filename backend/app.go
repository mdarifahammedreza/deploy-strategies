package main

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sync/atomic"
	"time"
)

// ColorResponse is the JSON body of GET /api/color.
type ColorResponse struct {
	Version  string    `json:"version"`
	Color    string    `json:"color"`
	Pod      string    `json:"pod"`
	ServedAt time.Time `json:"servedAt"`
	Error    string    `json:"error,omitempty"`
}

// App holds the HTTP handlers and the readiness flag.
type App struct {
	cfg     Config
	podName string         // in Kubernetes the hostname IS the pod name
	random  func() float64 // returns [0,1); injected so tests are deterministic
	ready   atomic.Bool    // flipped by main after STARTUP_DELAY, cleared on SIGTERM
}

func NewApp(cfg Config, podName string, random func() float64) *App {
	return &App{cfg: cfg, podName: podName, random: random}
}

// SetReady controls what /readyz answers.
func (a *App) SetReady(ready bool) { a.ready.Store(ready) }

// Routes wires URL paths to handlers (Go 1.22+ "METHOD /path" patterns).
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/color", a.handleColor)
	mux.HandleFunc("GET /healthz", a.handleHealthz)
	mux.HandleFunc("GET /readyz", a.handleReadyz)
	mux.HandleFunc("GET /{$}", a.handleIndex) // {$} = exactly "/", not a catch-all
	return mux
}

func (a *App) handleColor(w http.ResponseWriter, _ *http.Request) {
	resp := ColorResponse{Version: a.cfg.Version, Color: a.cfg.Color, Pod: a.podName, ServedAt: time.Now().UTC()}
	status := http.StatusOK
	if a.random() < a.cfg.ErrorRate {
		resp.Error = "simulated failure"
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// handleHealthz is the liveness probe: "the process is alive".
func (a *App) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("ok\n"))
}

// handleReadyz is the readiness probe: "send me traffic".
func (a *App) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	if !a.ready.Load() {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ready\n"))
}

// handleIndex paints the whole page in this version's color (handy for curl/browser demos).
func (a *App) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, indexHTML,
		html.EscapeString(a.cfg.Color), html.EscapeString(a.cfg.Version), html.EscapeString(a.podName))
}

const indexHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>color-backend</title></head>
<body style="margin:0;height:100vh;display:grid;place-items:center;background:%s;font-family:sans-serif;color:#fff;text-shadow:0 1px 3px #0008">
<h1>%s · served by %s</h1></body></html>
`
