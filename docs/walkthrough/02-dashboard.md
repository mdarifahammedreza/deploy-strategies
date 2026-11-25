# Walkthrough 2: The Dashboard Server (Go)

The dashboard is the second program. It runs in the cluster, watches the backend pods, measures where traffic goes, and sends a **snapshot** (one JSON message) to every browser twice per second.

```
 Kubernetes API ──watch──► pods (informer cache) ─┐
 backend pods ──identity probe (1/s)──► identities ┼─► state.Build ─► hub ─► WebSocket ─► browser
 Service ──traffic probe (20/s)──► traffic window ─┘
```

| Package | Folder | Job |
|---|---|---|
| main | `dashboard/config.go`, `dashboard/main.go` | Settings and wiring |
| traffic | `internal/traffic/` | Sends test requests, keeps the last N results |
| pods | `internal/pods/` | Pod status, version from image, identity probe |
| state | `internal/state/` | Merges everything into one `Snapshot` |
| hub | `internal/hub/` | Delivers snapshots to many clients |
| server | `internal/server/` | HTTP routes and WebSocket |

New Go ideas used here (short version):
- **goroutine**: a function running at the same time as others (`go f()`).
- **channel**: a pipe between goroutines. `ch <- x` sends, `<-ch` receives.
- **`context.Context`**: a cancel signal passed down to everything. When it is cancelled, loops stop.
- **mutex** (`sync.Mutex`): a lock. Only one goroutine at a time may hold it, so shared data is not corrupted.

---

## Part 1: `config.go`

### Settings struct

```go
// dashboard/config.go lines 1-20
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
```

Same idea as the backend's `config.go`, but more settings. This struct name starts with a lowercase `c`, so it is private to the `main` package. `Target` is the URL we probe. Today it is the in-cluster Service; later in the course it can be an AWS load balancer, and we change only an environment variable.

### Reading the environment

```go
// dashboard/config.go lines 22-30
func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		Namespace:   or(getenv("NAMESPACE"), "color-demo"),
		Selector:    or(getenv("POD_SELECTOR"), "app=color-backend"),
		Target:      or(getenv("TARGET_URL"), "http://color-backend/api/color"),
		BackendPort: or(getenv("BACKEND_PORT"), "8080"),
		Listen:      or(getenv("LISTEN"), ":8081"),
		StaticDir:   or(getenv("STATIC_DIR"), "./web"),
	}
```

Every text setting has a default that works inside the kind cluster, so the Kubernetes YAML only needs to set what differs. Note `getenv` is a parameter again (easy to test).

### Numbers and durations

```go
// dashboard/config.go lines 31-42
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
```

`var err error` is declared once and reused. The pattern `if cfg.RPS, err = positiveInt(...); err != nil` assigns and checks in one line. `RPS` is requests per second for the traffic probe, `WINDOW_SIZE` is how many recent requests the traffic bar uses, and `PUSH_INTERVAL` is how often the browser gets a snapshot (default half a second).

### Helpers

```go
// dashboard/config.go lines 44-63
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
```

`or` is a fallback helper like `valueOr` in the backend. `positiveInt` accepts an empty value (use the default) and rejects zero or negative numbers. Zero would be dangerous: later we divide `time.Second` by `RPS`, and dividing by zero crashes the program.

---

## Part 2: `main.go`

### Imports

```go
// dashboard/main.go lines 1-28
// Command dashboard runs inside the cluster, watches backend pods, measures traffic,
// and streams snapshots to browsers over WebSocket.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"deploy-strategies/dashboard/internal/hub"
	"deploy-strategies/dashboard/internal/pods"
	"deploy-strategies/dashboard/internal/server"
	"deploy-strategies/dashboard/internal/state"
	"deploy-strategies/dashboard/internal/traffic"
)
```

Three groups: the Go standard library, the Kubernetes libraries (`client-go`), and our own packages under `deploy-strategies/dashboard/internal/...`. The word before a path is an **alias**: `corev1` is a short name for `k8s.io/api/core/v1`. Packages in a folder named `internal` can be imported only by code inside this module, which keeps them private.

### Start and the cancel signal

```go
// dashboard/main.go lines 30-36
func main() {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
```

Load config and create `ctx`, which is cancelled on SIGTERM or Ctrl+C. Every long-running loop receives this `ctx`, so a single signal stops the whole program in order.

### Step 1: Talk to Kubernetes

