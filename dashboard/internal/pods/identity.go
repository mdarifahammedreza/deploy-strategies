package pods

import (
	"context"
	"encoding/json"
	"maps"
	"net"
	"net/http"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// ListFunc returns the current backend pods (from the informer cache in production).
type ListFunc func() ([]*corev1.Pod, error)

// Identity is what a pod says about itself on GET /api/color.
type Identity struct {
	Version string
	Color   string
}

// IdentityProber asks every pod directly (by pod IP, bypassing the Service) who it is.
// This colors the box even when the pod gets no traffic, e.g. the idle side of blue/green.
type IdentityProber struct {
	list     ListFunc
	port     string
	client   *http.Client
	interval time.Duration

	mu     sync.RWMutex
	byPod  map[string]Identity
	colors map[string]string // version → color, remembered forever
}

func NewIdentityProber(list ListFunc, port string, client *http.Client, interval time.Duration) *IdentityProber {
	return &IdentityProber{
		list: list, port: port, client: client, interval: interval,
		byPod: map[string]Identity{}, colors: map[string]string{},
	}
}

// Run probes all pods every interval until ctx is cancelled.
func (ip *IdentityProber) Run(ctx context.Context) {
	ticker := time.NewTicker(ip.interval)
	defer ticker.Stop()
	for {
		ip.ProbeAll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ProbeAll asks every pod that has an IP, in parallel.
func (ip *IdentityProber) ProbeAll(ctx context.Context) {
	podList, err := ip.list()
	if err != nil {
		return
	}

	var wg sync.WaitGroup
	var freshMu sync.Mutex
	fresh := make(map[string]Identity, len(podList))
	for _, p := range podList {
		if p.Status.PodIP == "" {
			continue
		}
		wg.Add(1)
		go func(name, addr string) {
			defer wg.Done()
			if id, ok := ip.probe(ctx, addr); ok {
				freshMu.Lock()
				fresh[name] = id
				freshMu.Unlock()
			}
		}(p.Name, p.Status.PodIP)
	}
	wg.Wait()

	ip.mu.Lock()
	defer ip.mu.Unlock()
	next := make(map[string]Identity, len(podList))
	for _, p := range podList {
		if id, ok := fresh[p.Name]; ok {
			next[p.Name] = id
			ip.colors[id.Version] = id.Color
		} else if old, ok := ip.byPod[p.Name]; ok {
			next[p.Name] = old // keep the last answer if this probe failed
		}
	}
	ip.byPod = next // pods that disappeared are forgotten
}

func (ip *IdentityProber) probe(ctx context.Context, addr string) (Identity, bool) {
	target := "http://" + net.JoinHostPort(addr, ip.port) + "/api/color"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Identity{}, false
	}
	resp, err := ip.client.Do(req)
	if err != nil {
		return Identity{}, false
	}
	defer resp.Body.Close()
	var body struct {
		Version string `json:"version"`
		Color   string `json:"color"`
	}
	// A 500 from a "bad" version still carries version and color, so we don't check the status code.
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Version == "" {
		return Identity{}, false
	}
	return Identity{Version: body.Version, Color: body.Color}, true
}

// Identities returns a copy of pod name → identity.
func (ip *IdentityProber) Identities() map[string]Identity {
	ip.mu.RLock()
	defer ip.mu.RUnlock()
	return maps.Clone(ip.byPod)
}

// Colors returns a copy of version → color.
func (ip *IdentityProber) Colors() map[string]string {
	ip.mu.RLock()
	defer ip.mu.RUnlock()
	return maps.Clone(ip.colors)
}
