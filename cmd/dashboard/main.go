package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"monitor/internal/cluster"
	"monitor/internal/config"
	"monitor/internal/dashboard"
	"monitor/internal/projects"
	"monitor/internal/registry"
	"monitor/internal/webui"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	clients, err := cluster.NewClients()
	if err != nil {
		log.Fatal(err)
	}

	projectSource := projects.NewSource(cfg, nil)
	res := cfg.Resources
	// Everything is watched cluster-wide with one informer per resource; only
	// pods are watched per namespace, where CronWorkflows (last-run pods) and
	// InferenceServices (serving pods) run.
	clusterWatcher := cluster.NewWatcher("cluster", clients,
		[]schema.GroupVersionResource{res.CronWorkflows, res.Workflows, res.InferenceServices, res.ServingRuntimes, res.LLMInferenceServices, cluster.Namespaces},
		cluster.AllNamespaces)
	podWatcher := cluster.NewWatcher("pods", clients,
		[]schema.GroupVersionResource{cluster.Pods},
		func() []string {
			var namespaces []string
			objects := clusterWatcher.Snapshot().Objects
			for _, gvr := range []schema.GroupVersionResource{res.CronWorkflows, res.InferenceServices, res.LLMInferenceServices} {
				for _, object := range objects[gvr] {
					namespaces = append(namespaces, object.GetNamespace())
				}
			}
			return namespaces
		})
	imageChecker := registry.NewChecker(cfg.NexusManifestURLTemplate, cfg.ImageCacheTTL, cfg.RegistryCheckConcurrency, cfg.UpstreamTimeout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go projectSource.Run(ctx)
	go clusterWatcher.Run(ctx)
	go podWatcher.Run(ctx)

	api := dashboard.NewHandler(dashboard.Sources{
		Projects: projectSource.Snapshot,
		Cluster:  clusterWatcher.Snapshot,
		Pods:     podWatcher.Snapshot,
		Images:   imageChecker,
	}, cfg)
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           webui.New(api, cfg.WebDir),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP shutdown: %v", err)
		}
	}()

	log.Printf("dashboard listening on %s", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
