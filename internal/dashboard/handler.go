package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"monitor/internal/cluster"
	"monitor/internal/model"
	"monitor/internal/projects"
	"monitor/internal/registry"
)

type Handler struct {
	projectSnapshot func() projects.Snapshot
	clusterSnapshot func() cluster.Snapshot
	checkImages     func(context.Context, []string) map[string]model.ImageResult
}

func NewHandler(projectsSource *projects.Source, watcher *cluster.Watcher, checker *registry.Checker) http.Handler {
	return newHandler(projectsSource.Snapshot, watcher.Snapshot, checker.Check)
}

func newHandler(projectSnapshot func() projects.Snapshot, clusterSnapshot func() cluster.Snapshot, checkImages func(context.Context, []string) map[string]model.ImageResult) http.Handler {
	return &Handler{projectSnapshot: projectSnapshot, clusterSnapshot: clusterSnapshot, checkImages: checkImages}
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
	response := h.snapshot(r.Context())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		return
	}
}

func (h *Handler) snapshot(ctx context.Context) model.DashboardResponse {
	now := time.Now().UTC()
	projectSnapshot := h.projectSnapshot()
	clusterSnapshot := h.clusterSnapshot()
	views := cluster.BuildViews(clusterSnapshot, now)
	images := make([]string, 0)
	imageRefs := make(map[string][]string, len(projectSnapshot.ImageIDs))
	for project, imageIDs := range projectSnapshot.ImageIDs {
		for _, imageID := range imageIDs {
			ref := fmt.Sprintf("mlops/bch-%s:%s", project, imageID)
			images = append(images, ref)
			imageRefs[project] = append(imageRefs[project], ref)
		}
	}
	imageResults := h.checkImages(ctx, images)
	registryHealth := model.SourceHealth{State: "ready"}
	unknownImages := 0
	seenUnknown := make(map[string]struct{})
	for i := range views {
		views[i].Images = make([]model.ImageResult, 0)
		for _, ref := range imageRefs[views[i].Namespace] {
			result, found := imageResults[ref]
			if !found {
				continue
			}
			views[i].Images = append(views[i].Images, result)
			if result.Status == "unknown" {
				if _, seen := seenUnknown[result.Reference]; !seen {
					seenUnknown[result.Reference] = struct{}{}
					unknownImages++
				}
			} else if result.CheckedAt != nil && (registryHealth.LastSuccess == nil || result.CheckedAt.After(*registryHealth.LastSuccess)) {
				checkedAt := *result.CheckedAt
				registryHealth.LastSuccess = &checkedAt
			}
		}
	}
	if unknownImages > 0 {
		registryHealth.State = "degraded"
		registryHealth.Error = fmt.Sprintf("%d image checks are unknown", unknownImages)
	}

	projectHealth := model.SourceHealth{State: "ready", LastSuccess: projectSnapshot.LastSuccess}
	if projectSnapshot.Stale {
		projectHealth.State = "degraded"
		projectHealth.Error = projectSnapshot.Error
	}
	namespaces := append([]string{}, projectSnapshot.Namespaces...)
	sort.Strings(namespaces)
	return model.DashboardResponse{
		GeneratedAt:    now,
		ProjectSource:  projectHealth,
		ClusterSource:  clusterSnapshot.Health,
		RegistrySource: registryHealth,
		Namespaces:     namespaces,
		CronWorkflows:  views,
	}
}
