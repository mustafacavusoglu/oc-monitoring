package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"monitor/internal/config"
	"monitor/internal/model"
)

type Snapshot struct {
	// Projects lists every project in the project JSON.
	Projects []model.Project
	// Skipped lists project entries that could not be read.
	Skipped     []string
	LastSuccess *time.Time
	Stale       bool
	Error       string
}

// Namespaces are the namespaces of every project.
func (s Snapshot) Namespaces() []string {
	namespaces := make([]string, 0, len(s.Projects))
	for _, project := range s.Projects {
		namespaces = append(namespaces, project.Namespace)
	}
	return namespaces
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
	return s.snapshot // slices are replaced, never mutated, so sharing is safe
}

func (s *Source) refresh(ctx context.Context) {
	raw, err := s.client.projects(ctx)
	if err != nil {
		log.Printf("project source refresh failed: %v", err)
		s.mu.Lock()
		s.snapshot.Stale = true
		s.snapshot.Error = err.Error()
		s.mu.Unlock()
		return
	}
	projects, skipped := parseProjects(raw)
	snapshot := Snapshot{Projects: projects, Skipped: skipped}
	log.Printf("project source refresh completed: entries=%d namespaces=%d skipped=%d", len(raw), len(projects), len(skipped))
	for _, reason := range skipped {
		log.Printf("project source skipped entry: %s", reason)
	}
	now := time.Now().UTC()
	snapshot.LastSuccess = &now
	s.mu.Lock()
	s.snapshot = snapshot
	s.mu.Unlock()
}

// parseProjects turns every project key into its namespace (PROJECT_NAME →
// project-name). Only the keys matter; everything else comes from the cluster.
// An empty key or a second key for the same namespace is skipped and reported.
func parseProjects(raw map[string]json.RawMessage) ([]model.Project, []string) {
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var projects []model.Project
	var skipped []string
	owners := make(map[string]string, len(raw))
	for _, key := range keys {
		namespace := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "_", "-"))
		if namespace == "" {
			skipped = append(skipped, "empty project key")
			continue
		}
		if owner, taken := owners[namespace]; taken {
			skipped = append(skipped, fmt.Sprintf("%q maps to namespace %q already used by %q", key, namespace, owner))
			continue
		}
		owners[namespace] = key
		projects = append(projects, model.Project{Key: key, Namespace: namespace})
	}
	return projects, skipped
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
