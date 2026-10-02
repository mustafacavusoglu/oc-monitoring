package config

import (
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

var validEnv = map[string]string{
	"HTTP_ADDR":                      ":8080",
	"WEB_DIR":                        "web/dist",
	"AZURE_REPO_URL":                 "https://dev.azure.com/example/project/_git/repo",
	"AZURE_REPO_BRANCH":              "main",
	"AZURE_PROJECTS_PATH":            "/projects.json",
	"BATCH_SERVING_KEYWORD":          "bch",
	"PROJECT_REFRESH_INTERVAL":       "5m",
	"NEXUS_MANIFEST_URL_TEMPLATE":    "https://nexus.example.test/v2/mlops/bch-{namespace}/manifests/{imageId}",
	"IMAGE_CACHE_TTL":                "24h",
	"REGISTRY_CHECK_CONCURRENCY":     "4",
	"UPSTREAM_TIMEOUT":               "10s",
	"LLM_RUNTIME_IMAGE_KEYWORDS":     "vllm",
	"ML_RUNTIME_IMAGE_KEYWORDS":      "triton, tritonserver",
	"GPU_RESOURCE_NAME":              "nvidia.com/gpu",
	"MIG_RESOURCE_PREFIX":            "nvidia.com/mig-",
	"CRONWORKFLOW_RESOURCE":          "argoproj.io/v1alpha1/cronworkflows",
	"WORKFLOW_RESOURCE":              "argoproj.io/v1alpha1/workflows",
	"INFERENCE_SERVICE_RESOURCE":     "serving.kserve.io/v1beta1/inferenceservices",
	"SERVING_RUNTIME_RESOURCE":       "serving.kserve.io/v1alpha1/servingruntimes",
	"LLM_INFERENCE_SERVICE_RESOURCE": "serving.kserve.io/v1alpha1/llminferenceservices",
	"UI_REFRESH_INTERVAL":            "30s",
}

func setEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	for key, value := range validEnv {
		t.Setenv(key, value)
	}
	for key, value := range overrides {
		t.Setenv(key, value)
	}
}

func TestLoadParsesConfigMapValues(t *testing.T) {
	setEnv(t, nil)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ImageCacheTTL != 24*time.Hour || cfg.UIRefreshInterval != 30*time.Second || cfg.RegistryCheckConcurrency != 4 {
		t.Fatalf("durations/ints = %s %s %d", cfg.ImageCacheTTL, cfg.UIRefreshInterval, cfg.RegistryCheckConcurrency)
	}
	if got := cfg.Serving.MLImageKeywords; len(got) != 2 || got[1] != "tritonserver" {
		t.Fatalf("ML keywords = %q", got)
	}
	want := schema.GroupVersionResource{Group: "serving.kserve.io", Version: "v1alpha1", Resource: "llminferenceservices"}
	if cfg.Resources.LLMInferenceServices != want {
		t.Fatalf("LLM resource = %v, want %v", cfg.Resources.LLMInferenceServices, want)
	}
}

func TestLoadReportsEveryMissingAndInvalidKey(t *testing.T) {
	setEnv(t, map[string]string{
		"NEXUS_MANIFEST_URL_TEMPLATE": "",
		"UI_REFRESH_INTERVAL":         "",
		"IMAGE_CACHE_TTL":             "-1h",
		"WORKFLOW_RESOURCE":           "workflows",
	})
	_, err := Load()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"NEXUS_MANIFEST_URL_TEMPLATE", "UI_REFRESH_INTERVAL", "IMAGE_CACHE_TTL", "WORKFLOW_RESOURCE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestLoadRequiresManifestTemplatePlaceholders(t *testing.T) {
	setEnv(t, map[string]string{"NEXUS_MANIFEST_URL_TEMPLATE": "https://nexus.example.test/{namespace}"})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "{imageId}") {
		t.Fatalf("error = %v, want missing {imageId}", err)
	}
}
