export type ImageResult = {
  reference: string
  imageId?: string
  url?: string
  status: 'exist' | 'missing' | 'error' | 'unknown' | 'checking' | string
  checkedAt?: string
  error?: string
}

export type Pod = {
  name: string
  phase: string
  containerStates?: string[]
  images?: string[]
}

export type WorkflowRun = {
  name: string
  phase: string
  scheduledAt?: string
  createdAt?: string
  startedAt?: string
  finishedAt?: string
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
  images: ImageResult[]
}

export type SourceHealth = {
  state: string
  lastSuccess?: string
  error?: string
}

export type DashboardResponse = {
  generatedAt: string
  projectSource: SourceHealth
  clusterSource: SourceHealth
  registrySource: SourceHealth
  namespaces: string[]
  cronWorkflows: CronWorkflow[]
}
