// Package registry checks whether image manifests exist in Nexus. Lookups
// never block a dashboard request: results are served from a TTL cache and
// expired or unknown URLs are refreshed in the background.
package registry

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"monitor/internal/model"
)

const manifestAccept = "application/vnd.docker.distribution.manifest.v2+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.oci.image.index.v1+json, " +
	"application/json"

type cacheEntry struct {
	result    model.ImageResult
	expiresAt time.Time
}

type Checker struct {
	urlTemplate string
	ttl         time.Duration
	client      *http.Client
	slots       chan struct{}

	mu       sync.Mutex
	cache    map[string]cacheEntry
	inflight map[string]struct{}
}

func NewChecker(urlTemplate string, ttl time.Duration, maxConcurrent int, timeout time.Duration) *Checker {
	return &Checker{
		urlTemplate: urlTemplate,
		ttl:         ttl,
		client:      &http.Client{Timeout: timeout},
		slots:       make(chan struct{}, maxConcurrent),
		cache:       make(map[string]cacheEntry),
		inflight:    make(map[string]struct{}),
	}
}

// ManifestURL fills the configured template's {namespace} and {imageId}.
func (c *Checker) ManifestURL(namespace, imageID string) string {
	return strings.NewReplacer(
		"{namespace}", url.PathEscape(namespace),
		"{imageId}", url.PathEscape(imageID),
	).Replace(c.urlTemplate)
}

// Lookup returns the cached result for each URL, or "checking" while a
// background refresh is running.
func (c *Checker) Lookup(ctx context.Context, urls []string) map[string]model.ImageResult {
	background := context.WithoutCancel(ctx)
	results := make(map[string]model.ImageResult, len(urls))
	started, cacheHits, inFlight := 0, 0, 0
	now := time.Now()
	c.mu.Lock()
	for _, manifestURL := range urls {
		if _, done := results[manifestURL]; done {
			continue
		}
		entry, cached := c.cache[manifestURL]
		_, running := c.inflight[manifestURL]
		fresh := cached && now.Before(entry.expiresAt)
		switch {
		case fresh:
			cacheHits++
		case running:
			inFlight++
		default:
			started++
			c.inflight[manifestURL] = struct{}{}
			go c.refresh(background, manifestURL)
		}
		if cached {
			results[manifestURL] = entry.result
		} else {
			results[manifestURL] = model.ImageResult{Reference: manifestURL, URL: manifestURL, Status: model.ImageChecking}
		}
	}
	c.mu.Unlock()
	slog.Info("registry image check cycle", "references", len(results), "started", started, "cache_hits", cacheHits, "in_flight", inFlight)
	return results
}

func (c *Checker) refresh(ctx context.Context, manifestURL string) {
	c.slots <- struct{}{}
	result := c.get(ctx, manifestURL)
	<-c.slots
	c.mu.Lock()
	c.cache[manifestURL] = cacheEntry{result: result, expiresAt: time.Now().Add(c.ttl)}
	delete(c.inflight, manifestURL)
	c.mu.Unlock()
}

func (c *Checker) get(ctx context.Context, manifestURL string) model.ImageResult {
	checkedAt := time.Now().UTC()
	result := model.ImageResult{Reference: manifestURL, URL: manifestURL, CheckedAt: &checkedAt, Status: model.ImageError}
	fail := func(err error) model.ImageResult {
		result.Error = err.Error()
		slog.Info("registry image check failed", "method", http.MethodGet, "url", manifestURL, "error", err)
		return result
	}
	slog.Info("registry image check request", "method", http.MethodGet, "url", manifestURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return fail(err)
	}
	req.Header.Set("Accept", manifestAccept)
	resp, err := c.client.Do(req)
	if err != nil {
		return fail(err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		result.Status = model.ImageExist
	case http.StatusNotFound:
		result.Status = model.ImageMissing
	default:
		result.Error = fmt.Sprintf("unexpected HTTP status %d", resp.StatusCode)
	}
	slog.Info("registry image check result", "url", manifestURL, "status", resp.StatusCode, "state", result.Status)
	return result
}
