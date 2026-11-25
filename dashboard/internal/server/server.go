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