```go
// dashboard/main.go lines 38-46
	// 1. Connect to the Kubernetes API using the pod's ServiceAccount token.
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("the dashboard must run inside Kubernetes: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		log.Fatalf("kubernetes client: %v", err)
	}
```

`rest.InClusterConfig()` reads the credentials that Kubernetes mounts into every pod (a ServiceAccount token) and the API server address. That is why the dashboard must run in the cluster. The RBAC file in `k8s/base/` gives that account permission to get, list and watch pods, and nothing more. `clientset` is the object we use to call the API.

### Step 2: The informer

```go
// dashboard/main.go lines 48-55
	// 2. Informer = local cache of backend pods, kept up to date by a WATCH stream.
	factory := informers.NewSharedInformerFactoryWithOptions(clientset, 0,
		informers.WithNamespace(cfg.Namespace),
		informers.WithTweakListOptions(func(o *metav1.ListOptions) { o.LabelSelector = cfg.Selector }))
	podLister := factory.Core().V1().Pods().Lister() // must be called before Start to register the informer
	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())
	listPods := func() ([]*corev1.Pod, error) { return podLister.List(labels.Everything()) }
```

Asking the API "list all pods" 2 times a second would be wasteful. An **informer** lists once, then opens a **watch** (a long-lived stream of changes) and keeps a local cache up to date. Our code reads only from the cache, which is fast and does not load the API server. The options limit it to our namespace and to pods that match the label selector `app=color-backend`.

Three details: `podLister` must be created before `Start` so the factory knows which informer to run; `WaitForCacheSync` blocks until the first full list has arrived; and `listPods` is a tiny function that returns the cached pods. Other code receives this function instead of the informer, which keeps it simple to test.

### Step 3: Traffic prober

```go
// dashboard/main.go lines 57-59
	// 3. Traffic prober: requests through the Service → which versions really serve users.
	window := traffic.NewWindow(cfg.Window)
	go (&traffic.Prober{Target: cfg.Target, RPS: cfg.RPS, Window: window, Client: traffic.NewClient(time.Second)}).Run(ctx)
```

`NewWindow` is the ring buffer for the last N results. The long `go (...).Run(ctx)` line builds a `Prober` and starts it in the background. `NewClient(time.Second)` gives an HTTP client with a 1-second timeout (and keep-alive off; see the traffic section below).

### Steps 4 and 5: Identity prober and publisher

```go
// dashboard/main.go lines 61-67
	// 4. Identity prober: ask each pod directly → color of every box, even idle ones.
	identities := pods.NewIdentityProber(listPods, cfg.BackendPort, traffic.NewClient(time.Second), time.Second)
	go identities.Run(ctx)

	// 5. Every tick: build a snapshot and push it to all browsers.
	h := hub.New()
	go publishLoop(ctx, cfg, h, listPods, identities, window)
```

The identity prober asks each pod directly who it is. The `hub` distributes snapshots. `publishLoop` builds a snapshot every tick and hands it to the hub. All three run in their own goroutines.

### Step 6: The HTTP server and its graceful stop

```go
// dashboard/main.go lines 69-82
	// 6. HTTP server: frontend, /api/state, /ws.
	srv := &http.Server{Addr: cfg.Listen, Handler: server.New(h, cfg.StaticDir), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("dashboard on %s watching %q in %s, probing %s at %d req/s",
		cfg.Listen, cfg.Selector, cfg.Namespace, cfg.Target, cfg.RPS)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
}
```

We serve the frontend, `/api/state` and `/ws` from one server. A small goroutine waits for `ctx.Done()` and calls `Shutdown` with a 5-second limit. `ListenAndServe` returns `ErrServerClosed` after a normal shutdown, so we ignore that one error and only log real failures.

### The publish loop

```go
// dashboard/main.go lines 84-93
func publishLoop(ctx context.Context, cfg config, h *hub.Hub, listPods pods.ListFunc,
	identities *pods.IdentityProber, window *traffic.Window) {
	ticker := time.NewTicker(cfg.Tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
```

A `time.Ticker` sends a value on `ticker.C` every `cfg.Tick`. The `select` waits for whichever happens first: the program is stopping (`ctx.Done()`, so we `return`), or the tick arrives (an empty case, so we continue below). `defer ticker.Stop()` frees the ticker when the function returns.

