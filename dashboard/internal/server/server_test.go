package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"deploy-strategies/dashboard/internal/hub"
)

func TestStateEndpointServesLatestSnapshot(t *testing.T) {
	h := hub.New()
	h.Publish([]byte(`{"pods":[]}`))
	rec := httptest.NewRecorder()
	New(h, t.TempDir()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/state", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != `{"pods":[]}` {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestStateEndpointBeforeFirstSnapshot(t *testing.T) {
	rec := httptest.NewRecorder()
	New(hub.New(), t.TempDir()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestWebSocketStreamsSnapshots(t *testing.T) {
	h := hub.New()
	ts := httptest.NewServer(New(h, t.TempDir()))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	h.Publish([]byte(`{"n":1}`))
	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != `{"n":1}` {
		t.Fatalf("got %q", msg)
	}
}

func TestServesStaticFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("hello dashboard"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	New(hub.New(), dir).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "hello dashboard") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}
