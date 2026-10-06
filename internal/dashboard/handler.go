// Package dashboard serves the single JSON snapshot the web UI renders.
package dashboard

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Sources are the read-only views the handler combines. Every call is served
// from in-memory caches, so a request never waits on Kubernetes, Azure or Nexus.
type Sources struct {
	Projects func() projects.Snapshot
	Cluster  func() cluster.Snapshot // cluster-wide resources
	Pods     func() cluster.Snapshot // pods of the watched project namespaces
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
	clusterSnapshot := h.sources.Cluster()
	podSnapshot := h.sources.Pods()
	objects, pods := clusterSnapshot.Objects, podSnapshot.Objects[cluster.Pods]
	res := h.cfg.Resources

	// Everything comes from the cluster; the project file only names namespaces.
	cronWorkflows := batch.BuildViews(objects[res.CronWorkflows], objects[res.Workflows], pods, now)
	models := serving.BuildModels(
		objects[res.InferenceServices],
		objects[res.ServingRuntimes],
		objects[res.LLMInferenceServices],
		pods,
		h.cfg.Serving,
	)

	projectHealth := model.SourceHealth{State: model.StateReady, LastSuccess: projectSnapshot.LastSuccess}
	switch {
	case projectSnapshot.Stale:
		projectHealth.State, projectHealth.Error = model.StateDegraded, projectSnapshot.Error
	case len(projectSnapshot.Skipped) > 0:
		projectHealth.Error = fmt.Sprintf("%d project entries skipped: %s", len(projectSnapshot.Skipped), strings.Join(projectSnapshot.Skipped, "; "))
	}

	return model.DashboardResponse{
		GeneratedAt:            now,
		RefreshIntervalSeconds: int(h.cfg.UIRefreshInterval.Seconds()),
		Sources: model.Sources{
			Projects: projectHealth,
			Cluster:  clusterSnapshot.Health,
			Pods:     podSnapshot.Health,
			Registry: batch.ResolveImages(ctx, cronWorkflows, h.sources.Images),
		},
		Namespaces:    namespaces(projectSnapshot.Namespaces(), models, cronWorkflows),
		Projects:      coverage(projectSnapshot.Projects, objects, res, pods),
		Models:        models,
		CronWorkflows: cronWorkflows,
	}
}

// coverage adds to every project what the cluster holds in its namespace, so
// a project with no resources (or no namespace) is visible.
func coverage(source []model.Project, objects map[schema.GroupVersionResource][]*unstructured.Unstructured, res config.Resources, pods []*unstructured.Unstructured) []model.Project {
	count := func(items []*unstructured.Unstructured) map[string]int {
		counts := make(map[string]int)
		for _, item := range items {
			counts[item.GetNamespace()]++
		}
		return counts
	}
	existing := make(map[string]bool)
	for _, namespace := range objects[cluster.Namespaces] {
		existing[namespace.GetName()] = true
	}
	cronWorkflows, inferenceServices, podCounts := count(objects[res.CronWorkflows]), count(objects[res.InferenceServices]), count(pods)

	result := make([]model.Project, 0, len(source))
	for _, project := range source {
		project.NamespaceExists = existing[project.Namespace]
		project.CronWorkflows = cronWorkflows[project.Namespace]
		project.InferenceServices = inferenceServices[project.Namespace]
		project.Pods = podCounts[project.Namespace]
		result = append(result, project)
	}
	return result
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

// namespaces lists every namespace the UI can filter by: project namespaces
// plus every namespace that hosts a model or a CronWorkflow.
func namespaces(projectNamespaces []string, models []model.Model, cronWorkflows []model.CronWorkflow) []string {
	seen := make(map[string]struct{}, len(projectNamespaces)+len(models))
	for _, namespace := range projectNamespaces {
		seen[namespace] = struct{}{}
	}
	for _, m := range models {
		seen[m.Namespace] = struct{}{}
	}
	for _, w := range cronWorkflows {
		seen[w.Namespace] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for namespace := range seen {
		result = append(result, namespace)
	}
	sort.Strings(result)
	return result
}