```go
// dashboard/main.go lines 94-115
		podList, err := listPods()
		if err != nil {
			log.Printf("list pods: %v", err)
			continue
		}
		snap := state.Build(state.Input{
			Namespace:  cfg.Namespace,
			Target:     cfg.Target,
			Now:        time.Now(),
			Pods:       podList,
			Identities: identities.Identities(),
			Colors:     identities.Colors(),
			Traffic:    window.Summary(),
		})
		msg, err := json.Marshal(snap)
		if err != nil {
			log.Printf("marshal snapshot: %v", err)
			continue
		}
		h.Publish(msg)
	}
}
```

Each round: read the pods from the cache, combine all inputs in `state.Build`, turn the snapshot into JSON with `json.Marshal`, and `Publish` it. If something fails we log and `continue`; one bad round must not kill the loop. Notice that `Build` receives plain data and returns plain data: no network, no clock inside. That is why it is easy to test.

---

## Part 3: `internal/traffic/window.go`

### Types

```go
// dashboard/internal/traffic/window.go lines 1-19
// Package traffic measures which backend versions really answer requests sent through the Service.
package traffic

import (
	"math"
	"sort"
	"sync"
)

// Unreachable is the pseudo-version recorded when no pod answered at all.
const Unreachable = "unreachable"

// Result is the outcome of one request sent through the Service.
type Result struct {
	Pod     string
	Version string
	Color   string
	OK      bool // false for HTTP errors and connection failures
}
```

`Result` is the outcome of one probe request. When a request fails completely (timeout, connection refused) we record the made-up version `"unreachable"`; this is how the "recreate" downtime appears as its own segment in the traffic bar.

```go
// dashboard/internal/traffic/window.go lines 21-42
// VersionShare says how much of the recent traffic one version answered.
type VersionShare struct {
	Version string  `json:"version"`
	Color   string  `json:"color"`
	Count   int     `json:"count"`
	Errors  int     `json:"errors"`
	Percent float64 `json:"percent"`
}

// PodCount is how many of the recent requests one pod answered.
type PodCount struct {
	Hits   int
	Errors int
}

// Summary aggregates the window.
type Summary struct {
	Total     int                 `json:"total"`
	Errors    int                 `json:"errors"`
	ByVersion []VersionShare      `json:"byVersion"`
	ByPod     map[string]PodCount `json:"-"`
}
```

These structs become JSON. `ByPod` has the tag `json:"-"`, meaning "never send this one"; the pod hit counts are copied into each pod box instead. `[]VersionShare` is a slice (a growable list) and `map[string]PodCount` is a dictionary from pod name to counts.

### The ring buffer

```go
// dashboard/internal/traffic/window.go lines 44-54
// Window is a fixed-size ring buffer holding the last N results. Safe for concurrent use.
type Window struct {
	mu   sync.Mutex
	buf  []Result
	next int  // where the next result is written
	full bool // true once we have wrapped around at least once
}

func NewWindow(size int) *Window {
	return &Window{buf: make([]Result, size)}
}
```

A **ring buffer** is a fixed-size array where new items overwrite the oldest. We want "the last 100 requests", and a ring gives constant memory and constant-time add. `next` is the write position and `full` tells us if we have already wrapped around. The mutex is needed because many probe goroutines add results while the publisher reads.

### Add

```go
// dashboard/internal/traffic/window.go lines 56-65
// Add stores a result, overwriting the oldest one when the buffer is full.
func (w *Window) Add(r Result) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf[w.next] = r
	w.next = (w.next + 1) % len(w.buf)
	if w.next == 0 {
		w.full = true
	}
}
```

`Lock()` then `defer Unlock()` is the standard Go pattern: the lock is released when the function ends, even on early return. We write at `next`, then move on with `% len(w.buf)`, which jumps back to 0 at the end. When `next` returns to 0 the buffer has been filled once, so `full` becomes true.

### Summary: take a copy

```go
// dashboard/internal/traffic/window.go lines 67-77
// Summary counts the results currently in the window.
func (w *Window) Summary() Summary {
	w.mu.Lock()
	n := w.next
	if w.full {
		n = len(w.buf)
	}
	items := make([]Result, n)
	copy(items, w.buf[:n])
	w.mu.Unlock()

```

If the buffer is not full only the first `next` slots are valid. We copy those into a new slice and unlock right away, so the counting below does not block the probes.

### Summary: count

