import type { CronWorkflow, Model, ModelType } from '../types'

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

export const TYPE_LABELS: Record<ModelType | 'batch', string> = { llm: 'LLM', ml: 'ML', batch: 'Batch' }

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

export function modelIssues(model: Model): string[] {
  if (model.state !== 'NotReady') return []
  return [[model.reason, model.message].filter(Boolean).join(': ') || 'Hazır değil']
}

/** Health bucket used by the overview chart; tone doubles as the bucket key. */
export function modelTone(model: Model): Tone {
  return statusInfo(model.state).tone
}

export function batchTone(workflow: CronWorkflow): Tone {
  if (batchIssues(workflow).length) return 'critical'
  const tone = statusInfo(batchPhase(workflow)).tone
  return tone === 'warning' ? 'info' : tone
}

/** GPUs held at minimum scale; KServe keeps one replica unless minReplicas says otherwise. */
export const allocatedGpu = (model: Model) => model.gpu * (model.minReplicas ?? 1)
