package model

import "time"

type ImageResult struct {
	Reference string     `json:"reference"`
	Status    string     `json:"status"`
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
	Error     string     `json:"error,omitempty"`
}

type Pod struct {
	Name            string   `json:"name"`
	Phase           string   `json:"phase"`
	ContainerStates []string `json:"containerStates,omitempty"`
	Images          []string `json:"images,omitempty"`
}

type WorkflowRun struct {
	Name        string     `json:"name"`
	Phase       string     `json:"phase"`
	ScheduledAt *time.Time `json:"scheduledAt,omitempty"`
	CreatedAt   *time.Time `json:"createdAt,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
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
	Images          []ImageResult `json:"images"`
}

type SourceHealth struct {
	State       string     `json:"state"`
	LastSuccess *time.Time `json:"lastSuccess,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type DashboardResponse struct {
	GeneratedAt    time.Time      `json:"generatedAt"`
	ProjectSource  SourceHealth   `json:"projectSource"`
	ClusterSource  SourceHealth   `json:"clusterSource"`
	RegistrySource SourceHealth   `json:"registrySource"`
	Namespaces     []string       `json:"namespaces"`
	CronWorkflows  []CronWorkflow `json:"cronWorkflows"`
}
