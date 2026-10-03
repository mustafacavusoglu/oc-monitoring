import { useMemo, useState } from 'react'
import { DataTable, NameCell, type Column } from '../components/DataTable'
import { DetailList } from '../components/DetailList'
import { KpiRow } from '../components/Kpi'
import { Panel } from '../components/Panel'
import { PodList } from '../components/PodList'
import { SearchInput } from '../components/SearchInput'
import { StatusBadge } from '../components/StatusBadge'
import { Donut } from '../components/charts/Donut'
import { RunHistory } from '../components/charts/RunHistory'
import { Timeline } from '../components/charts/Timeline'
import { formatDateTime, formatDuration, formatRelative, formatTime, matchesQuery } from '../lib/format'
import { batchPhase, statusInfo } from '../lib/status'
import type { CronWorkflow, ImageResult } from '../types'

type Filter = 'all' | 'running' | 'failed' | 'missing' | 'suspended'

const TIMELINE_HOURS = 24
const IMAGE_SEVERITY = ['missing', 'error', 'checking', 'unknown', 'exist']

const FILTERS: Record<Filter, (workflow: CronWorkflow) => boolean> = {
  all: () => true,
  running: (w) => batchPhase(w) === 'running',
  failed: (w) => ['failed', 'error'].includes(batchPhase(w)),
  missing: (w) => w.images.some((image) => image.status === 'missing'),
  suspended: (w) => w.suspended,
}

/** The worst image status of a workflow, for a one-badge summary. */
const imageSummary = (images: ImageResult[]) =>
  IMAGE_SEVERITY.find((status) => images.some((image) => image.status === status))

const lastRunTime = (w: CronWorkflow) => w.lastRun?.startedAt ?? w.lastRun?.createdAt ?? w.lastScheduledAt

const columns: Column<CronWorkflow>[] = [
  { key: 'name', header: 'CronWorkflow', render: (w) => <NameCell name={w.name} namespace={w.namespace} />, sortValue: (w) => w.name },
  {
    key: 'state', header: 'Durum', sortValue: (w) => batchPhase(w),
    render: (w) => <div className="stack"><StatusBadge value={batchPhase(w)} />{w.suspended ? <StatusBadge value="suspended" /> : null}</div>,
  },
  { key: 'history', header: 'Son çalışmalar', render: (w) => <RunHistory runs={w.history} />, secondary: true },
  { key: 'schedule', header: 'Zamanlama', render: (w) => <code className="cron">{w.schedules.join(' · ') || '—'}</code>, secondary: true },
  {
    key: 'last', header: 'Son çalışma', sortValue: (w) => lastRunTime(w) ?? '',
    render: (w) => <div className="stack"><span>{formatRelative(lastRunTime(w))}</span>
      {w.lastRun ? <small className="muted">{formatDuration(w.lastRun.startedAt, w.lastRun.finishedAt)}</small> : null}</div>,
  },
  { key: 'next', header: 'Sonraki', render: (w) => w.suspended ? '—' : formatRelative(w.nextScheduledAt), sortValue: (w) => w.nextScheduledAt ?? '9' },
  { key: 'images', header: 'Image', render: (w) => imageSummary(w.images) ? <StatusBadge value={imageSummary(w.images)} /> : <span className="muted">—</span>, sortValue: (w) => IMAGE_SEVERITY.indexOf(imageSummary(w.images) ?? 'exist') },
]

