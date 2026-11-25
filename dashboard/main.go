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

func main() {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	// 1. Connect to the Kubernetes API using the pod's ServiceAccount token.
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("the dashboard must run inside Kubernetes: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		log.Fatalf("kubernetes client: %v", err)
	}

	// 2. Informer = local cache of backend pods, kept up to date by a WATCH stream.
	factory := informers.NewSharedInformerFactoryWithOptions(clientset, 0,
		informers.WithNamespace(cfg.Namespace),
		informers.WithTweakListOptions(func(o *metav1.ListOptions) { o.LabelSelector = cfg.Selector }))
	podLister := factory.Core().V1().Pods().Lister() // must be called before Start to register the informer
	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())
	listPods := func() ([]*corev1.Pod, error) { return podLister.List(labels.Everything()) }

	// 3. Traffic prober: requests through the Service → which versions really serve users.
	window := traffic.NewWindow(cfg.Window)
	go (&traffic.Prober{Target: cfg.Target, RPS: cfg.RPS, Window: window, Client: traffic.NewClient(time.Second)}).Run(ctx)

	// 4. Identity prober: ask each pod directly → color of every box, even idle ones.
	identities := pods.NewIdentityProber(listPods, cfg.BackendPort, traffic.NewClient(time.Second), time.Second)
	go identities.Run(ctx)

	// 5. Every tick: build a snapshot and push it to all browsers.
	h := hub.New()
	go publishLoop(ctx, cfg, h, listPods, identities, window)

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