```go
// dashboard/internal/traffic/window.go lines 78-92
	s := Summary{ByVersion: []VersionShare{}, ByPod: map[string]PodCount{}}
	index := map[string]int{} // version → position in s.ByVersion
	for _, r := range items {
		s.Total++
		i, seen := index[r.Version]
		if !seen {
			i = len(s.ByVersion)
			index[r.Version] = i
			s.ByVersion = append(s.ByVersion, VersionShare{Version: r.Version})
		}
		share := &s.ByVersion[i]
		share.Count++
		if share.Color == "" {
			share.Color = r.Color
		}
```

`index` maps a version name to its position in the slice, so each version appears once, and order is not lost. `i, seen := index[r.Version]` is Go's "does the key exist?" form. `share := &s.ByVersion[i]` takes a **pointer** to the element, so `share.Count++` changes the real element and not a copy. The first non-empty color seen for a version is remembered.

```go
// dashboard/internal/traffic/window.go lines 93-103
		pod := s.ByPod[r.Pod]
		pod.Hits++
		if !r.OK {
			s.Errors++
			share.Errors++
			pod.Errors++
		}
		if r.Pod != "" {
			s.ByPod[r.Pod] = pod
		}
	}
```

Counts per pod (`Hits`) and errors. A failed request has `OK == false`. Results with an empty pod name (unreachable) are counted in totals but not stored under a pod, because no pod answered.

### Summary: percentages and order

```go
// dashboard/internal/traffic/window.go lines 104-110
	for i := range s.ByVersion {
		pct := float64(s.ByVersion[i].Count) * 100 / float64(s.Total)
		s.ByVersion[i].Percent = math.Round(pct*10) / 10 // one decimal place
	}
	sort.Slice(s.ByVersion, func(a, b int) bool { return s.ByVersion[a].Version < s.ByVersion[b].Version })
	return s
}
```

Percent = count x 100 / total, rounded to one decimal (`Round(pct*10)/10`). We sort by version name so the bar does not reorder itself between snapshots.

---

## Part 4: `internal/traffic/prober.go`

### The client: why `DisableKeepAlives`

```go
// dashboard/internal/traffic/prober.go lines 10-18
// NewClient returns an HTTP client that opens a NEW TCP connection for every request.
// kube-proxy load-balances per connection, so with keep-alive every request would
// hit the same pod and the traffic bar would lie.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
}
```

Normally an HTTP client reuses one TCP connection for many requests (**keep-alive**). But `kube-proxy`, which implements Services, chooses a pod **once per TCP connection**. With keep-alive, all 20 requests per second would travel on one connection to one pod, and the traffic bar would show 100% for a single version even in the middle of a canary. `DisableKeepAlives: true` forces a new connection for each request, so each request is balanced separately and the percentages are honest. `Timeout` stops a stuck pod from holding a probe forever.

### The probe loop

```go
// dashboard/internal/traffic/prober.go lines 20-41
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
```

`time.Second / time.Duration(p.RPS)` is the time between requests: 50 ms for 20 per second. Each probe is started with `go`, so a slow or dead pod (up to 1 s timeout) never delays the next probe. That keeps the request rate steady even during an outage.

### One probe

```go
// dashboard/internal/traffic/prober.go lines 43-53
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
```

Build a request tied to `ctx` (so it stops when the dashboard stops) and send it. Any network error becomes `Version: Unreachable`. `defer resp.Body.Close()` is required in Go to free the connection.

```go
// dashboard/internal/traffic/prober.go lines 55-64
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
```

We decode only the three fields we need into an anonymous struct. If the JSON is broken or has no version, the answer is treated as unreachable. Note that `OK` is true only for status 200; a 500 from the "bad" version still has a body with its version, so it is counted as an error **for that version**. That is how the dashboard blames v3 for its own errors.

---

## Part 5: `internal/pods/status.go`

### The five states

```go
// dashboard/internal/pods/status.go lines 1-17
// Package pods turns Kubernetes Pod objects into what the dashboard draws.
package pods

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// The five box states shown on the dashboard.
const (
	StatusPending     = "Pending"     // accepted, waiting for a node / image / container start
	StatusStarting    = "Starting"    // container running but readiness probe not passing yet
	StatusReady       = "Ready"       // receives traffic from Services
	StatusTerminating = "Terminating" // being shut down (deletionTimestamp is set)
	StatusFailed      = "Failed"      // container exited with an error
)
```

Constants for the five box states. The frontend uses these same words as CSS classes.

