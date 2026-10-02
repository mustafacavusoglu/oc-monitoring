import { useMemo, useState } from 'react'
import { DataTable, NameCell, type Column } from '../components/DataTable'
import { DetailList } from '../components/DetailList'
import { KpiRow } from '../components/Kpi'
import { Panel } from '../components/Panel'
import { SearchInput } from '../components/SearchInput'
import { StatusBadge } from '../components/StatusBadge'
import { StackedBars } from '../components/charts/StackedBars'
import { HEALTH_SERIES, TYPE_SERIES, groupRows } from '../lib/chart'
import { formatDateTime, formatRelative, matchesQuery } from '../lib/format'
import { allocatedGpu, modelTone } from '../lib/status'
import type { Model, ModelType } from '../types'

type Filter = 'all' | 'Ready' | 'NotReady' | 'gpu'

const NAMESPACE_ROWS = 8

const FILTERS: Record<Filter, (model: Model) => boolean> = {
  all: () => true,
  Ready: (model) => model.state === 'Ready',
  NotReady: (model) => model.state !== 'Ready',
  gpu: (model) => model.gpu > 0,
}

const replicas = (model: Model) =>
  model.minReplicas === undefined && model.maxReplicas === undefined ? '—'
    : model.minReplicas === model.maxReplicas ? String(model.minReplicas) : `${model.minReplicas ?? 1}–${model.maxReplicas ?? '∞'}`

const columns: Column<Model>[] = [
  { key: 'name', header: 'Model', render: (m) => <NameCell name={m.name} namespace={m.namespace} />, sortValue: (m) => m.name },
  {
    key: 'state', header: 'Durum', sortValue: (m) => m.state,
    render: (m) => <div className="stack"><StatusBadge value={m.state} title={m.message} />{m.state !== 'Ready' && m.reason ? <small className="muted">{m.reason}</small> : null}</div>,
  },
  { key: 'runtime', header: 'Runtime', render: (m) => m.runtime || m.kind, sortValue: (m) => m.runtime || m.kind },
  { key: 'format', header: 'Model', render: (m) => <div className="stack"><span>{m.modelFormat || '—'}</span><small className="muted truncate" title={m.storageUri}>{m.storageUri}</small></div>, secondary: true },
  { key: 'replicas', header: 'Replika', render: replicas, sortValue: (m) => m.minReplicas ?? 1, secondary: true },
  { key: 'gpu', header: 'GPU', render: (m) => m.gpu || '—', sortValue: (m) => m.gpu },
  { key: 'age', header: 'Yaş', render: (m) => formatRelative(m.createdAt), sortValue: (m) => m.createdAt ?? '', secondary: true },
]

function ModelDetail({ model }: { model: Model }) {
  return <DetailList items={[
    ['Kaynak türü', model.kind],
    ['Runtime', model.runtime],
    ['Runtime image', model.image && <code>{model.image}</code>],
    ['Model formatı', model.modelFormat],
    ['Storage URI', model.storageUri && <code>{model.storageUri}</code>],
    ['Endpoint', model.url && <a href={model.url} target="_blank" rel="noreferrer"><code>{model.url}</code></a>],
    ['GPU / replika', model.gpu || undefined],
    ['Oluşturulma', formatDateTime(model.createdAt)],
    ['Durum değişimi', model.stateSince && formatDateTime(model.stateSince)],
    ['Mesaj', model.message && <span className="issue">{model.message}</span>],
  ]} />
}

export function Models({ type, models }: { type: ModelType; models: Model[] }) {
  const [filter, setFilter] = useState<Filter>('all')
  const [query, setQuery] = useState('')
  const color = TYPE_SERIES.find((series) => series.key === type)!.color

  const ready = models.filter(FILTERS.Ready).length
  const gpus = models.reduce((sum, model) => sum + allocatedGpu(model), 0)
  const kpis = [
    { key: 'all', label: 'Toplam model', value: models.length },
    { key: 'Ready', label: 'Hazır', value: ready, tone: 'good' as const },
    { key: 'NotReady', label: 'Hazır değil', value: models.length - ready, tone: 'critical' as const },
    { key: 'gpu', label: 'Ayrılan GPU', value: gpus, sub: 'min. replika × GPU' },
  ]

  const healthRows = useMemo(() => groupRows(models, (m) => m.namespace, modelTone, NAMESPACE_ROWS), [models])
  const gpuRows = useMemo(() => groupRows(models, (m) => m.namespace, () => 'gpu', NAMESPACE_ROWS, allocatedGpu), [models])
  const visible = useMemo(() => models.filter((m) => FILTERS[filter](m) && matchesQuery(query, m.name, m.namespace, m.runtime, m.modelFormat)), [models, filter, query])

  return <div className="page">
    <KpiRow items={kpis} active={filter} onSelect={(key) => setFilter(key as Filter)} />
    <div className="grid-2">
      <Panel title="Namespace bazında durum" subtitle="Hazır / sorunlu model sayısı">
        <StackedBars rows={healthRows} series={HEALTH_SERIES.filter((s) => s.key !== 'info')} empty="Model bulunamadı." />
      </Panel>
      <Panel title="GPU dağılımı" subtitle="Namespace başına ayrılan GPU">
        <StackedBars rows={gpuRows} series={[{ key: 'gpu', label: 'GPU', color }]} empty="GPU kullanan model yok." />
      </Panel>
    </div>
    <Panel title="Modeller" subtitle={`${visible.length} / ${models.length} model`}
      actions={<SearchInput value={query} onChange={setQuery} placeholder="Model, namespace, runtime ara" />}>
      <DataTable rows={visible} columns={columns} rowKey={(m) => `${m.kind}/${m.namespace}/${m.name}`} detail={(m) => <ModelDetail model={m} />}
        empty="Bu filtrelerle eşleşen model yok." initialSort={{ key: 'state', direction: 1 }} />
    </Panel>
  </div>
}
