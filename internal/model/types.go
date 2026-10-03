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
	TypeLLM         = "llm"
	TypeML          = "ml"
	TypeCustomServe = "custom"
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
	Name            string     `json:"name"`
	Phase           string     `json:"phase"`
	Ready           int        `json:"ready"` // ready containers
	Containers      int        `json:"containers"`
	Restarts        int64      `json:"restarts"`
	Node            string     `json:"node,omitempty"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	ContainerStates []string   `json:"containerStates,omitempty"`
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
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Type        string `json:"type"`
	Runtime     string `json:"runtime,omitempty"`
	Image       string `json:"image,omitempty"`
	ModelFormat string `json:"modelFormat,omitempty"`
	StorageURI  string `json:"storageUri,omitempty"`
	URL         string `json:"url,omitempty"`
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	Message     string `json:"message,omitempty"`
	MinReplicas *int64 `json:"minReplicas,omitempty"`
	MaxReplicas *int64 `json:"maxReplicas,omitempty"`
	GPU         int64  `json:"gpu"`
	// MIG holds MIG slices per profile (e.g. "1g.5gb": 2), per replica.
	MIG map[string]int64 `json:"mig,omitempty"`
	// Pods of a Custom Serve model (label serving.kserve.io/inferenceservice).
	Pods       []Pod      `json:"pods,omitempty"`
	CreatedAt  *time.Time `json:"createdAt,omitempty"`
	StateSince *time.Time `json:"stateSince,omitempty"`
}

type SourceHealth struct {
	State       string     `json:"state"`
	LastSuccess *time.Time `json:"lastSuccess,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type Sources struct {
	Projects SourceHealth `json:"projects"`
	Cluster  SourceHealth `json:"cluster"`
	Pods     SourceHealth `json:"pods"`
	Registry SourceHealth `json:"registry"`
}

// Project is a project from the project JSON, with what the cluster actually
// holds for it so gaps are visible. Batch means its `serving` list has the
// batch keyword (its images are checked in Nexus).
type Project struct {
	Key         string `json:"key"`
	Namespace   string `json:"namespace"`
	Type        string `json:"type,omitempty"`
	Batch       bool   `json:"batch"`
	CustomServe bool   `json:"customServe"`

	NamespaceExists   bool `json:"namespaceExists"`
	CronWorkflows     int  `json:"cronWorkflows"`
	InferenceServices int  `json:"inferenceServices"`
	Pods              int  `json:"pods"`
}

type DashboardResponse struct {
	GeneratedAt            time.Time      `json:"generatedAt"`
	RefreshIntervalSeconds int            `json:"refreshIntervalSeconds"`
	Sources                Sources        `json:"sources"`
	Namespaces             []string       `json:"namespaces"`
	Projects               []Project      `json:"projects"`
	Models                 []Model        `json:"models"`
	CronWorkflows          []CronWorkflow `json:"cronWorkflows"`
	// OutsideProjects lists "namespace/name" of CronWorkflows in namespaces
	// that belong to no project, so the cluster total can be reconciled.
	OutsideProjects []string `json:"outsideProjects"`
}
