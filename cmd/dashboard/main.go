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
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	projectSource := projects.NewSource(cfg, nil)
	clusterWatcher, err := cluster.NewWatcher(nil, func() []string {
		return projectSource.Snapshot().Namespaces
	})
	if err != nil {
		log.Fatal(err)
	}
	imageChecker, err := registry.NewChecker(cfg.NexusURL, cfg.ImageCacheTTL, cfg.RegistryCheckConcurrency)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go projectSource.Run(ctx)
	go clusterWatcher.Run(ctx)

	api := dashboard.NewHandler(projectSource, clusterWatcher, imageChecker)
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