### Deriving the state

```go
// dashboard/internal/pods/status.go lines 19-33
// Status maps a Pod to one of the five box states.
func Status(p *corev1.Pod) string {
	switch {
	case p.DeletionTimestamp != nil:
		return StatusTerminating
	case p.Status.Phase == corev1.PodFailed:
		return StatusFailed
	case isReady(p):
		return StatusReady
	case p.Status.Phase == corev1.PodRunning:
		return StatusStarting
	default:
		return StatusPending
	}
}
```

A `switch` with no value checks each `case` condition from top to bottom; the first true one wins, so the order matters. Terminating is checked first because a pod being deleted can still look "Running" and "Ready". Then Failed, then Ready. Running but not ready means the container is started but the readiness probe has not passed: **Starting**. Everything else is **Pending**.

```go
// dashboard/internal/pods/status.go lines 35-42
func isReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
```

`isReady` looks through the pod conditions for type `Ready`. Kubernetes sets this condition from the readiness probe. If the condition is missing, we say not ready.

### Version from the image tag

```go
// dashboard/internal/pods/status.go lines 44-60
// VersionFromImage returns the tag of the pod's first container image:
// "color-backend:v2" → "v2". Known even while the pod is still Pending.
func VersionFromImage(p *corev1.Pod) string {
	if len(p.Spec.Containers) == 0 {
		return "unknown"
	}
	image := p.Spec.Containers[0].Image
	if at := strings.Index(image, "@"); at >= 0 { // drop "@sha256:..." digests
		image = image[:at]
	}
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon > slash { // a colon after the last slash is a tag, not a registry port
		return image[colon+1:]
	}
	return "latest"
}
```

For image `color-backend:v2` we want `v2`. This works even while the pod is Pending, before the container can answer, so the box can be placed in the right version panel right away. We cut off a digest (`@sha256:...`) first. Then we compare the last `:` with the last `/`: a colon that comes after the last slash is a tag. A colon before the last slash belongs to a registry port like `localhost:5000/app`, so no tag means `latest`.

---

## Part 6: `internal/pods/identity.go`

### Types

```go
// dashboard/internal/pods/identity.go lines 1-22
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
```

`ListFunc` is a **function type**: any function that returns pods and an error. `main.go` passes `listPods`; tests pass a fake. `Identity` is what a pod says about itself.

### The struct

```go
// dashboard/internal/pods/identity.go lines 24-35
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
```

Why probe each pod directly, when the traffic probe already tells us versions? Because of pods that get no traffic: the idle side of blue/green, or a pod that is Starting. Their boxes should still be colored. We use `sync.RWMutex`, a lock that allows many readers at once but only one writer: good because the publisher reads much more often than we write. `colors` remembers version to color forever, so a version keeps its color even after all its pods have gone.

### Constructor and run loop

```go
// dashboard/internal/pods/identity.go lines 37-56
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
```

`NewIdentityProber` makes the empty maps (a nil map crashes on write). `Run` probes once immediately and then every interval. The probe happens first in the loop on purpose, so the first colors appear without a one-second wait.

### ProbeAll: ask every pod in parallel

```go
// dashboard/internal/pods/identity.go lines 58-82
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
```

For every pod that already has an IP, we start a goroutine. `sync.WaitGroup` counts running goroutines: `Add(1)` before starting, `Done()` when finished, and `Wait()` blocks until the count is zero. Results are written into the shared `fresh` map under a small mutex. Notice `go func(name, addr string) {...}(p.Name, p.Status.PodIP)`: values are passed as arguments, so each goroutine gets its own copy and not the loop variable that changes.

### ProbeAll: merge results

```go
// dashboard/internal/pods/identity.go lines 84-96
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
```

Now we lock the main state and build a new map. Fresh answer: use it and remember its color. No fresh answer (a timeout): keep the old answer, so boxes do not flicker to gray when one probe fails. Because `next` is built only from current pods, pods that disappeared are forgotten automatically.

### The single probe

```go
// dashboard/internal/pods/identity.go lines 98-118
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
```

`net.JoinHostPort` builds `ip:port` (and handles IPv6 correctly). We call the pod IP directly, bypassing the Service. The comment explains an important choice: we do not check the status code, because a failing v3 pod answers 500 but its body still names its version and color.

### Safe copies

```go
// dashboard/internal/pods/identity.go lines 120-132
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
```

