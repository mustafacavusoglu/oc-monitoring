// Package config loads every environment-specific setting from the process
// environment, which the Deployment fills from a ConfigMap (and an optional
// Secret). Nothing here has an in-code default: a missing key is a startup error.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type Config struct {
	HTTPAddr string
	WebDir   string

	AzureRepoURL           string
	AzureRepoBranch        string
	AzureProjectsPath      string
	AzureToken             string // optional, from Secret
	AzureTokenFile         string // optional, from Secret mount
	ProjectRefreshInterval time.Duration

	// NexusManifestURLTemplate contains {namespace} and {imageId} placeholders.
	NexusManifestURLTemplate string
	ImageCacheTTL            time.Duration
	RegistryCheckConcurrency int
	UpstreamTimeout          time.Duration

	Serving   ServingRules
	Resources Resources

	UIRefreshInterval time.Duration
	// ConsoleURL is the OpenShift web console the UI links resources to.
	ConsoleURL string
}

// ServingRules classifies KServe InferenceServices by their ServingRuntime image.
type ServingRules struct {
	LLMImageKeywords []string
	MLImageKeywords  []string
	GPUResourceName  string
	// MIGResourcePrefix matches MIG slice resources; the rest of the name is the profile.
	MIGResourcePrefix string
}

// Resources holds the API group/version/resource of each watched CRD so that
// cluster-specific API versions are configured, not compiled in.
type Resources struct {
	CronWorkflows        schema.GroupVersionResource
	Workflows            schema.GroupVersionResource
	InferenceServices    schema.GroupVersionResource
	ServingRuntimes      schema.GroupVersionResource
	LLMInferenceServices schema.GroupVersionResource
}

func Load() (Config, error) {
	var e env
	cfg := Config{
		HTTPAddr:                 e.str("HTTP_ADDR"),
		WebDir:                   e.str("WEB_DIR"),
		AzureRepoURL:             e.str("AZURE_REPO_URL"),
		AzureRepoBranch:          e.str("AZURE_REPO_BRANCH"),
		AzureProjectsPath:        e.str("AZURE_PROJECTS_PATH"),
		AzureToken:               strings.TrimSpace(os.Getenv("AZURE_TOKEN")),
		AzureTokenFile:           strings.TrimSpace(os.Getenv("AZURE_TOKEN_FILE")),
		ProjectRefreshInterval:   e.duration("PROJECT_REFRESH_INTERVAL"),
		NexusManifestURLTemplate: e.str("NEXUS_MANIFEST_URL_TEMPLATE"),
		ImageCacheTTL:            e.duration("IMAGE_CACHE_TTL"),
		RegistryCheckConcurrency: e.positiveInt("REGISTRY_CHECK_CONCURRENCY"),
		UpstreamTimeout:          e.duration("UPSTREAM_TIMEOUT"),
		Serving: ServingRules{
			LLMImageKeywords:  e.list("LLM_RUNTIME_IMAGE_KEYWORDS"),
			MLImageKeywords:   e.list("ML_RUNTIME_IMAGE_KEYWORDS"),
			GPUResourceName:   e.str("GPU_RESOURCE_NAME"),
			MIGResourcePrefix: e.str("MIG_RESOURCE_PREFIX"),
		},
		Resources: Resources{
			CronWorkflows:        e.resource("CRONWORKFLOW_RESOURCE"),
			Workflows:            e.resource("WORKFLOW_RESOURCE"),
			InferenceServices:    e.resource("INFERENCE_SERVICE_RESOURCE"),
			ServingRuntimes:      e.resource("SERVING_RUNTIME_RESOURCE"),
			LLMInferenceServices: e.resource("LLM_INFERENCE_SERVICE_RESOURCE"),
		},
		UIRefreshInterval: e.duration("UI_REFRESH_INTERVAL"),
		ConsoleURL:        strings.TrimRight(e.str("OPENSHIFT_CONSOLE_URL"), "/"),
	}
	for _, placeholder := range []string{"{namespace}", "{imageId}"} {
		if cfg.NexusManifestURLTemplate != "" && !strings.Contains(cfg.NexusManifestURLTemplate, placeholder) {
			e.fail("NEXUS_MANIFEST_URL_TEMPLATE must contain %s", placeholder)
		}
	}
	if err := e.err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// env collects every missing or invalid key so one startup error lists them all.
type env struct {
	missing  []string
	problems []string
}

func (e *env) str(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		e.missing = append(e.missing, key)
	}
	return value
}

func (e *env) duration(key string) time.Duration {
	value := e.str(key)
	if value == "" {
		return 0
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		e.fail("%s must be a positive duration, got %q", key, value)
	}
	return d
}

func (e *env) positiveInt(key string) int {
	value := e.str(key)
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		e.fail("%s must be a positive integer, got %q", key, value)
	}
	return n
}

func (e *env) list(key string) []string {
	var items []string
	for _, item := range strings.Split(e.str(key), ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

// resource parses "group/version/resource"; the core group is written "/v1/pods".
func (e *env) resource(key string) schema.GroupVersionResource {
	value := e.str(key)
	if value == "" {
		return schema.GroupVersionResource{}
	}
	parts := strings.Split(value, "/")
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		e.fail("%s must look like group/version/resource, got %q", key, value)
		return schema.GroupVersionResource{}
	}
	return schema.GroupVersionResource{Group: parts[0], Version: parts[1], Resource: parts[2]}
}

func (e *env) fail(format string, args ...any) {
	e.problems = append(e.problems, fmt.Sprintf(format, args...))
}

func (e *env) err() error {
	var errs []error
	if len(e.missing) > 0 {
		errs = append(errs, fmt.Errorf("missing required settings: %s", strings.Join(e.missing, ", ")))
	}
	for _, problem := range e.problems {
		errs = append(errs, errors.New(problem))
	}
	return errors.Join(errs...)
}
