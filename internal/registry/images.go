package registry

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"monitor/internal/model"
)

const requestTimeout = 10 * time.Second

type cacheEntry struct {
	result    model.ImageResult
	expiresAt time.Time
}

type Checker struct {
	baseURL       string
	ttl           time.Duration
	maxConcurrent int
	client        *http.Client
	slots         chan struct{}
	cache         map[string]cacheEntry
	mu            sync.Mutex
	inflight      map[string]chan struct{}
}

func NewChecker(baseURL string, ttl time.Duration, maxConcurrent int) (*Checker, error) {
	if ttl <= 0 {
		return nil, fmt.Errorf("image cache TTL must be positive")
	}
	if maxConcurrent < 1 {
		return nil, fmt.Errorf("registry concurrency must be positive")
	}
	return &Checker{
		baseURL:       strings.TrimRight(baseURL, "/"),
		ttl:           ttl,
		maxConcurrent: maxConcurrent,
		client:        &http.Client{Timeout: requestTimeout},
		slots:         make(chan struct{}, maxConcurrent),
		cache:         make(map[string]cacheEntry),
		inflight:      make(map[string]chan struct{}),
	}, nil
}

func (c *Checker) Check(ctx context.Context, refs []string) map[string]model.ImageResult {
	results := make(map[string]model.ImageResult, len(refs))
	jobs := make(chan string)
	var wg sync.WaitGroup
	var resultsMu sync.Mutex
	workers := min(len(refs), c.maxConcurrent)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for imageID := range jobs {
				result := c.checkOne(ctx, imageID)
				resultsMu.Lock()
				results[imageID] = result
				resultsMu.Unlock()
			}
		}()
	}
	for _, imageID := range refs {
		jobs <- imageID
	}
	close(jobs)
	wg.Wait()
	return results
}

func (c *Checker) checkOne(ctx context.Context, imageID string) model.ImageResult {
	for {
		if err := ctx.Err(); err != nil {
			return model.ImageResult{Reference: imageID, Status: "unknown", Error: "lookup cancelled"}
		}
		c.mu.Lock()
		if entry, ok := c.cache[imageID]; ok && time.Now().Before(entry.expiresAt) {
			c.mu.Unlock()
			return entry.result
		}
		if wait, ok := c.inflight[imageID]; ok {
			c.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return model.ImageResult{Reference: imageID, Status: "unknown", Error: "lookup cancelled"}
			}
		}
		wait := make(chan struct{})
		c.inflight[imageID] = wait
		c.mu.Unlock()

		select {
		case c.slots <- struct{}{}:
		case <-ctx.Done():
			c.mu.Lock()
			delete(c.inflight, imageID)
			close(wait)
			c.mu.Unlock()
			return model.ImageResult{Reference: imageID, Status: "unknown", Error: "lookup cancelled"}
		}
		result := c.get(ctx, imageID)
		<-c.slots
		c.mu.Lock()
		if ctx.Err() == nil {
			c.cache[imageID] = cacheEntry{result: result, expiresAt: time.Now().Add(c.ttl)}
		}
		delete(c.inflight, imageID)
		close(wait)
		c.mu.Unlock()
		return result
	}
}

func (c *Checker) get(ctx context.Context, imageID string) model.ImageResult {
	checkedAt := time.Now().UTC()
	result := model.ImageResult{Reference: imageID, CheckedAt: &checkedAt, Status: "error"}
	requestURL, err := url.JoinPath(c.baseURL, imageID)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	resp, err := c.client.Do(req)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		result.Status = "exist"
	case http.StatusNotFound:
		result.Status = "missing"
	default:
		result.Error = fmt.Sprintf("unexpected HTTP status %d", resp.StatusCode)
	}
	return result
}
