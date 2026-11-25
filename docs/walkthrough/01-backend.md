# Walkthrough 1: The Backend (Go)

The backend is the small program that every box on the dashboard represents. One running copy (one **pod**) of this program = one colored box. It has only three source files, and it uses only Go's standard library (no extra packages).

| File | Job |
|---|---|
| `backend/config.go` | Reads settings from environment variables |
| `backend/app.go` | The HTTP handlers (what happens when someone calls a URL) |
| `backend/main.go` | Starts the server, controls readiness, handles shutdown |

Quick Go notes for beginners:
- A Go program is split into **packages**. All three files say `package main`, so they are one program.
- `func name(args) returnType` defines a function. Go functions can return **two values**; the second is very often an `error`.
- `:=` creates a new variable and guesses its type. `_` means "I do not need this value".
- `nil` means "nothing". Code like `if err != nil` is how Go checks for errors.

---

## Part 1: `config.go`

### Package and imports

```go
// backend/config.go lines 1-7
package main

import (
	"fmt"
	"strconv"
	"time"
)
```

Every Go file starts with its package name. `import` lists the standard-library packages this file uses: `fmt` for building error messages, `strconv` for turning text into numbers, `time` for durations like `5s`. Go will not compile if you import something you do not use.

### The Config struct

```go
// backend/config.go lines 9-17
// Config holds every setting the color API reads from environment variables.
type Config struct {
	Version      string        // label shown on the dashboard, e.g. "v2"
	Color        string        // any CSS color, e.g. "mediumseagreen"
	Port         string        // TCP port to listen on
	StartupDelay time.Duration // /readyz answers 503 for this long after start
	ErrorRate    float64       // share (0..1) of /api/color calls that return HTTP 500
	NeverReady   bool          // if true /readyz never succeeds (simulates a broken release)
}
```

A `struct` is a group of named fields, like a simple object. This struct holds all six settings of the app. The comment on the right of each field tells you what it means. `time.Duration` is Go's type for a length of time, for example `5 * time.Second`.

Why environment variables? One Docker image per version is built from the same code, and Kubernetes can change env vars in YAML without rebuilding. The settings later become `ENV` lines in the Dockerfile (see walkthrough 4).

### LoadConfig: defaults

```go
// backend/config.go lines 19-25
// LoadConfig builds a Config from a getenv function (os.Getenv in production).
func LoadConfig(getenv func(string) string) (Config, error) {
	cfg := Config{
		Version: valueOr(getenv("VERSION"), "v1"),
		Color:   valueOr(getenv("COLOR"), "royalblue"),
		Port:    valueOr(getenv("PORT"), "8080"),
	}
```

`LoadConfig` does not call `os.Getenv` itself. It receives a function `getenv` as a parameter. In production we pass `os.Getenv`; in tests we pass a fake function that returns values from a map. This is called **dependency injection**, and it makes the function easy to test without touching the real environment. The function returns `(Config, error)`: either a good config, or an error.

The three text settings get a default if the variable is empty (`v1`, `royalblue`, `8080`).

### LoadConfig: parsing the delay

```go
// backend/config.go lines 27-31
	delay, err := time.ParseDuration(valueOr(getenv("STARTUP_DELAY"), "0s"))
	if err != nil {
		return Config{}, fmt.Errorf("STARTUP_DELAY: %w", err)
	}
	cfg.StartupDelay = delay
```

`time.ParseDuration` understands strings like `5s`, `1m30s` or `250ms`. If the text is invalid, we return an empty `Config{}` and a wrapped error. `%w` keeps the original error inside the new one, so tools can still inspect it. The message starts with `STARTUP_DELAY:` so the person who reads the log knows which variable is wrong.

### LoadConfig: parsing the error rate

```go
// backend/config.go lines 33-37
	rate, err := strconv.ParseFloat(valueOr(getenv("ERROR_RATE"), "0"), 64)
	if err != nil || rate < 0 || rate > 1 {
		return Config{}, fmt.Errorf("ERROR_RATE must be a number between 0 and 1, got %q", getenv("ERROR_RATE"))
	}
	cfg.ErrorRate = rate
```

`ParseFloat` converts text to a number. We also check the range: an error rate is a share between 0 and 1 (0.3 means 30%). If anything is wrong, the program refuses to start. Failing early with a clear message is better than running with a strange value. `%q` prints the bad value inside quotes, so an empty string is visible.

