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
	// Batch resources are watched only in the namespaces selected from the
	// project file; models are watched cluster-wide.
	batchWatcher := cluster.NewWatcher("batch", clients,
		[]schema.GroupVersionResource{res.CronWorkflows, res.Workflows, cluster.Pods},
		func() []string { return projectSource.Snapshot().Namespaces })
	modelWatcher := cluster.NewWatcher("models", clients,
		[]schema.GroupVersionResource{res.InferenceServices, res.ServingRuntimes, res.LLMInferenceServices},
		cluster.AllNamespaces)
	imageChecker := registry.NewChecker(cfg.NexusManifestURLTemplate, cfg.ImageCacheTTL, cfg.RegistryCheckConcurrency, cfg.UpstreamTimeout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go projectSource.Run(ctx)
	go batchWatcher.Run(ctx)
	go modelWatcher.Run(ctx)

	api := dashboard.NewHandler(dashboard.Sources{
		Projects: projectSource.Snapshot,
		Batch:    batchWatcher.Snapshot,
		Models:   modelWatcher.Snapshot,
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
