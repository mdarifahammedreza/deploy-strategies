package traffic

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// NewClient returns an HTTP client that opens a NEW TCP connection for every request.
// kube-proxy load-balances per connection, so with keep-alive every request would
// hit the same pod and the traffic bar would lie.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
}

// Prober sends RPS requests per second to Target and records each answer in Window.
type Prober struct {
	Target string
	RPS    int
	Window *Window
	Client *http.Client
}

// Run probes until ctx is cancelled. Each probe runs in its own goroutine so a slow
// pod never slows down the probe rate.
func (p *Prober) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second / time.Duration(p.RPS))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			go func() { p.Window.Add(p.ProbeOnce(ctx)) }()
		}
	}
}

// ProbeOnce sends one request and converts the answer into a Result.
func (p *Prober) ProbeOnce(ctx context.Context) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Target, nil)
	if err != nil {
		return Result{Version: Unreachable}
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return Result{Version: Unreachable}
	}
	defer resp.Body.Close()

	var body struct {
		Version string `json:"version"`
		Color   string `json:"color"`
		Pod     string `json:"pod"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Version == "" {
		return Result{Version: Unreachable}
	}
	return Result{Pod: body.Pod, Version: body.Version, Color: body.Color, OK: resp.StatusCode == http.StatusOK}
}