These getters return **copies** (`maps.Clone`) under a read lock. If we returned the internal map, other goroutines could read it while the prober writes to it, which crashes Go programs ("concurrent map read and map write").

---

## Part 7: `internal/state/snapshot.go`

### Types

```go
// dashboard/internal/state/snapshot.go lines 1-29
// Package state combines pods, identities and traffic into the JSON the browser draws.
package state

import (
	"maps"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"

	"deploy-strategies/dashboard/internal/pods"
	"deploy-strategies/dashboard/internal/traffic"
)

// PodView is one box on the dashboard.
type PodView struct {
	Name       string            `json:"name"`
	Node       string            `json:"node"`
	IP         string            `json:"ip"`
	Version    string            `json:"version"`
	Color      string            `json:"color"`
	Status     string            `json:"status"`
	Labels     map[string]string `json:"labels"`
	Hits       int               `json:"hits"`
	Errors     int               `json:"errors"`
	AgeSeconds int               `json:"ageSeconds"`

	created time.Time // for sorting only, not sent to the browser
}
```

`PodView` is one box. The field `created` starts with a lowercase letter, so it is private and the JSON encoder ignores it: we only need it for sorting. This is also the mapping of the JSON contract described in the design spec.

```go
// dashboard/internal/state/snapshot.go lines 31-49
// Snapshot is the full message pushed to the browser every tick.
type Snapshot struct {
	Namespace string          `json:"namespace"`
	Target    string          `json:"target"`
	Time      time.Time       `json:"time"`
	Pods      []PodView       `json:"pods"`
	Traffic   traffic.Summary `json:"traffic"`
}

// Input is everything Build needs. Keeping it a plain struct makes Build a pure, testable function.
type Input struct {
	Namespace  string
	Target     string
	Now        time.Time
	Pods       []*corev1.Pod
	Identities map[string]pods.Identity
	Colors     map[string]string
	Traffic    traffic.Summary
}
```

`Snapshot` is the whole message. `Input` bundles all arguments of `Build` into one struct, so `Build` is a **pure function**: same input gives same output, no hidden clock or network. That makes it simple to test with a handmade `Input`.

### Colors per version

```go
// dashboard/internal/state/snapshot.go lines 51-62
// Build merges the inputs into one Snapshot.
func Build(in Input) Snapshot {
	// Colors per version: learned from direct pod probes first, then from traffic answers.
	colors := maps.Clone(in.Colors)
	if colors == nil {
		colors = map[string]string{}
	}
	for _, share := range in.Traffic.ByVersion {
		if colors[share.Version] == "" && share.Color != "" {
			colors[share.Version] = share.Color
		}
	}
```

We need a color for every box. First choice: colors learned from direct pod probes. Second choice: colors seen in traffic answers. `maps.Clone` is used so we never change the caller's map. Cloning a nil map gives nil, so we replace nil with an empty map, because writing to a nil map crashes.

### Building each pod view

```go
// dashboard/internal/state/snapshot.go lines 64-77
	views := make([]PodView, 0, len(in.Pods))
	for _, p := range in.Pods {
		version := pods.VersionFromImage(p) // known even while Pending
		color := colors[version]
		if id, ok := in.Identities[p.Name]; ok { // the pod's own answer wins
			version, color = id.Version, id.Color
		}

		labels := maps.Clone(p.Labels)
		if labels == nil {
			labels = map[string]string{}
		}
		delete(labels, "pod-template-hash") // noise for students

```

The version starts as the image tag (available when Pending). If the pod has answered the identity probe, its own answer replaces both version and color: the pod is the truth. We copy the labels and remove `pod-template-hash`, an automatic label that only adds noise for students.

```go
// dashboard/internal/state/snapshot.go lines 78-92
		count := in.Traffic.ByPod[p.Name]
		views = append(views, PodView{
			Name:       p.Name,
			Node:       p.Spec.NodeName,
			IP:         p.Status.PodIP,
			Version:    version,
			Color:      color,
			Status:     pods.Status(p),
			Labels:     labels,
			Hits:       count.Hits,
			Errors:     count.Errors,
			AgeSeconds: int(in.Now.Sub(p.CreationTimestamp.Time).Seconds()),
			created:    p.CreationTimestamp.Time,
		})
	}
```

We fill the struct, calling `pods.Status(p)` for the state, taking hits and errors from the traffic summary, and computing age from the creation time.

