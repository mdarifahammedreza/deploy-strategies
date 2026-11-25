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

	// Kubernetes sends SIGTERM before killing a pod: stop being ready, finish in-flight requests, exit.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	drained := make(chan struct{})
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

	log.Printf("color-backend %s (%s) listening on :%s startupDelay=%s errorRate=%.2f",
		cfg.Version, cfg.Color, cfg.Port, cfg.StartupDelay, cfg.ErrorRate)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
	<-drained
	log.Printf("bye")
}
