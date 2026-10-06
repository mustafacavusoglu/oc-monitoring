import type { CronWorkflow, Model, ModelType, Pod, Project } from '../types'

export type Tone = 'good' | 'warning' | 'critical' | 'info' | 'neutral'

type StatusInfo = { label: string; tone: Tone }

const STATUS: Record<string, StatusInfo> = {
  ready: { label: 'Hazır', tone: 'good' },
  notready: { label: 'Hazır değil', tone: 'critical' },
  succeeded: { label: 'Başarılı', tone: 'good' },
  failed: { label: 'Başarısız', tone: 'critical' },
  error: { label: 'Hata', tone: 'critical' },
  running: { label: 'Çalışıyor', tone: 'info' },
  pending: { label: 'Bekliyor', tone: 'warning' },
  exist: { label: 'Mevcut', tone: 'good' },
  missing: { label: 'Bulunamadı', tone: 'critical' },
  checking: { label: 'Kontrol ediliyor', tone: 'info' },
  syncing: { label: 'Senkronize ediliyor', tone: 'info' },
  degraded: { label: 'Sorunlu', tone: 'warning' },
  suspended: { label: 'Askıda', tone: 'neutral' },
  none: { label: 'Çalışma yok', tone: 'neutral' },
  unknown: { label: 'Bilinmiyor', tone: 'neutral' },
}

export function statusInfo(value?: string): StatusInfo {
  const key = (value || 'unknown').toLowerCase()
  return STATUS[key] ?? { label: value || 'Bilinmiyor', tone: 'neutral' }
}

export const TYPE_LABELS: Record<ModelType | 'batch', string> = { llm: 'LLM', ml: 'ML', batch: 'Batch', custom: 'Custom Serve' }

/** The CronWorkflow's current state: running, the last run's phase, or none. */
export function batchPhase(workflow: CronWorkflow): string {
  if (workflow.active) return 'running'
  return (workflow.lastRun?.phase || 'none').toLowerCase()
}

export function batchIssues(workflow: CronWorkflow): string[] {
  const issues: string[] = []
  const phase = batchPhase(workflow)
  if (phase === 'failed' || phase === 'error') issues.push(`Son çalışma: ${statusInfo(phase).label}`)
  const missing = workflow.images.filter((image) => image.status === 'missing').length
  if (missing) issues.push(`${missing} image Nexus'ta yok`)
  if (!workflow.suspended && !workflow.nextScheduledAt && workflow.scheduleError) issues.push(`Zamanlama: ${workflow.scheduleError}`)
  return issues
}

export const podHealthy = (pod: Pod) => pod.phase === 'Succeeded' || (pod.phase === 'Running' && pod.ready === pod.containers)

export const unhealthyPods = (model: Model) => (model.pods ?? []).filter((pod) => !podHealthy(pod)).length

export function modelIssues(model: Model): string[] {
  const issues: string[] = []
  if (model.runtimeMissing) issues.push(`ServingRuntime “${model.runtime}” bulunamadı`)
  if (model.state === 'NotReady') issues.push([model.reason, model.message].filter(Boolean).join(': ') || 'Hazır değil')
  const pods = unhealthyPods(model)
  if (pods) issues.push(`${pods} pod hazır değil`)
  return issues
}

/** What a monitored project is missing in the cluster, if anything. */
export function projectGap(project: Project): string | undefined {
  if (!project.namespaceExists) return 'Namespace yok'
  if (!project.cronWorkflows && !project.inferenceServices) return 'CronWorkflow / InferenceService yok'
  return undefined
}

/** Health bucket used by the charts and filters; tone doubles as the bucket key. */
export function modelTone(model: Model): Tone {
  return modelIssues(model).length ? 'critical' : statusInfo(model.state).tone
}

export function batchTone(workflow: CronWorkflow): Tone {
  if (batchIssues(workflow).length) return 'critical'
  const tone = statusInfo(batchPhase(workflow)).tone
  return tone === 'warning' ? 'info' : tone
}

/** Replicas held at minimum scale; KServe keeps one unless minReplicas says otherwise. */
const minScale = (model: Model) => model.minReplicas ?? 1

export const allocatedGpu = (model: Model) => model.gpu * minScale(model)

/** MIG slices held at minimum scale, per profile. */
export const allocatedMig = (model: Model) =>
  Object.entries(model.mig ?? {}).map(([profile, count]) => ({ profile, count: count * minScale(model) }))

export const usesAccelerator = (model: Model) => model.gpu > 0 || Object.keys(model.mig ?? {}).length > 0

/** "2 GPU" / "1g.5gb ×2" / "1 GPU · 3g.20gb ×1" for one replica. */
export function acceleratorLabel(model: Model) {
  const parts = Object.entries(model.mig ?? {}).map(([profile, count]) => `${profile} ×${count}`)
  if (model.gpu) parts.unshift(`${model.gpu} GPU`)
  return parts.join(' · ')
}