### LoadConfig: parsing NEVER_READY

```go
// backend/config.go lines 39-46
	never, err := strconv.ParseBool(valueOr(getenv("NEVER_READY"), "false"))
	if err != nil {
		return Config{}, fmt.Errorf("NEVER_READY: %w", err)
	}
	cfg.NeverReady = never

	return cfg, nil
}
```

`ParseBool` accepts `true`, `false`, `1`, `0` and a few more. After all three parsings pass, we return the finished config and `nil` (no error).

### The helper

```go
// backend/config.go lines 48-54
// valueOr returns v, or fallback when v is empty.
func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
```

`valueOr` returns a fallback when the text is empty. The name starts with a lowercase letter, so it is private to this package. In Go, a capital first letter (`LoadConfig`) means "exported" (public).

---

## Part 2: `app.go`

### Imports

```go
// backend/app.go lines 1-10
package main

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sync/atomic"
	"time"
)
```

New here: `encoding/json` (write JSON), `html` (escape text so it is safe inside HTML), `net/http` (the web server), `sync/atomic` (safe shared variables, explained below).

### The JSON response type

```go
// backend/app.go lines 12-19
// ColorResponse is the JSON body of GET /api/color.
type ColorResponse struct {
	Version  string    `json:"version"`
	Color    string    `json:"color"`
	Pod      string    `json:"pod"`
	ServedAt time.Time `json:"servedAt"`
	Error    string    `json:"error,omitempty"`
}
```

The text in backticks after each field is a **struct tag**. It tells the JSON encoder which key name to use: `Version` in Go becomes `"version"` in JSON. `omitempty` on `Error` means: if the string is empty, leave the key out. So a good answer has no `error` key and a failed answer has one. This is the contract the dashboard reads.

### The App struct and why `atomic.Bool`

```go
// backend/app.go lines 21-27
// App holds the HTTP handlers and the readiness flag.
type App struct {
	cfg     Config
	podName string         // in Kubernetes the hostname IS the pod name
	random  func() float64 // returns [0,1); injected so tests are deterministic
	ready   atomic.Bool    // flipped by main after STARTUP_DELAY, cleared on SIGTERM
}
```

`App` holds the handlers' shared state. Three details matter:
- `podName`: in Kubernetes the container hostname is the pod name, so we can show *which* pod answered.
- `random` is a **function stored in a field**. In production it is a real random generator; in tests it returns a fixed number, so we can test "30% errors" without luck.
- `ready` is an `atomic.Bool`. Go runs each HTTP request in its own goroutine (a lightweight thread), and `main.go` changes the flag from yet another goroutine. Two goroutines touching a normal `bool` at the same time is a **data race**. `atomic.Bool` makes reads and writes safe without a lock.

### Constructor and SetReady

```go
// backend/app.go lines 29-34
func NewApp(cfg Config, podName string, random func() float64) *App {
	return &App{cfg: cfg, podName: podName, random: random}
}

// SetReady controls what /readyz answers.
func (a *App) SetReady(ready bool) { a.ready.Store(ready) }
```

Go has no constructors; by convention a function named `NewSomething` builds the value. `&App{...}` creates the struct and returns a pointer to it (so everyone shares the same one). `SetReady` is the only way `main` changes readiness. `(a *App)` before the name means "this is a method on App".

### Routes

```go
// backend/app.go lines 36-44
// Routes wires URL paths to handlers (Go 1.22+ "METHOD /path" patterns).
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/color", a.handleColor)
	mux.HandleFunc("GET /healthz", a.handleHealthz)
	mux.HandleFunc("GET /readyz", a.handleReadyz)
	mux.HandleFunc("GET /{$}", a.handleIndex) // {$} = exactly "/", not a catch-all
	return mux
}
```

`http.NewServeMux()` is the router. Since Go 1.22 a pattern can include the HTTP method: `"GET /api/color"` matches only GET. Requests with other methods get an automatic 405 answer.

Why `GET /{$}`? A plain pattern `/` is a **catch-all**: it matches every path that nothing else matched, so `/anything` would paint the colored page. `{$}` means "end of path here", so only exactly `/` matches, and unknown paths correctly give 404.

### handleColor

```go
// backend/app.go lines 46-58
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
```

This is the main endpoint. It builds the answer, then asks `a.random()` for a number in [0,1). If that number is lower than `ErrorRate`, we mark the response as an error and use status 500. With `ERROR_RATE=0.3`, about 3 requests in 10 fail; this is how version v3 plays the "bad canary".