### Stable order

```go
// dashboard/internal/state/snapshot.go lines 93-103
	// Stable order so boxes don't jump around: version, then age (oldest first), then name.
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i], views[j]
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		if !a.created.Equal(b.created) {
			return a.created.Before(b.created)
		}
		return a.Name < b.Name
	})
```

Boxes should not jump around between snapshots. We sort by version, then oldest first, then name. `sort.SliceStable` takes a "less" function that says whether item `i` comes before item `j`. Always sorting by name last guarantees a total order.

### Never null

```go
// dashboard/internal/state/snapshot.go lines 105-110
	summary := in.Traffic
	if summary.ByVersion == nil {
		summary.ByVersion = []traffic.VersionShare{}
	}
	return Snapshot{Namespace: in.Namespace, Target: in.Target, Time: in.Now.UTC(), Pods: views, Traffic: summary}
}
```

An empty Go slice that is `nil` becomes `null` in JSON, and the frontend would crash on `null.map(...)`. So we make sure it is an empty list `[]`. Rule from the spec: arrays and maps are never null.

---

## Part 8: `internal/hub/hub.go`

### Why a hub

Many browsers watch at once. The publisher must not wait for a slow browser (a phone on bad Wi-Fi). The hub solves this with a **mailbox of size one** per browser.

### Struct

```go
// dashboard/internal/hub/hub.go lines 1-16
// Package hub fans one stream of snapshots out to many WebSocket clients.
package hub

import "sync"

// Hub keeps the latest message and a 1-slot mailbox per subscriber.
// A slow browser never blocks the others: it simply skips to the newest snapshot.
type Hub struct {
	mu     sync.Mutex
	subs   map[chan []byte]struct{}
	latest []byte
}

func New() *Hub {
	return &Hub{subs: map[chan []byte]struct{}{}}
}
```

`subs` is a set of channels, one per connected client. (`map[...]struct{}` is Go's idiom for a set.) `latest` is the newest message, kept so that new clients and `/api/state` get data immediately.

### Publish: the 1-slot mailbox

```go
// dashboard/internal/hub/hub.go lines 18-37
// Publish stores msg as the latest and delivers it to every subscriber, replacing any unread message.
func (h *Hub) Publish(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.latest = msg
	for ch := range h.subs {
		select {
		case ch <- msg:
		default: // mailbox full: drop the stale message, put the new one
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- msg:
			default:
			}
		}
	}
}
```

For each subscriber we try to send. A `select` with a `default` case never blocks. If the channel (capacity 1) has space, the message goes in. If it is full, the browser has not read the previous snapshot yet. Old snapshots are worthless (we only care about "now"), so we drain the old message (inner `select` with `default`) and put the new one. A slow client therefore skips straight to the newest state and can never slow down the others or make memory grow. The lock is held during publish so subscribe and cancel cannot change the map at the same time.

### Subscribe

```go
// dashboard/internal/hub/hub.go lines 39-54
// Subscribe returns a channel of messages and a cancel function. The latest message (if any) arrives immediately.
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan []byte, 1)
	if h.latest != nil {
		ch <- h.latest
	}
	h.subs[ch] = struct{}{}
	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs, ch)
	}
	return ch, cancel
}
```

`make(chan []byte, 1)` creates the 1-slot channel. If a latest message exists, it is put in at once, so a new browser sees a picture without waiting up to 500 ms. We return a receive-only channel (`<-chan`) and a `cancel` function, a **closure** that removes this subscriber.

### Small getters

```go
// dashboard/internal/hub/hub.go lines 56-68
// Latest returns the most recently published message (nil before the first one).
func (h *Hub) Latest() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.latest
}

// Subscribers returns how many clients are connected.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
```

`Latest` and `Subscribers` just read under the lock. `Subscribers` is useful in tests to wait until a client has really connected.

---

## Part 9: `internal/server/server.go`

### Routes

```go
// dashboard/internal/server/server.go lines 1-33
// Package server exposes the dashboard over HTTP: static frontend, JSON state, and a WebSocket stream.
package server

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"deploy-strategies/dashboard/internal/hub"
)

// New builds the HTTP routes.
func New(h *hub.Hub, staticDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, _ *http.Request) {
		latest := h.Latest()
		if latest == nil {
			http.Error(w, "no snapshot yet", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(latest)
	})
	mux.HandleFunc("GET /ws", streamSnapshots(h))
	mux.Handle("GET /", http.FileServer(http.Dir(staticDir)))
	return mux
}
```

`/healthz` is for the dashboard's own Kubernetes probes. `/api/state` returns the latest snapshot as JSON; before the first one we answer 503 instead of an empty body. The e2e script reads this endpoint. The last route serves the built frontend files from `staticDir`. The route `GET /` here is a catch-all on purpose (unlike the backend's `{$}`) so that `/assets/...` files are found; more specific patterns such as `/ws` win over it.

