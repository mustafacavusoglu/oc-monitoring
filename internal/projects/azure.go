package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"monitor/internal/config"
)

const maxProjectFileSize = 4 << 20

type azureClient struct {
	repoURL  string
	branch   string
	filePath string
	token    string
	http     *http.Client
}

func newAzureClient(cfg config.Config, client *http.Client) *azureClient {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &azureClient{
		repoURL:  cfg.AzureRepoURL,
		branch:   cfg.AzureRepoBranch,
		filePath: cfg.AzureProjectsPath,
		token:    cfg.AzureTokenFile,
		http:     client,
	}
}

func (c *azureClient) projects(ctx context.Context) (map[string]json.RawMessage, error) {
	requestURL, err := azureItemURL(c.repoURL, c.filePath, c.branch)
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create Azure Repos request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	if c.token != "" {
		token, err := readToken(c.token)
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth("", token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch project JSON from Azure Repos: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch project JSON from Azure Repos: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProjectFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read project JSON from Azure Repos: %w", err)
	}
	if len(body) > maxProjectFileSize {
		return nil, fmt.Errorf("project JSON exceeds %d bytes", maxProjectFileSize)
	}
	if len(strings.TrimSpace(string(body))) == 0 || strings.TrimSpace(string(body))[0] != '{' {
		return nil, fmt.Errorf("project JSON must be a top-level object")
	}
	var projects map[string]json.RawMessage
	if err := json.Unmarshal(body, &projects); err != nil {
		return nil, fmt.Errorf("parse project JSON: %w", err)
	}
	if projects == nil {
		return nil, fmt.Errorf("project JSON must be a top-level object")
	}
	return projects, nil
}

func readToken(file string) (string, error) {
	return readSecretFile(file)
}

func azureItemURL(repoURL, filePath, branch string) (string, error) {
	repo, err := url.Parse(repoURL)
	if err != nil || repo.Scheme != "https" || repo.Host == "" || repo.RawQuery != "" || repo.Fragment != "" {
		return "", fmt.Errorf("AZURE_REPO_URL must be an HTTPS Azure Repos repository URL")
	}
	segments := strings.Split(strings.Trim(repo.Path, "/"), "/")
	gitIndex := -1
	for i, segment := range segments {
		if segment == "_git" {
			gitIndex = i
			break
		}
	}
	if gitIndex < 1 || gitIndex+1 >= len(segments) {
		return "", fmt.Errorf("AZURE_REPO_URL must contain /_git/{repository}")
	}
	repository := segments[gitIndex+1]
	if repository == "" || strings.Contains(repository, "/") {
		return "", fmt.Errorf("invalid repository name in AZURE_REPO_URL")
	}
	prefix := strings.Join(segments[:gitIndex], "/")
	endpoint := &url.URL{Scheme: repo.Scheme, Host: repo.Host}
	endpoint.Path = path.Join("/", prefix, "_apis/git/repositories", repository, "items")
	query := endpoint.Query()
	filePath = "/" + strings.TrimLeft(strings.TrimSpace(filePath), "/")
	query.Set("path", filePath)
	query.Set("versionDescriptor.version", branch)
	query.Set("versionDescriptor.versionType", "branch")
	query.Set("download", "true")
	query.Set("api-version", "7.1")
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}