The headers: `no-store` stops caches from reusing an old answer (we want each request to reach a real pod), and `Access-Control-Allow-Origin: *` lets a browser page on another origin read the answer. `WriteHeader(status)` must come after the headers and before the body. `_ =` ignores the error from writing; if the client has gone away there is nothing useful to do.

### handleHealthz (liveness)

```go
// backend/app.go lines 60-63
// handleHealthz is the liveness probe: "the process is alive".
func (a *App) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("ok\n"))
}
```

Always answers `200 ok`. It only proves "the process is alive and the web server works".

### handleReadyz (readiness)

```go
// backend/app.go lines 65-72
// handleReadyz is the readiness probe: "send me traffic".
func (a *App) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	if !a.ready.Load() {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ready\n"))
}
```

Answers 503 until `SetReady(true)` is called, then 200. `http.Error` writes the status and a text message. The `return` after it is important: without it the code would continue and write "ready" too.

### handleIndex and the HTML template

```go
// backend/app.go lines 74-80
// handleIndex paints the whole page in this version's color (handy for curl/browser demos).
func (a *App) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, indexHTML,
		html.EscapeString(a.cfg.Color), html.EscapeString(a.cfg.Version), html.EscapeString(a.podName))
}
```

This serves a whole web page colored with this version's color, useful when you `curl` or open the pod in a browser. `fmt.Fprintf` fills the `%s` placeholders in order. `html.EscapeString` protects against HTML injection: if someone sets `COLOR` to something with `<script>`, it will show as text, not run.

```go
// backend/app.go lines 82-86
const indexHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>color-backend</title></head>
<body style="margin:0;height:100vh;display:grid;place-items:center;background:%s;font-family:sans-serif;color:#fff;text-shadow:0 1px 3px #0008">
<h1>%s · served by %s</h1></body></html>
`
```

The page is a Go **raw string** (between backticks, so quotes and newlines need no escaping). The three `%s` become the color, the version and the pod name. `100vh` and `place-items:center` make one centered title on a full-screen background.

---

## Part 3: `main.go`

### Package doc and imports

```go
// backend/main.go lines 1-15
// Command color-backend is a tiny HTTP API that answers with its version and color.
// Each running copy (pod) becomes one box on the classroom dashboard.
package main

import (
	"context"
	"errors"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)