### The WebSocket stream

```go
// dashboard/internal/server/server.go lines 35-48
// streamSnapshots upgrades the request to a WebSocket and forwards every hub message to the browser.
func streamSnapshots(h *hub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// InsecureSkipVerify allows the Vite dev server (other origin) to connect during development.
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()

		ctx := conn.CloseRead(r.Context()) // we never read; ctx is cancelled when the browser disconnects
		messages, unsubscribe := h.Subscribe()
		defer unsubscribe()

```

A WebSocket is a long-lived two-way connection that starts as a normal HTTP request. `websocket.Accept` upgrades it. `InsecureSkipVerify` turns off the origin check so the Vite dev server (on another port) can connect during development. The browser never sends anything, so `CloseRead` starts a reader that handles control messages and cancels `ctx` when the browser disconnects. `defer unsubscribe()` makes sure we always leave the hub.

```go
// dashboard/internal/server/server.go lines 49-63
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-messages:
				writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(writeCtx, websocket.MessageText, msg)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}
}
```

The loop waits for either a disconnect or a new snapshot from the mailbox. Each write has a 5-second timeout, so a dead connection cannot block this goroutine forever. Any write error ends the handler, and the deferred calls clean up.

---

## How one test is structured

Tests live next to the code in `_test.go` files and run with `go test ./...`. Here is the first half of a test for the ring buffer.

```go
// dashboard/internal/traffic/window_test.go lines 1-21
package traffic

import "testing"

func TestSummaryCountsVersionsAndErrors(t *testing.T) {
	w := NewWindow(10)
	for i := 0; i < 6; i++ {
		w.Add(Result{Pod: "a", Version: "v1", Color: "royalblue", OK: true})
	}
	for i := 0; i < 3; i++ {
		w.Add(Result{Pod: "b", Version: "v2", Color: "mediumseagreen", OK: true})
	}
	w.Add(Result{Pod: "b", Version: "v2", Color: "mediumseagreen", OK: false})

	s := w.Summary()
	if s.Total != 10 || s.Errors != 1 {
		t.Fatalf("total=%d errors=%d, want 10 and 1", s.Total, s.Errors)
	}
	if len(s.ByVersion) != 2 {
		t.Fatalf("want 2 versions, got %+v", s.ByVersion)
	}
```

Pattern: **Arrange, Act, Assert**. Arrange: make a window of size 10 and add 10 results (6 for v1, 3 good and 1 bad for v2). Act: call `Summary()`. Assert: compare to what we expect; `t.Fatalf` fails the test with a clear message. A test is a function whose name starts with `Test` and takes `*testing.T`. No mocks are needed because the window is plain data.

---

## Try it yourself

Run commands in the `dashboard/` folder. The tests need only Go (no cluster).

1. **Run the tests.** `go test ./...`. Expected: every package prints `ok`.
2. **Overflow the ring.** In a scratch test (or copy of `window_test.go`) make `NewWindow(3)`, add four results with versions `v1, v1, v1, v2`, and call `Summary()`. Expected: `Total` is 3 and the first `v1` is gone: v1 has count 2 (66.7%) and v2 has count 1 (33.3%).
3. **Change the window size in a real cluster.** After `make up`, set `WINDOW_SIZE=20` on the dashboard Deployment (`kubectl -n color-demo set env deploy/color-dashboard WINDOW_SIZE=20`). Expected: the traffic bar reacts faster and jumps in steps of 5% (1 request out of 20).
4. **Check the contract.** `curl -s localhost:30080/api/state | head -c 400`. Expected: JSON starting with `"namespace":"color-demo"` and a `pods` array; `byVersion` is a list, never `null`.
5. **See keep-alive lie.** In `prober.go` change `DisableKeepAlives: true` to `false`, rebuild, and run a canary. Expected: the bar jumps between 0% and 100% instead of showing a steady split. Change it back afterwards.
