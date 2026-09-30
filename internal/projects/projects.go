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
	LastSuccess *time.Time
	Stale       bool
	Error       string
}

type projectTarget struct {
	Namespace   string
	CheckImages bool
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
		var namespaces []string
		namespaces, err = selectBCHNamespaces(projects)
		if err == nil {
			log.Printf("project source generated BCH namespaces: project_entries=%d namespaces=%q", len(projects), namespaces)
			now := time.Now().UTC()
			s.mu.Lock()
			s.snapshot = Snapshot{Namespaces: namespaces, LastSuccess: &now}
			s.mu.Unlock()
			log.Printf("project source refresh completed: project_entries=%d BCH_namespaces=%d", len(projects), len(namespaces))
			return
		}
	}

	s.markStale(err)
}

func (s *Source) markStale(err error) {
	log.Printf("project source refresh failed: %v", err)
	s.mu.Lock()
	s.snapshot.Stale = true
	s.snapshot.Error = err.Error()
	s.mu.Unlock()
}

func selectBCHNamespaces(projects map[string]json.RawMessage) ([]string, error) {
	targets, err := projectTargets(projects)
	if err != nil {
		return nil, err
	}
	namespaces := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.CheckImages {
			namespaces = append(namespaces, target.Namespace)
		}
	}
	return namespaces, nil
}

func projectTargets(projects map[string]json.RawMessage) ([]projectTarget, error) {
	targets := make([]projectTarget, 0)
	seenNamespaces := make(map[string]string, len(projects))
	for projectKey, rawProject := range projects {
		var project map[string]json.RawMessage
		if err := json.Unmarshal(rawProject, &project); err != nil || project == nil {
			return nil, fmt.Errorf("project %q must contain a JSON object", projectKey)
		}
		namespace := strings.ToLower(strings.ReplaceAll(projectKey, "_", "-"))
		if previous, found := seenNamespaces[namespace]; found {
			return nil, fmt.Errorf("project keys %q and %q normalize to the same namespace %q", previous, projectKey, namespace)
		}
		seenNamespaces[namespace] = projectKey
		target := projectTarget{Namespace: namespace}
		servingRaw := projectField(project, "serving")
		if len(servingRaw) > 0 {
			if len(strings.TrimSpace(string(servingRaw))) == 0 || strings.TrimSpace(string(servingRaw))[0] != '[' {
				return nil, fmt.Errorf("project %q serving must be a string array", projectKey)
			}
			var serving []string
			if err := json.Unmarshal(servingRaw, &serving); err != nil {
				return nil, fmt.Errorf("project %q serving must be a string array", projectKey)
			}
			for _, value := range serving {
				if strings.Contains(strings.ToLower(strings.TrimSpace(value)), "bch") {
					target.CheckImages = true
					break
				}
			}
		}
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Namespace < targets[j].Namespace })
	return targets, nil
}

func projectField(project map[string]json.RawMessage, name string) json.RawMessage {
	for key, value := range project {
		if strings.EqualFold(key, name) {
			return value
		}
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