function WorkflowDetail({ workflow }: { workflow: CronWorkflow }) {
  const run = workflow.lastRun
  return <div className="detail-grid">
    <section>
      <h3>Zamanlama</h3>
      <DetailList items={[
        ['Schedule', <code>{workflow.schedules.join(' · ') || '—'}</code>],
        ['Saat dilimi', workflow.timezone],
        ['Son tetiklenme', formatDateTime(workflow.lastScheduledAt)],
        ['Sonraki', workflow.suspended ? 'Askıda' : formatDateTime(workflow.nextScheduledAt)],
        ['Uyarı', workflow.scheduleError && <span className="issue">{workflow.scheduleError}</span>],
      ]} />
    </section>
    <section>
      <h3>Son workflow</h3>
      {run ? <>
        <DetailList items={[
          ['Ad', run.name],
          ['Başlangıç', formatDateTime(run.startedAt ?? run.createdAt)],
          ['Bitiş', formatDateTime(run.finishedAt)],
          ['Süre', formatDuration(run.startedAt, run.finishedAt)],
        ]} />
        <h4>Pod’lar ({run.pods.length})</h4>
        <PodList pods={run.pods} />
      </> : <p className="muted">Henüz workflow çalışmamış.</p>}
    </section>
    <section>
      <h3>Image’lar ({workflow.images.length})</h3>
      {workflow.images.length ? <ul className="item-list">{workflow.images.map((image) => <li key={image.reference}>
        <div className="stack">
          <strong>Image ID: <code>{image.imageId || '—'}</code></strong>
          <small className="muted break"><code>{image.url || image.reference}</code></small>
          {image.error ? <small className="issue">{image.error}</small> : null}
        </div>
        <StatusBadge value={image.status} />
      </li>)}</ul> : <p className="muted">Proje image’ı bulunamadı.</p>}
    </section>
  </div>
}

export function Batch({ cronWorkflows, now }: { cronWorkflows: CronWorkflow[]; now: number }) {
  const [filter, setFilter] = useState<Filter>('all')
  const [query, setQuery] = useState('')

  const count = (key: Filter) => cronWorkflows.filter(FILTERS[key]).length
  const kpis = [
    { key: 'all', label: 'CronWorkflow', value: cronWorkflows.length },
    { key: 'running', label: 'Çalışıyor', value: count('running'), tone: 'info' as const },
    { key: 'failed', label: 'Son çalışma başarısız', value: count('failed'), tone: 'critical' as const },
    { key: 'missing', label: 'Eksik image', value: count('missing'), tone: 'critical' as const },
    { key: 'suspended', label: 'Askıda', value: count('suspended') },
  ]

  const slices = useMemo(() => {
    const phases = cronWorkflows.map(batchPhase)
    const of = (...keys: string[]) => phases.filter((phase) => keys.includes(phase)).length
    return [
      { key: 'succeeded', label: statusInfo('succeeded').label, value: of('succeeded'), color: 'var(--good)' },
      { key: 'failed', label: 'Başarısız / hata', value: of('failed', 'error'), color: 'var(--critical)' },
      { key: 'running', label: statusInfo('running').label, value: of('running'), color: 'var(--info)' },
      { key: 'pending', label: statusInfo('pending').label, value: of('pending'), color: 'var(--warning)' },
      { key: 'none', label: statusInfo('none').label, value: of('none'), color: 'var(--neutral)' },
    ]
  }, [cronWorkflows])

  const upcoming = useMemo(() => cronWorkflows.filter((w) => w.nextScheduledAt && !w.suspended).map((w) => {
    const at = Date.parse(w.nextScheduledAt!)
    return { key: `${w.namespace}/${w.name}`, label: w.name, detail: `${w.namespace} · ${formatTime(new Date(at))}`, at }
  }), [cronWorkflows])

  const visible = useMemo(() => cronWorkflows.filter((w) => FILTERS[filter](w) && matchesQuery(query, w.name, w.namespace)), [cronWorkflows, filter, query])

  return <div className="page">
    <KpiRow items={kpis} active={filter} onSelect={(key) => setFilter(key as Filter)} />
    <div className="grid-2 grid-wide-right">
      <Panel title="Son çalışma durumu" subtitle="CronWorkflow başına son çalışmanın sonucu">
        <Donut slices={slices} centerLabel="CronWorkflow" />
      </Panel>
      <Panel title="Önümüzdeki 24 saat" subtitle="Planlanan çalışmalar">
        <Timeline items={upcoming} now={now} hours={TIMELINE_HOURS} empty="Önümüzdeki 24 saatte planlanan çalışma yok." />
      </Panel>
    </div>
    <Panel title="CronWorkflow’lar" subtitle={`${visible.length} / ${cronWorkflows.length} kayıt`}
      actions={<SearchInput value={query} onChange={setQuery} placeholder="CronWorkflow veya namespace ara" />}>
      <DataTable rows={visible} columns={columns} rowKey={(w) => `${w.namespace}/${w.name}`} detail={(w) => <WorkflowDetail workflow={w} />}
        empty="Bu filtrelerle eşleşen CronWorkflow yok." />
    </Panel>
  </div>
}
