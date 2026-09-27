package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr                 string
	WebDir                   string
	AzureRepoURL             string
	AzureRepoBranch          string
	AzureProjectsPath        string
	AzureTokenFile           string
	RegistryAuthFile         string
	ProjectRefreshInterval   time.Duration
	ImageCacheTTL            time.Duration
	RegistryCheckConcurrency int
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:          valueOr("HTTP_ADDR", ":8080"),
		WebDir:            valueOr("WEB_DIR", "web/dist"),
		AzureRepoURL:      strings.TrimSpace(os.Getenv("AZURE_REPO_URL")),
		AzureRepoBranch:   strings.TrimSpace(os.Getenv("AZURE_REPO_BRANCH")),
		AzureProjectsPath: strings.TrimSpace(os.Getenv("AZURE_PROJECTS_PATH")),
		AzureTokenFile:    strings.TrimSpace(os.Getenv("AZURE_TOKEN_FILE")),
		RegistryAuthFile:  strings.TrimSpace(os.Getenv("REGISTRY_AUTH_FILE")),
	}
	if cfg.AzureRepoURL == "" || cfg.AzureRepoBranch == "" || cfg.AzureProjectsPath == "" {
		return Config{}, fmt.Errorf("AZURE_REPO_URL, AZURE_REPO_BRANCH, and AZURE_PROJECTS_PATH are required")
	}

	var err error
	if cfg.ProjectRefreshInterval, err = durationOr("PROJECT_REFRESH_INTERVAL", 5*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.ImageCacheTTL, err = durationOr("IMAGE_CACHE_TTL", 5*time.Minute); err != nil {
		return Config{}, err
	}
	cfg.RegistryCheckConcurrency, err = intOr("REGISTRY_CHECK_CONCURRENCY", 4)
	if err != nil {
		return Config{}, err
	}
	if cfg.RegistryCheckConcurrency < 1 {
		return Config{}, fmt.Errorf("REGISTRY_CHECK_CONCURRENCY must be positive")
	}
	if cfg.ProjectRefreshInterval <= 0 || cfg.ImageCacheTTL <= 0 {
		return Config{}, fmt.Errorf("refresh intervals must be positive")
	}
	if cfg.RegistryAuthFile != "" {
		if filepath.Base(cfg.RegistryAuthFile) != "config.json" {
			return Config{}, fmt.Errorf("REGISTRY_AUTH_FILE must point to a Docker config file named config.json")
		}
		if err := os.Setenv("DOCKER_CONFIG", filepath.Dir(cfg.RegistryAuthFile)); err != nil {
			return Config{}, fmt.Errorf("configure registry credentials: %w", err)
		}
	}
	return cfg, nil
}

func valueOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationOr(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return duration, nil
}

func intOr(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}