```

The comment directly above `package main` is the package documentation. Notice `math/rand/v2`: it gives us `rand.Float64`, which has exactly the type `func() float64` that `NewApp` expects. `os/signal` and `syscall` are for handling shutdown signals.

### Loading config and building the server

```go
// backend/main.go lines 17-29
func main() {
	cfg, err := LoadConfig(os.Getenv)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	podName, _ := os.Hostname() // inside Kubernetes the hostname is the pod name

	app := NewApp(cfg, podName, rand.Float64)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
```

`log.Fatalf` prints a message and exits with an error code, so a bad config makes the pod crash loudly and visibly. `os.Hostname()` gives the pod name. `ReadHeaderTimeout` limits how long a client may take to send its request headers; it protects against slow-client attacks and is a normal safe default.

### Becoming ready after a delay

```go
// backend/main.go lines 31-40
	// Become ready only after STARTUP_DELAY, so students can watch the "Starting" phase.
	if cfg.NeverReady {
		log.Printf("NEVER_READY=true: /readyz will always answer 503")
	} else {
		go func() {
			time.Sleep(cfg.StartupDelay)
			app.SetReady(true)
			log.Printf("ready (after %s)", cfg.StartupDelay)
		}()
	}
```

`go func() { ... }()` starts a **goroutine**: a function that runs at the same time as the rest of `main`. It sleeps for `STARTUP_DELAY`, then flips the ready flag. Meanwhile the server is already running, so `/readyz` answers 503 during that time. That gap is the **Starting** state students see on the dashboard. With `NEVER_READY=true` we never start this goroutine, which simulates a broken release (v4).

### Listening for SIGTERM

```go
// backend/main.go lines 42-45
	// Kubernetes sends SIGTERM before killing a pod: stop being ready, finish in-flight requests, exit.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	drained := make(chan struct{})
```

When Kubernetes wants to stop a pod, it sends the signal `SIGTERM` (a polite "please stop"). `signal.NotifyContext` gives us a `ctx` that becomes "done" when that signal (or Ctrl+C, `os.Interrupt`) arrives. `drained` is a channel used only as a "finished" notification: closing it wakes up whoever waits on it. `struct{}` carries no data and uses no memory.

### The shutdown goroutine

```go
// backend/main.go lines 46-56
	go func() {
		<-ctx.Done()
		log.Printf("shutdown signal received, draining connections")
		app.SetReady(false)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		close(drained)
	}()
```

`<-ctx.Done()` blocks until the signal arrives. Then we set not-ready first, so the pod is removed from Service endpoints, and call `srv.Shutdown`, which stops accepting new connections but lets requests that are already running finish (up to 10 seconds). At the end we `close(drained)` to tell `main` that we are finished. `defer cancel()` runs when the function ends and releases the timer.

### Running the server

```go
// backend/main.go lines 58-65
	log.Printf("color-backend %s (%s) listening on :%s startupDelay=%s errorRate=%.2f",
		cfg.Version, cfg.Color, cfg.Port, cfg.StartupDelay, cfg.ErrorRate)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
	<-drained
	log.Printf("bye")
}
```

`ListenAndServe` blocks while the server runs. When `Shutdown` is called, it returns `http.ErrServerClosed`, which is the **normal** case, so we only treat other errors as failures. Then `<-drained` waits until the shutdown goroutine really finished. If `main` returned immediately, the process would exit and cut off the in-flight requests.

---

## The HTTP contract

| Method and path | Success | Failure | Used by |
|---|---|---|---|
| `GET /api/color` | `200` JSON `{"version","color","pod","servedAt"}` | `500` same JSON plus `"error":"simulated failure"` | Dashboard traffic probe and identity probe |
| `GET /healthz` | `200 ok` | never fails (if the process is dead, there is no answer) | Kubernetes **liveness** probe |
| `GET /readyz` | `200 ready` | `503 not ready` before `STARTUP_DELAY`, after SIGTERM, or always with `NEVER_READY` | Kubernetes **readiness** probe |
| `GET /` | `200` HTML page in the version color | none | Humans, `curl` |

Example answer from a healthy v2 pod:

```json
{"version":"v2","color":"mediumseagreen","pod":"color-backend-6f7c9-x2k","servedAt":"2026-11-25T12:00:00Z"}
```

## Readiness vs liveness

These two sound similar but answer different questions.

| | Liveness (`/healthz`) | Readiness (`/readyz`) |
|---|---|---|
| Question | "Is the process alive?" | "Can it serve traffic right now?" |
| If it fails | Kubernetes **restarts** the container | Kubernetes **removes the pod from the Service** (no traffic); no restart |
| In this project | always 200 | 503 during startup, after SIGTERM, or forever (v4) |

Why it matters for deployment strategies: a rolling update only continues when new pods become *Ready*. With v4 (`NEVER_READY`) new pods never pass readiness, so the rollout gets stuck, and `maxUnavailable: 0` keeps the old pods serving. With v3 (30% errors) the pods **are** ready because `/readyz` only checks "started", not "correct". That is the lesson "readiness is not correctness": a bad version can pass readiness and still hurt users, which is why canary releases exist.

Note that liveness is always 200 on purpose. If it failed for v4, Kubernetes would restart the pod again and again and hide the lesson.

---

## Try it yourself

You need Go installed. Run all commands inside `backend/`.

1. **Run it and look at the color.** `go run . ` then in another terminal `curl -s localhost:8080/api/color`. Expected: a JSON line with `"version":"v1"`, `"color":"royalblue"` and your computer name as `pod`.
2. **Watch readiness change.** Run `STARTUP_DELAY=10s go run .`, then quickly `curl -i localhost:8080/readyz`. Expected: `503 Service Unavailable` with `not ready`; after 10 seconds the same command gives `200` and `ready`.
3. **Make errors.** Run `ERROR_RATE=0.5 go run .` and call `for i in $(seq 20); do curl -s -o /dev/null -w "%{http_code}\n" localhost:8080/api/color; done`. Expected: a mix of `200` and `500`, roughly half each.
4. **Test the `{$}` pattern.** Run `curl -i localhost:8080/nothing`. Expected: `404`. Now change `GET /{$}` to `GET /` in `app.go`, restart, and repeat: you get the colored page instead. Change it back afterwards.
5. **Try graceful shutdown.** Start the server, press Ctrl+C. Expected: the log shows `shutdown signal received, draining connections` and then `bye`.
