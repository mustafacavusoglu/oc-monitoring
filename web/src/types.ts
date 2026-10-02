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

export type ModelType = 'llm' | 'ml'

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
  batch: SourceHealth
  models: SourceHealth
  registry: SourceHealth
}

export type DashboardResponse = {
  generatedAt: string
  refreshIntervalSeconds: number
  sources: Sources
  namespaces: string[]
  models: Model[]
  cronWorkflows: CronWorkflow[]
}
