import { useMemo, useState } from 'react'
import { DataTable, NameCell, type Column } from '../components/DataTable'
import { KpiRow } from '../components/Kpi'
import { Panel } from '../components/Panel'
import { SearchInput } from '../components/SearchInput'
import { matchesQuery } from '../lib/format'
import { projectGap } from '../lib/status'
import type { Project } from '../types'

type Filter = 'all' | 'batch' | 'custom' | 'gap'

const FILTERS: Record<Filter, (project: Project) => boolean> = {
  all: () => true,
  batch: (p) => p.batch,
  custom: (p) => p.customServe,
  gap: (p) => projectGap(p) !== undefined,
}

const count = (value: number) => value || <span className="muted">0</span>

const columns: Column<Project>[] = [
  { key: 'key', header: 'Proje', render: (p) => <NameCell name={p.key} namespace={p.namespace} />, sortValue: (p) => p.key },
  {
    key: 'type', header: 'Tür', sortValue: (p) => Number(p.batch) + 2 * Number(p.customServe),
    render: (p) => <div className="stack">
      {p.batch ? <span className="type-tag type-batch">BCH</span> : null}
      {p.customServe ? <span className="type-tag type-custom">Custom Serve</span> : null}
      {!p.batch && !p.customServe ? <span className="muted">—</span> : null}
    </div>,
  },
  {
    key: 'gap', header: 'Durum', sortValue: (p) => (projectGap(p) ? 0 : 1),
    render: (p) => projectGap(p) ? <span className="badge tone-critical"><span aria-hidden="true">✕</span>{projectGap(p)}</span>
      : <span className="badge tone-good"><span aria-hidden="true">✓</span>Kaynaklar bulundu</span>,
  },
  { key: 'cron', header: 'CronWorkflow', render: (p) => count(p.cronWorkflows), sortValue: (p) => p.cronWorkflows },
  { key: 'isvc', header: 'InferenceService', render: (p) => count(p.inferenceServices), sortValue: (p) => p.inferenceServices },
  { key: 'pods', header: 'Pod', render: (p) => count(p.pods), sortValue: (p) => p.pods, secondary: true },
]

/** Every project from the project JSON next to what the cluster holds, so gaps are visible. */
export function Projects({ projects, outside }: { projects: Project[]; outside: string[] }) {
  const [filter, setFilter] = useState<Filter>('all')
  const [query, setQuery] = useState('')
  const kpis = [
    { key: 'all', label: 'Proje', value: projects.length, sub: 'Proje JSON’undan' },
    { key: 'batch', label: 'BCH projesi', value: projects.filter(FILTERS.batch).length, sub: 'Nexus image kontrolü yapılır' },
    { key: 'custom', label: 'Custom Serve projesi', value: projects.filter(FILTERS.custom).length },
    { key: 'gap', label: 'Kaynağı eksik', value: projects.filter(FILTERS.gap).length, sub: 'Namespace veya kaynak yok', tone: 'critical' as const },
  ]
  const visible = useMemo(() => projects.filter((p) => FILTERS[filter](p) && matchesQuery(query, p.key, p.namespace)), [projects, filter, query])

  return <div className="page">
    <KpiRow items={kpis} active={filter} onSelect={(key) => setFilter(key as Filter)} />
    <Panel title="Proje kapsamı" subtitle={`${visible.length} / ${projects.length} proje · namespace = proje anahtarı (küçük harf, _ → -)`}
      actions={<SearchInput value={query} onChange={setQuery} placeholder="Proje veya namespace ara" />}>
      <DataTable rows={visible} columns={columns} rowKey={(p) => p.key} empty="Bu filtrelerle eşleşen proje yok."
        initialSort={{ key: 'gap', direction: 1 }} />
    </Panel>
    {outside.length ? <Panel title="Proje namespace’i dışındaki CronWorkflow’lar" subtitle={`${outside.length} CronWorkflow · namespace’i proje JSON’unda yok, dashboard’da gösterilmez`}>
      <ul className="plain-list">{outside.map((name) => <li key={name}><code>{name}</code></li>)}</ul>
    </Panel> : null}
  </div>
}
