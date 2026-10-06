import { useMemo, useState } from 'react'
import { FilterBar } from '../components/FilterBar'
import { DataTable, NameCell, type Column } from '../components/DataTable'
import { KpiRow } from '../components/Kpi'
import { Panel } from '../components/Panel'
import { SearchInput } from '../components/SearchInput'
import { matchesQuery } from '../lib/format'
import { projectGap } from '../lib/status'
import type { Navigate } from '../lib/useRoute'
import type { Project } from '../types'

const FILTERS: Record<string, { label: string; match: (project: Project) => boolean }> = {
  all: { label: 'Tümü', match: () => true },
  cron: { label: 'CronWorkflow’u olan', match: (p) => p.cronWorkflows > 0 },
  isvc: { label: 'InferenceService’i olan', match: (p) => p.inferenceServices > 0 },
  gap: { label: 'Kaynağı eksik', match: (p) => projectGap(p) !== undefined },
}

const count = (value: number) => value || <span className="muted">0</span>

const columns: Column<Project>[] = [
  { key: 'key', header: 'Proje', render: (p) => <NameCell name={p.key} namespace={p.namespace} />, sortValue: (p) => p.key },
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
export function Projects({ projects, namespace, filter, navigate }: { projects: Project[]; namespace: string; filter: string; navigate: Navigate }) {
  const [query, setQuery] = useState('')
  const active = FILTERS[filter] ?? FILTERS.all
  const kpis = [
    { key: 'all', label: 'Proje', value: projects.length, sub: 'Proje JSON’undan' },
    { key: 'cron', label: 'CronWorkflow’u olan', value: projects.filter(FILTERS.cron.match).length },
    { key: 'isvc', label: 'InferenceService’i olan', value: projects.filter(FILTERS.isvc.match).length },
    { key: 'gap', label: 'Kaynağı eksik', value: projects.filter(FILTERS.gap.match).length, sub: 'Namespace veya kaynak yok', tone: 'critical' as const },
  ]
  const visible = useMemo(() => projects.filter((p) => active.match(p) && matchesQuery(query, p.key, p.namespace)), [projects, active, query])

  return <div className="page">
    <KpiRow items={kpis} active={filter} onSelect={(key) => navigate({ filter: key })} />
    <Panel title="Proje kapsamı" subtitle="Proje JSON’undaki her proje ve namespace’inde bulunanlar · namespace = proje anahtarı (küçük harf, _ → -)"
      actions={<SearchInput value={query} onChange={setQuery} placeholder="Proje veya namespace ara" />}>
      <FilterBar namespace={namespace} filter={filter} filterLabel={active.label} shown={visible.length} total={projects.length} navigate={navigate} />
      <DataTable rows={visible} columns={columns} rowKey={(p) => p.key} empty="Bu filtrelerle eşleşen proje yok."
        initialSort={{ key: 'gap', direction: 1 }} />
    </Panel>
  </div>
}
