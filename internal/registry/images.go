package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"monitor/internal/model"
)

const registryRequestTimeout = 10 * time.Second

type cacheEntry struct {
	result    model.ImageResult
	expiresAt time.Time
}

type Checker struct {
	ttl           time.Duration
	maxConcurrent int
	transport     http.RoundTripper
	slots         chan struct{}
	cache         map[string]cacheEntry
	mu            sync.Mutex
	inflight      map[string]chan struct{}
	lastPrune     time.Time
}

func NewChecker(authFile string, ttl time.Duration, maxConcurrent int) (*Checker, error) {
	if ttl <= 0 {
		return nil, fmt.Errorf("image cache TTL must be positive")
	}
	if maxConcurrent < 1 {
		return nil, fmt.Errorf("registry concurrency must be positive")
	}
	if authFile != "" {
		if filepath.Base(authFile) != "config.json" {
			return nil, fmt.Errorf("registry auth file must be named config.json")
		}
		if _, err := os.Stat(authFile); err != nil {
			return nil, fmt.Errorf("registry auth file is unavailable")
		}
		if err := os.Setenv("DOCKER_CONFIG", filepath.Dir(authFile)); err != nil {
			return nil, fmt.Errorf("configure registry credentials")
		}
	}
	return &Checker{
		ttl:           ttl,
		maxConcurrent: maxConcurrent,
		transport:     remote.DefaultTransport,
		slots:         make(chan struct{}, maxConcurrent),
		cache:         make(map[string]cacheEntry),
		inflight:      make(map[string]chan struct{}),
	}, nil
}

// Check returns results keyed by each input reference. Equivalent references
// share normalization, in-flight work, and cached registry lookups.
func (c *Checker) Check(ctx context.Context, refs []string) map[string]model.ImageResult {
	c.pruneExpired()
	results := make(map[string]model.ImageResult, len(refs))
	if len(refs) == 0 {
		return results
	}

	type requested struct {
		normalized string
		ref        name.Reference
		originals  []string
	}
	unique := make(map[string]requested)
	for _, raw := range refs {
		ref, err := name.ParseReference(strings.TrimSpace(raw))
		if err != nil {
			results[raw] = model.ImageResult{Reference: raw, Status: "unknown", Error: "invalid image reference"}
			continue
		}
		key := ref.String()
		job := unique[key]
		job.normalized, job.ref = key, ref
		job.originals = append(job.originals, raw)
		unique[key] = job
	}
	if len(unique) == 0 {
		return results
	}

	jobs := make(chan requested)
	var wg sync.WaitGroup
	var resultsMu sync.Mutex
	workers := min(len(unique), c.maxConcurrent)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				result := c.checkOne(ctx, job.normalized, job.ref)
				resultsMu.Lock()
				for _, raw := range job.originals {
					item := result
					item.Reference = raw
					results[raw] = item
				}
				resultsMu.Unlock()
			}
		}()
	}
	for _, job := range unique {
		jobs <- job
	}
	close(jobs)
	wg.Wait()
	return results
}

func (c *Checker) pruneExpired() {
	now := time.Now()
	interval := min(c.ttl, time.Minute)
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Sub(c.lastPrune) < interval {
		return
	}
	for key, entry := range c.cache {
		if !now.Before(entry.expiresAt) {
			delete(c.cache, key)
		}
	}
	c.lastPrune = now
}

func (c *Checker) checkOne(ctx context.Context, key string, ref name.Reference) model.ImageResult {
	for {
		if err := ctx.Err(); err != nil {
			return model.ImageResult{Reference: key, Status: "unknown", Error: "lookup cancelled"}
		}
		c.mu.Lock()
		if entry, ok := c.cache[key]; ok && time.Now().Before(entry.expiresAt) {
			c.mu.Unlock()
			return entry.result
		}
		if wait, ok := c.inflight[key]; ok {
			c.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return model.ImageResult{Reference: key, Status: "unknown", Error: "lookup cancelled"}
			}
		}
		wait := make(chan struct{})
		c.inflight[key] = wait
		c.mu.Unlock()

		select {
		case c.slots <- struct{}{}:
		case <-ctx.Done():
			c.mu.Lock()
			delete(c.inflight, key)
			close(wait)
			c.mu.Unlock()
			return model.ImageResult{Reference: key, Status: "unknown", Error: "lookup cancelled"}
		}
		result := lookup(ctx, ref, c.transport)
		<-c.slots
		c.mu.Lock()
		if ctx.Err() == nil {
			c.cache[key] = cacheEntry{result: result, expiresAt: time.Now().Add(c.ttl)}
		}
		delete(c.inflight, key)
		close(wait)
		c.mu.Unlock()
		return result
	}
}

func lookup(ctx context.Context, ref name.Reference, httpTransport http.RoundTripper) model.ImageResult {
	checkedAt := time.Now().UTC()
	requestCtx, cancel := context.WithTimeout(ctx, registryRequestTimeout)
	defer cancel()
	_, err := remote.Head(ref,
		remote.WithContext(requestCtx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithTransport(httpTransport),
	)
	return classifyLookupResult(ref, err, checkedAt, requestCtx.Err())
}

func classifyLookupResult(ref name.Reference, err error, checkedAt time.Time, contextErr error) model.ImageResult {
	result := model.ImageResult{Reference: ref.String(), CheckedAt: &checkedAt}
	if err == nil {
		result.Status = "present"
		return result
	}
	var registryErr *transport.Error
	if errors.As(err, &registryErr) {
		if registryErr.StatusCode == http.StatusNotFound {
			result.Status = "missing"
			return result
		}
		result.Status = "unknown"
		result.Error = fmt.Sprintf("registry returned HTTP %d", registryErr.StatusCode)
		return result
	}
	result.Status = "unknown"
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(contextErr, context.DeadlineExceeded) {
		result.Error = "registry lookup timed out"
	} else if errors.Is(err, context.Canceled) || errors.Is(contextErr, context.Canceled) {
		result.Error = "lookup cancelled"
	} else {
		result.Error = "registry lookup failed"
	}
	return result
}
