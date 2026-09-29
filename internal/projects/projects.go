package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"monitor/internal/config"
)

type Snapshot struct {
	Namespaces  []string
	ImageIDs    map[string][]string
	LastSuccess *time.Time
	Stale       bool
	Error       string
}

type Source struct {
	client   *azureClient
	interval time.Duration

	mu       sync.RWMutex
	snapshot Snapshot
}

func NewSource(cfg config.Config, client *http.Client) *Source {
	return &Source{
		client:   newAzureClient(cfg, client),
		interval: cfg.ProjectRefreshInterval,
		snapshot: Snapshot{Stale: true, Error: "waiting for first project refresh"},
	}
}

func (s *Source) Run(ctx context.Context) {
	s.refresh(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refresh(ctx)
		}
	}
}

func (s *Source) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := s.snapshot
	snapshot.Namespaces = append([]string(nil), s.snapshot.Namespaces...)
	snapshot.ImageIDs = make(map[string][]string, len(s.snapshot.ImageIDs))
	for namespace, imageIDs := range s.snapshot.ImageIDs {
		snapshot.ImageIDs[namespace] = append([]string(nil), imageIDs...)
	}
	if s.snapshot.LastSuccess != nil {
		lastSuccess := *s.snapshot.LastSuccess
		snapshot.LastSuccess = &lastSuccess
	}
	return snapshot
}

func (s *Source) refresh(ctx context.Context) {
	projects, err := s.client.projects(ctx)
	if err == nil {
		var namespaces []string
		namespaces, err = selectBCHNamespaces(projects)
		if err == nil {
			imageIDs := make(map[string][]string, len(namespaces))
			for _, namespace := range namespaces {
				imageIDs[namespace], err = s.client.projectImageIDs(ctx, namespace)
				if err != nil {
					err = fmt.Errorf("read image ID for namespace %q: %w", namespace, err)
					break
				}
			}
			if err != nil {
				s.markStale(err)
				return
			}
			now := time.Now().UTC()
			s.mu.Lock()
			s.snapshot = Snapshot{Namespaces: namespaces, ImageIDs: imageIDs, LastSuccess: &now}
			s.mu.Unlock()
			return
		}
	}

	s.markStale(err)
}

func (s *Source) markStale(err error) {
	s.mu.Lock()
	s.snapshot.Stale = true
	s.snapshot.Error = err.Error()
	s.mu.Unlock()
}

func selectBCHNamespaces(projects map[string]json.RawMessage) ([]string, error) {
	namespaces := make([]string, 0)
	for namespace, rawProject := range projects {
		var project map[string]json.RawMessage
		if err := json.Unmarshal(rawProject, &project); err != nil || project == nil {
			return nil, fmt.Errorf("project %q must contain a JSON object", namespace)
		}
		servingRaw, found := project["serving"]
		if !found {
			continue
		}
		if len(strings.TrimSpace(string(servingRaw))) == 0 || strings.TrimSpace(string(servingRaw))[0] != '[' {
			return nil, fmt.Errorf("project %q serving must be a string array", namespace)
		}
		var serving []string
		if err := json.Unmarshal(servingRaw, &serving); err != nil {
			return nil, fmt.Errorf("project %q serving must be a string array", namespace)
		}
		for _, value := range serving {
			if strings.EqualFold(strings.TrimSpace(value), "BCH") {
				namespaces = append(namespaces, namespace)
				break
			}
		}
	}
	sort.Strings(namespaces)
	return namespaces, nil
}

func readSecretFile(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read Azure token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("Azure token file is empty")
	}
	return token, nil
}
