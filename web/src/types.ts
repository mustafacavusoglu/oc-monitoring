export type ImageResult = {
  reference: string
  /** Project named by the image's bch-<project> segment; used in the Nexus URL. */
  project?: string
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
  image?: string
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
  /** Custom Serve models only. */
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
  type?: string
  batch: boolean
  customServe: boolean
  namespaceExists: boolean
  cronWorkflows: number
  inferenceServices: number
  pods: number
}

export type DashboardResponse = {
  generatedAt: string
  refreshIntervalSeconds: number
  sources: Sources
  namespaces: string[]
  projects: Project[]
  models: Model[]
  cronWorkflows: CronWorkflow[]
}
