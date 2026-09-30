package config

import (
	"testing"
	"time"
)

func TestImageCacheTTLDefaultsToOneDay(t *testing.T) {
	t.Setenv("AZURE_REPO_URL", "https://dev.azure.com/example/project/_git/repo")
	t.Setenv("AZURE_REPO_BRANCH", "main")
	t.Setenv("AZURE_PROJECTS_PATH", "/projects.json")
	t.Setenv("NEXUS_URL", "https://nexus.example.test/repository/mlops")
	t.Setenv("IMAGE_CACHE_TTL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ImageCacheTTL != 24*time.Hour {
		t.Fatalf("image cache TTL = %s, want 24h", cfg.ImageCacheTTL)
	}
}
