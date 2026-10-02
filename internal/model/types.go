package model

import "time"

// Source health states.
const (
	StateReady    = "ready"
	StateSyncing  = "syncing"
	StateDegraded = "degraded"
)

// Image check states.
const (
	ImageExist    = "exist"
	ImageMissing  = "missing"
	ImageError    = "error"
	ImageUnknown  = "unknown"
	ImageChecking = "checking"
)

// Model readiness states, derived from the resource's Ready condition.
const (
	ModelReady    = "Ready"
	ModelNotReady = "NotReady"
	ModelUnknown  = "Unknown"
)

// Model types.
const (
	TypeLLM = "llm"
	TypeML  = "ml"
)

type ImageResult struct {
	Reference string     `json:"reference"`
	ImageID   string     `json:"imageId,omitempty"`
	URL       string     `json:"url,omitempty"`
	Status    string     `json:"status"`
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
	Error     string     `json:"error,omitempty"`
}

type Pod struct {
	Name            string   `json:"name"`
	Phase           string   `json:"phase"`
	ContainerStates []string `json:"containerStates,omitempty"`
}

type RunSummary struct {
	Name       string     `json:"name"`
	Phase      string     `json:"phase"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type WorkflowRun struct {
	RunSummary
	ScheduledAt *time.Time `json:"scheduledAt,omitempty"`
	CreatedAt   *time.Time `json:"createdAt,omitempty"`
	Pods        []Pod      `json:"pods"`
}

type CronWorkflow struct {
	Namespace       string        `json:"namespace"`
	Name            string        `json:"name"`
	Schedules       []string      `json:"schedules"`
	Timezone        string        `json:"timezone,omitempty"`
	Suspended       bool          `json:"suspended"`
	Active          bool          `json:"active"`
	LastScheduledAt *time.Time    `json:"lastScheduledAt,omitempty"`
	NextScheduledAt *time.Time    `json:"nextScheduledAt,omitempty"`
	ScheduleError   string        `json:"scheduleError,omitempty"`
	LastRun         *WorkflowRun  `json:"lastRun,omitempty"`
	History         []RunSummary  `json:"history"` // newest first
	Images          []ImageResult `json:"images"`
}

// Model is an online-serving deployment: a KServe InferenceService classified
// by its ServingRuntime image, or an LLMInferenceService.
type Model struct {
	Namespace   string     `json:"namespace"`
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	Type        string     `json:"type"`
	Runtime     string     `json:"runtime,omitempty"`
	Image       string     `json:"image,omitempty"`
	ModelFormat string     `json:"modelFormat,omitempty"`
	StorageURI  string     `json:"storageUri,omitempty"`
	URL         string     `json:"url,omitempty"`
	State       string     `json:"state"`
	Reason      string     `json:"reason,omitempty"`
	Message     string     `json:"message,omitempty"`
	MinReplicas *int64     `json:"minReplicas,omitempty"`
	MaxReplicas *int64     `json:"maxReplicas,omitempty"`
	GPU         int64      `json:"gpu"`
	CreatedAt   *time.Time `json:"createdAt,omitempty"`
	StateSince  *time.Time `json:"stateSince,omitempty"`
}

type SourceHealth struct {
	State       string     `json:"state"`
	LastSuccess *time.Time `json:"lastSuccess,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type Sources struct {
	Projects SourceHealth `json:"projects"`
	Batch    SourceHealth `json:"batch"`
	Models   SourceHealth `json:"models"`
	Registry SourceHealth `json:"registry"`
}

type DashboardResponse struct {
	GeneratedAt            time.Time      `json:"generatedAt"`
	RefreshIntervalSeconds int            `json:"refreshIntervalSeconds"`
	Sources                Sources        `json:"sources"`
	Namespaces             []string       `json:"namespaces"`
	Models                 []Model        `json:"models"`
	CronWorkflows          []CronWorkflow `json:"cronWorkflows"`
}
