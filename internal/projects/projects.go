package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"monitor/internal/config"
	"monitor/internal/model"
)

type Snapshot struct {
	// Projects lists every batch or Custom Serve project.
	Projects []model.Project
	// Skipped lists project entries that could not be read.
	Skipped     []string
	LastSuccess *time.Time
	Stale       bool
	Error       string
}

func (s Snapshot) namespaces(match func(model.Project) bool) []string {
	var namespaces []string
	for _, project := range s.Projects {
		if match(project) {
			namespaces = append(namespaces, project.Namespace)
		}
	}
	return namespaces
}

func (s Snapshot) BatchNamespaces() []string {
	return s.namespaces(func(p model.Project) bool { return p.Batch })
}

func (s Snapshot) CustomServeNamespaces() []string {
	return s.namespaces(func(p model.Project) bool { return p.CustomServe })
}

// WatchedNamespaces are the namespaces whose pods are watched.
func (s Snapshot) WatchedNamespaces() []string {
	return s.namespaces(func(p model.Project) bool { return p.Batch || p.CustomServe })
}

type rules struct {
	batchKeyword    string // matched in the `serving` list
	customServeType string // matched against the `type` field
}

type Source struct {
	client   *azureClient
	interval time.Duration
	rules    rules

	mu       sync.RWMutex
	snapshot Snapshot
}

func NewSource(cfg config.Config, client *http.Client) *Source {
	return &Source{
		client:   newAzureClient(cfg, client),
		interval: cfg.ProjectRefreshInterval,
		rules: rules{
			batchKeyword:    strings.ToLower(cfg.BatchServingKeyword),
			customServeType: cfg.CustomServeType,
		},
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
	projects, skipped := parseProjects(raw, s.rules)
	snapshot := Snapshot{Projects: projects, Skipped: skipped}
	log.Printf("project source refresh completed: entries=%d selected=%d batch=%d custom_serve=%d skipped=%d",
		len(raw), len(projects), len(snapshot.BatchNamespaces()), len(snapshot.CustomServeNamespaces()), len(skipped))
	for _, reason := range skipped {
		log.Printf("project source skipped entry: %s", reason)
	}
	now := time.Now().UTC()
	snapshot.LastSuccess = &now
	s.mu.Lock()
	s.snapshot = snapshot
	s.mu.Unlock()
}

// parseProjects keeps batch and Custom Serve projects. A malformed entry is
// skipped (and reported) instead of failing the whole project list.
func parseProjects(raw map[string]json.RawMessage, r rules) ([]model.Project, []string) {
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var projects []model.Project
	var skipped []string
	owners := make(map[string]string, len(raw))
	for _, key := range keys {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw[key], &fields); err != nil || fields == nil {
			skipped = append(skipped, fmt.Sprintf("%q is not a JSON object", key))
			continue
		}
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

		serving := stringList(field(fields, "serving"))
		projectType := strings.Join(stringList(field(fields, "type")), ",")
		project := model.Project{
			Key:         key,
			Namespace:   namespace,
			Type:        projectType,
			Batch:       slices.ContainsFunc(serving, func(v string) bool { return strings.Contains(strings.ToLower(v), r.batchKeyword) }),
			CustomServe: strings.EqualFold(strings.TrimSpace(projectType), r.customServeType),
		}
		if project.Batch || project.CustomServe {
			projects = append(projects, project)
		}
	}
	return projects, skipped
}

func field(fields map[string]json.RawMessage, name string) json.RawMessage {
	for key, value := range fields {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return nil
}

// stringList reads a JSON string or string array.
func stringList(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var single string
	if json.Unmarshal(raw, &single) == nil && single != "" {
		return []string{single}
	}
	return nil
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
