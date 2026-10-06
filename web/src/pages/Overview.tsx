import { useMemo } from 'react'
import { DataTable, NameCell, type Column } from '../components/DataTable'
import { KpiRow, type KpiItem } from '../components/Kpi'
import { Panel } from '../components/Panel'
import { StackedBars } from '../components/charts/StackedBars'
import { HEALTH_SERIES, TYPE_SERIES, groupRows, namespacePick, namespaceSelection } from '../lib/chart'
import { formatRelative } from '../lib/format'
import { TYPE_LABELS, batchIssues, batchTone, modelIssues, modelTone } from '../lib/status'
import type { Navigate, Page } from '../lib/useRoute'
import type { CronWorkflow, Model } from '../types'

type Kind = 'llm' | 'ml' | 'custom' | 'batch'
type Item = { kind: Kind; namespace: string; name: string; tone: string; issues: string[]; since?: string }

const NAMESPACE_ROWS = 10

/** Opens the item's page filtered to its namespace and problems. */
const openItem = (navigate: Navigate, item: Item) => navigate({ page: item.kind, namespace: item.namespace, filter: 'critical' })

const columns = (navigate: Navigate): Column<Item>[] => [
  {
    key: 'name', header: 'Kaynak', sortValue: (item) => item.name,
    render: (item) => <button type="button" className="link-button" title="Kaynağın sayfasında aç" onClick={() => openItem(navigate, item)}>
      <NameCell name={item.name} namespace={item.namespace} />
    </button>,
  },
  { key: 'kind', header: 'Tür', render: (item) => <span className={`type-tag type-${item.kind}`}>{TYPE_LABELS[item.kind]}</span>, sortValue: (item) => item.kind },
  { key: 'issue', header: 'Sorun', render: (item) => <span className="issue">{item.issues.join(' · ')}</span> },
  { key: 'since', header: 'Ne zamandır', render: (item) => formatRelative(item.since), sortValue: (item) => item.since ?? '', secondary: true },
]

export function Overview({ models, cronWorkflows, namespace, navigate }: {
  models: Model[]; cronWorkflows: CronWorkflow[]; namespace: string; navigate: Navigate
}) {
  const items = useMemo<Item[]>(() => [
    ...models.map((m) => ({ kind: m.type, namespace: m.namespace, name: m.name, tone: modelTone(m), issues: modelIssues(m), since: m.stateSince })),
    ...cronWorkflows.map((w) => ({
      kind: 'batch' as const, namespace: w.namespace, name: w.name, tone: batchTone(w), issues: batchIssues(w),
      since: w.lastRun?.finishedAt ?? w.lastRun?.startedAt,
    })),
  ], [models, cronWorkflows])

  const attention = items.filter((item) => item.issues.length)
  const byKind = (kind: Kind) => items.filter((item) => item.kind === kind)
  const kpis: KpiItem[] = (['llm', 'ml', 'custom', 'batch'] as const).map((kind) => {
    const all = byKind(kind)
    const problems = all.filter((item) => item.issues.length).length
    return { key: kind, label: kind === 'batch' ? 'Batch işleri' : kind === 'custom' ? 'Custom Serve' : `${TYPE_LABELS[kind]} modelleri`, value: all.length, sub: problems ? `${problems} sorunlu` : 'Sorun yok' }
  })
  kpis.push({ key: 'attention', label: 'Dikkat gerektiren', value: attention.length, sub: 'Tüm türlerde', tone: 'critical' })

  const namespaceRows = useMemo(() => groupRows(items, (item) => item.namespace, (item) => item.kind, NAMESPACE_ROWS), [items])
  const healthRows = TYPE_SERIES.map((series) => ({
    key: series.key, label: series.label,
    values: Object.fromEntries(HEALTH_SERIES.map((health) => [health.key, byKind(series.key as Kind).filter((item) => item.tone === health.key).length])),
  }))

  return <div className="page">
    <KpiRow items={kpis} onSelect={(key) => key !== 'attention' && navigate({ page: key as Page })} />
    <div className="grid-2">
      <Panel title="Namespace dağılımı" subtitle="Namespace başına kaynak sayısı, türe göre · segment türün sayfasını, namespace adı bu namespace’i açar">
        <StackedBars rows={namespaceRows} series={TYPE_SERIES} empty="Kaynak bulunamadı."
          selected={namespaceSelection(namespace, 'all', [])}
          onSelect={(pick) => navigate({ ...namespacePick(pick), ...(pick.series ? { page: pick.series as Page } : {}) })} />
      </Panel>
      <Panel title="Sağlık durumu" subtitle="Türe göre güncel durum · segmente tıklayınca o türün listesi bu durumla filtrelenir">
        <StackedBars rows={healthRows} series={HEALTH_SERIES} empty="Kaynak bulunamadı."
          onSelect={(pick) => navigate({ page: pick.row as Page, filter: pick.series ?? 'all' })} />
      </Panel>
    </div>
    <Panel title="Dikkat gerektirenler" subtitle={`${attention.length} kaynak · kaynağın adına tıklayınca kendi sayfasında açılır`}>
      <DataTable rows={attention} columns={columns(navigate)} rowKey={(item) => `${item.kind}/${item.namespace}/${item.name}`}
        empty="Her şey yolunda — sorunlu kaynak yok." initialSort={{ key: 'since', direction: -1 }} />
    </Panel>
  </div>
}
