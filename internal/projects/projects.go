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
)

type Snapshot struct {
	Namespaces  []string
	ImageIDs    map[string][]string
	LastSuccess *time.Time
	Stale       bool
	Error       string
}

type projectTarget struct {
	ProjectKey string
	Namespace  string
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
		log.Printf("project source fetched project metadata: entries=%d", len(projects))
		var targets []projectTarget
		targets, err = selectBCHProjects(projects)
		if err == nil {
			namespaces := make([]string, 0, len(targets))
			for _, target := range targets {
				namespaces = append(namespaces, target.Namespace)
			}
			if len(namespaces) == 0 {
				log.Printf("project source selected no BCH namespaces; inspecting project keys and serving values")
				logEmptyNamespaceSelection(projects)
			} else {
				log.Printf("project source selected BCH namespaces: count=%d namespaces=%q", len(namespaces), namespaces)
			}
			imageIDs := make(map[string][]string, len(namespaces))
			for _, target := range targets {
				imageIDs[target.Namespace], err = s.client.projectImageIDs(ctx, target.ProjectKey)
				if err != nil {
					err = fmt.Errorf("read image ID for project %q (namespace %q): %w", target.ProjectKey, target.Namespace, err)
					break
				}
				log.Printf("project source loaded image IDs: project=%q namespace=%q count=%d", target.ProjectKey, target.Namespace, len(imageIDs[target.Namespace]))
			}
			if err != nil {
				s.markStale(err)
				return
			}
			now := time.Now().UTC()
			s.mu.Lock()
			s.snapshot = Snapshot{Namespaces: namespaces, ImageIDs: imageIDs, LastSuccess: &now}
			s.mu.Unlock()
			log.Printf("project source refresh succeeded: project entries=%d namespaces=%d image IDs=%d", len(projects), len(namespaces), countImageIDs(imageIDs))
			return
		}
	}

	s.markStale(err)
}

func logEmptyNamespaceSelection(projects map[string]json.RawMessage) {
	keys := make([]string, 0, len(projects))
	for key := range projects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var project struct {
			Serving []string `json:"serving"`
		}
		if err := json.Unmarshal(projects[key], &project); err != nil {
			log.Printf("project source namespace candidate: key=%q metadata parse error=%v", key, err)
			continue
		}
		log.Printf("project source namespace candidate: key=%q serving=%q", key, project.Serving)
	}
}

func countImageIDs(imageIDs map[string][]string) int {
	count := 0
	for _, ids := range imageIDs {
		count += len(ids)
	}
	return count
}

func (s *Source) markStale(err error) {
	log.Printf("project source refresh failed: %v", err)
	s.mu.Lock()
	s.snapshot.Stale = true
	s.snapshot.Error = err.Error()
	s.mu.Unlock()
}

func selectBCHNamespaces(projects map[string]json.RawMessage) ([]string, error) {
	targets, err := selectBCHProjects(projects)
	if err != nil {
		return nil, err
	}
	namespaces := make([]string, 0, len(targets))
	for _, target := range targets {
		namespaces = append(namespaces, target.Namespace)
	}
	return namespaces, nil
}

func selectBCHProjects(projects map[string]json.RawMessage) ([]projectTarget, error) {
	targets := make([]projectTarget, 0)
	seenNamespaces := make(map[string]string, len(projects))
	for projectKey, rawProject := range projects {
		var project map[string]json.RawMessage
		if err := json.Unmarshal(rawProject, &project); err != nil || project == nil {
			return nil, fmt.Errorf("project %q must contain a JSON object", projectKey)
		}
		servingRaw, found := project["serving"]
		if !found {
			continue
		}
		if len(strings.TrimSpace(string(servingRaw))) == 0 || strings.TrimSpace(string(servingRaw))[0] != '[' {
			return nil, fmt.Errorf("project %q serving must be a string array", projectKey)
		}
		var serving []string
		if err := json.Unmarshal(servingRaw, &serving); err != nil {
			return nil, fmt.Errorf("project %q serving must be a string array", projectKey)
		}
		for _, value := range serving {
			if strings.Contains(strings.ToLower(strings.TrimSpace(value)), "bch") {
				namespace := strings.ToLower(strings.ReplaceAll(projectKey, "_", "-"))
				if previous, found := seenNamespaces[namespace]; found {
					return nil, fmt.Errorf("project keys %q and %q normalize to the same namespace %q", previous, projectKey, namespace)
				}
				seenNamespaces[namespace] = projectKey
				targets = append(targets, projectTarget{ProjectKey: projectKey, Namespace: namespace})
				break
			}
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Namespace < targets[j].Namespace })
	return targets, nil
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
