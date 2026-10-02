// Package dashboard serves the single JSON snapshot the web UI renders.
package dashboard

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"monitor/internal/batch"
	"monitor/internal/cluster"
	"monitor/internal/config"
	"monitor/internal/model"
	"monitor/internal/projects"
	"monitor/internal/serving"
)

// Sources are the read-only views the handler combines. Every call is served
// from in-memory caches, so a request never waits on Kubernetes, Azure or Nexus.
type Sources struct {
	Projects func() projects.Snapshot
	Batch    func() cluster.Snapshot
	Models   func() cluster.Snapshot
	Images   batch.ImageChecker
}

type Handler struct {
	sources Sources
	cfg     config.Config
}

func NewHandler(sources Sources, cfg config.Config) *Handler {
	return &Handler{sources: sources, cfg: cfg}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/dashboard" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Accept-Encoding")
	var out io.Writer = w
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		out = gz
	}
	if err := json.NewEncoder(out).Encode(h.snapshot(r.Context(), time.Now().UTC())); err != nil {
		log.Printf("dashboard response: %v", err)
	}
}

func (h *Handler) snapshot(ctx context.Context, now time.Time) model.DashboardResponse {
	projectSnapshot := h.sources.Projects()
	batchSnapshot := h.sources.Batch()
	modelSnapshot := h.sources.Models()
	res := h.cfg.Resources

	cronWorkflows := batch.BuildViews(
		batchSnapshot.Objects[res.CronWorkflows],
		batchSnapshot.Objects[res.Workflows],
		batchSnapshot.Objects[cluster.Pods],
		now,
	)
	models := serving.BuildModels(
		modelSnapshot.Objects[res.InferenceServices],
		modelSnapshot.Objects[res.ServingRuntimes],
		modelSnapshot.Objects[res.LLMInferenceServices],
		h.cfg.Serving,
	)

	projectHealth := model.SourceHealth{State: model.StateReady, LastSuccess: projectSnapshot.LastSuccess}
	if projectSnapshot.Stale {
		projectHealth.State, projectHealth.Error = model.StateDegraded, projectSnapshot.Error
	}

	return model.DashboardResponse{
		GeneratedAt:            now,
		RefreshIntervalSeconds: int(h.cfg.UIRefreshInterval.Seconds()),
		Sources: model.Sources{
			Projects: projectHealth,
			Batch:    batchSnapshot.Health,
			Models:   modelSnapshot.Health,
			Registry: batch.ResolveImages(ctx, cronWorkflows, h.sources.Images),
		},
		Namespaces:    namespaces(projectSnapshot.Namespaces, models),
		Models:        models,
		CronWorkflows: cronWorkflows,
	}
}

// namespaces lists every namespace the UI can filter by: watched batch
// namespaces plus the namespaces that host a model.
func namespaces(batchNamespaces []string, models []model.Model) []string {
	seen := make(map[string]struct{}, len(batchNamespaces)+len(models))
	for _, namespace := range batchNamespaces {
		seen[namespace] = struct{}{}
	}
	for _, m := range models {
		seen[m.Namespace] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for namespace := range seen {
		result = append(result, namespace)
	}
	sort.Strings(result)
	return result
}
