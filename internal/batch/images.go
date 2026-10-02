package batch

import (
	"context"
	"fmt"

	"monitor/internal/model"
)

// ImageChecker resolves project images against the registry.
type ImageChecker interface {
	ManifestURL(namespace, imageID string) string
	Lookup(ctx context.Context, urls []string) map[string]model.ImageResult
}

// ResolveImages fills the registry status of every project image in place and
// returns the registry's health as seen by this lookup.
func ResolveImages(ctx context.Context, views []model.CronWorkflow, checker ImageChecker) model.SourceHealth {
	var urls []string
	for i := range views {
		for j := range views[i].Images {
			image := &views[i].Images[j]
			if image.ImageID != "" {
				image.URL = checker.ManifestURL(views[i].Namespace, image.ImageID)
				urls = append(urls, image.URL)
			}
		}
	}
	results := checker.Lookup(ctx, urls)

	health := model.SourceHealth{State: model.StateReady}
	failed := make(map[string]struct{})
	for i := range views {
		for j := range views[i].Images {
			image := &views[i].Images[j]
			result, found := results[image.URL]
			if image.URL == "" || !found {
				continue
			}
			image.Status, image.Error, image.CheckedAt = result.Status, result.Error, result.CheckedAt
			switch {
			case result.Status == model.ImageError || result.Status == model.ImageUnknown:
				failed[image.URL] = struct{}{}
			case result.CheckedAt != nil && (health.LastSuccess == nil || result.CheckedAt.After(*health.LastSuccess)):
				checkedAt := *result.CheckedAt
				health.LastSuccess = &checkedAt
			}
		}
	}
	if len(failed) > 0 {
		health.State = model.StateDegraded
		health.Error = fmt.Sprintf("%d image checks failed", len(failed))
	}
	return health
}
