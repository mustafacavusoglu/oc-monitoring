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
	return newHandler(projectsSource.Snapshot, watcher.Snapshot, checker.Lookup)
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

func statusRank(status string) int {
	switch status {
	case "exist":
		return 0
	case "checking":
		return 1
	case "error":
		return 2
	case "unknown":
		return 3
	default:
		return 4
	}

}

func combinedResults(refs []string, results map[string]model.ImageResult) (model.ImageResult, bool) {
	var best model.ImageResult
	found := false
	for _, ref := range refs {
		result, ok := results[ref]
		if !ok {
			continue
		}
		if !found || statusRank(result.Status) > statusRank(best.Status) {
			best = result
			found = true
		}
	}
	return best, found
}



func (h *Handler) snapshot(ctx context.Context) model.DashboardResponse {
	now := time.Now().UTC()
	projectSnapshot := h.projectSnapshot()
	clusterSnapshot := h.clusterSnapshot()
	views := cluster.BuildViews(clusterSnapshot, now)
	images := make([]string, 0)
	imageRefs := make(map[string]map[string][]string, len(views))
	for i := range views {
		for j := range views[i].Images {
			image := &views[i].Images[j]
			if image.ImageID == "" {
				continue
			}
			ref := []string{
				fmt.Sprintf("bch-%s/manifests/%s", views[i].Namespace, image.ImageID),
				fmt.Sprintf("cm-%s/manifests/%s", views[i].Namespace, image.ImageID),
			}
			images = append(images, ref...)
			if imageRefs[views[i].Namespace] == nil {
				imageRefs[views[i].Namespace] = make(map[string][]string)
			}
			imageRefs[views[i].Namespace][image.Reference] = ref
		}
	}
	imageResults := h.checkImages(ctx, images)
	registryHealth := model.SourceHealth{State: "ready"}
	failedImages := 0
	seenFailed := make(map[string]struct{})
	for i := range views {
		for j := range views[i].Images {
			image := &views[i].Images[j]
			ref := imageRefs[views[i].Namespace][image.Reference]
			if len(ref) == 0 {
				continue
			}
			result, found := combinedResults(ref, imageResults)
			if !found {
				continue
			}
			image.URL = result.URL
			image.Status = result.Status
			image.Error = result.Error
			image.CheckedAt = result.CheckedAt
			if result.Status == "error" || result.Status == "unknown" {
				if _, seen := seenFailed[result.Reference]; !seen {
					seenFailed[result.Reference] = struct{}{}
					failedImages++
				}
			} else if result.CheckedAt != nil && (registryHealth.LastSuccess == nil || result.CheckedAt.After(*registryHealth.LastSuccess)) {
				checkedAt := *result.CheckedAt
				registryHealth.LastSuccess = &checkedAt
			}
		}
	}
	if failedImages > 0 {
		registryHealth.State = "degraded"
		registryHealth.Error = fmt.Sprintf("%d image checks failed", failedImages)
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
