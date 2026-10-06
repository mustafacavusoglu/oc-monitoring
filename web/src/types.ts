export type ImageResult = {
  reference: string
  imageId?: string
  url?: string
  status: string
  checkedAt?: string
  error?: string
}

export type Pod = {
  name: string
  phase: string
  ready: number
  containers: number
  restarts: number
  node?: string
  startedAt?: string
  containerStates?: string[]
}

export type RunSummary = {
  name: string
  phase: string
  startedAt?: string
  finishedAt?: string
}

export type WorkflowRun = RunSummary & {
  scheduledAt?: string
  createdAt?: string
  pods: Pod[]
}

export type CronWorkflow = {
  namespace: string
  name: string
  schedules: string[]
  timezone?: string
  suspended: boolean
  active: boolean
  lastScheduledAt?: string
  nextScheduledAt?: string
  scheduleError?: string
  lastRun?: WorkflowRun
  history: RunSummary[]
  images: ImageResult[]
}

export type ModelType = 'llm' | 'ml' | 'custom'

export type Model = {
  namespace: string
  name: string
  kind: string
  type: ModelType
  runtime?: string
  /** The named ServingRuntime does not exist in the namespace. */
  runtimeMissing?: boolean
  /** ServingRuntime (or LLMInferenceService) container images. */
  images?: string[]
  modelFormat?: string
  storageUri?: string
  url?: string
  state: 'Ready' | 'NotReady' | 'Unknown'
  reason?: string
  message?: string
  minReplicas?: number
  maxReplicas?: number
  gpu: number
  /** MIG slices per profile (e.g. "1g.5gb": 2), per replica. */
  mig?: Record<string, number>
  /** InferenceService pods (label serving.kserve.io/inferenceservice). */
  pods?: Pod[]
  createdAt?: string
  stateSince?: string
}

export type SourceHealth = {
  state: string
  lastSuccess?: string
  error?: string
}

export type Sources = {
  projects: SourceHealth
  cluster: SourceHealth
  pods: SourceHealth
  registry: SourceHealth
}

/** A project from the project JSON and what the cluster holds for it. */
export type Project = {
  key: string
  namespace: string
  namespaceExists: boolean
  cronWorkflows: number
  inferenceServices: number
  pods: number
}

/** OpenShift console base URL and "group~version~Kind" refs per kind. */
export type Console = { url: string; refs: Record<string, string> }

export type DashboardResponse = {
  generatedAt: string
  console: Console
  refreshIntervalSeconds: number
  sources: Sources
  namespaces: string[]
  projects: Project[]
  models: Model[]
  cronWorkflows: CronWorkflow[]
}
