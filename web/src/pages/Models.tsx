import { useMemo, useState } from 'react'
import { FilterBar } from '../components/FilterBar'
import { DataTable, NameCell, type Column } from '../components/DataTable'
import { DetailList } from '../components/DetailList'
import { KpiRow, type KpiItem } from '../components/Kpi'
import { Panel } from '../components/Panel'
import { PodList } from '../components/PodList'
import { SearchInput } from '../components/SearchInput'
import { StatusBadge } from '../components/StatusBadge'
import { StackedBars } from '../components/charts/StackedBars'
import { HEALTH_SERIES, TYPE_SERIES, groupRows, namespacePick, namespaceSelection } from '../lib/chart'
import { ConsoleLink } from '../lib/console'
import { formatDateTime, formatRelative, matchesQuery } from '../lib/format'
import { acceleratorLabel, allocatedGpu, allocatedMig, modelTone, unhealthyPods, usesAccelerator } from '../lib/status'
import type { Navigate } from '../lib/useRoute'
import type { Model, ModelType } from '../types'

const NAMESPACE_ROWS = 8

const HEALTH = HEALTH_SERIES.filter((s) => s.key !== 'info')

/** Filters reachable from the KPI tiles and charts; the key is kept in the URL. */
const FILTERS: Record<string, { label: string; match: (model: Model) => boolean }> = {
  all: { label: 'Tümü', match: () => true },
  Ready: { label: 'Hazır', match: (m) => m.state === 'Ready' },
  NotReady: { label: 'Hazır değil', match: (m) => m.state !== 'Ready' },
  gpu: { label: 'GPU / MIG kullanan', match: usesAccelerator },
  pods: { label: 'Sorunlu pod’u olan', match: (m) => unhealthyPods(m) > 0 },
  ...Object.fromEntries(HEALTH.map((s) => [s.key, { label: s.label, match: (m: Model) => modelTone(m) === s.key }])),
}

const replicas = (model: Model) =>
  model.minReplicas === undefined && model.maxReplicas === undefined ? '—'
    : model.minReplicas === model.maxReplicas ? String(model.minReplicas) : `${model.minReplicas ?? 1}–${model.maxReplicas ?? '∞'}`

const podsColumn: Column<Model> = {
  key: 'pods', header: 'Pod’lar', sortValue: (m) => unhealthyPods(m) * 1000 + (m.pods?.length ?? 0),
  render: (m) => {
    const pods = m.pods ?? []
    const restarts = pods.reduce((sum, pod) => sum + pod.restarts, 0)
    const bad = unhealthyPods(m)
    if (!pods.length) return <span className="muted">—</span>
    return <div className="stack">
      <span className={`badge tone-${bad ? 'critical' : 'good'}`}>{pods.length - bad}/{pods.length} hazır</span>
      {restarts ? <small className="muted">{restarts} restart</small> : null}
    </div>
  },
}

const columns: Column<Model>[] = [
  { key: 'name', header: 'Model', render: (m) => <NameCell name={m.name} namespace={m.namespace} kind={m.kind} />, sortValue: (m) => m.name },
  {
    key: 'state', header: 'Durum', sortValue: (m) => m.state,
    render: (m) => <div className="stack"><StatusBadge value={m.state} title={m.message} />{m.state !== 'Ready' && m.reason ? <small className="muted">{m.reason}</small> : null}</div>,
  },
  {
    key: 'runtime', header: 'Runtime', sortValue: (m) => m.runtime || m.kind,
    render: (m) => !m.runtime ? m.kind : <div className="stack">
      <span>{m.runtime}{m.runtimeMissing ? null : <ConsoleLink kind="ServingRuntime" namespace={m.namespace} name={m.runtime} />}</span>
      {m.runtimeMissing ? <small className="issue">ServingRuntime bulunamadı</small> : null}
    </div>,
  },
  { key: 'format', header: 'Model', render: (m) => <div className="stack"><span>{m.modelFormat || '—'}</span><small className="muted truncate" title={m.storageUri}>{m.storageUri}</small></div>, secondary: true },
  { key: 'replicas', header: 'Replika', render: replicas, sortValue: (m) => m.minReplicas ?? 1, secondary: true },
  { key: 'gpu', header: 'GPU / MIG', render: (m) => acceleratorLabel(m) || '—', sortValue: (m) => m.gpu * 100 + Object.values(m.mig ?? {}).reduce((sum, n) => sum + n, 0) },
  { key: 'age', header: 'Yaş', render: (m) => formatRelative(m.createdAt), sortValue: (m) => m.createdAt ?? '', secondary: true },
]

function ModelDetail({ model }: { model: Model }) {
  const details = <DetailList items={[
    ['Kaynak türü', model.kind],
    ['ServingRuntime', model.runtime && <>{model.runtime}{model.runtimeMissing
      ? <span className="issue"> — namespace’te bulunamadı</span>
      : <ConsoleLink kind="ServingRuntime" namespace={model.namespace} name={model.runtime} />}</>],
    ['Runtime image’ları', model.images?.length ? <div className="stack">{model.images.map((image) => <code key={image}>{image}</code>)}</div> : undefined],
    ['Model formatı', model.modelFormat],
    ['Storage URI', model.storageUri && <code>{model.storageUri}</code>],
    ['Endpoint', model.url && <a href={model.url} target="_blank" rel="noreferrer"><code>{model.url}</code></a>],
    ['GPU / MIG (replika başına)', acceleratorLabel(model)],
    ['Oluşturulma', formatDateTime(model.createdAt)],
    ['Durum değişimi', model.stateSince && formatDateTime(model.stateSince)],
    ['Mesaj', model.message && <span className="issue">{model.message}</span>],
  ]} />
  if (!model.pods) return details
  return <div className="detail-grid detail-grid-2">
    <section><h3>Model</h3>{details}</section>
    <section><h3>Pod’lar ({model.pods.length})</h3><PodList pods={model.pods} namespace={model.namespace} /></section>
  </div>
}

export function Models({ type, models, namespace, filter, navigate }: {
  type: ModelType; models: Model[]; namespace: string; filter: string; navigate: Navigate
}) {
  const [query, setQuery] = useState('')
  const active = FILTERS[filter] ?? FILTERS.all
  const color = TYPE_SERIES.find((series) => series.key === type)!.color

  const ready = models.filter(FILTERS.Ready.match).length
  const gpus = models.reduce((sum, model) => sum + allocatedGpu(model), 0)
  const migSlices = useMemo(() => models.flatMap((m) => allocatedMig(m).map((slice) => ({ ...slice, namespace: m.namespace }))), [models])
  const migTotal = migSlices.reduce((sum, slice) => sum + slice.count, 0)
  const kpis: KpiItem[] = [
    { key: 'all', label: 'Toplam model', value: models.length },
    { key: 'Ready', label: 'Hazır', value: ready, tone: 'good' as const },
    { key: 'NotReady', label: 'Hazır değil', value: models.length - ready, tone: 'critical' as const },
    type === 'custom'
      ? { key: 'pods', label: 'Sorunlu pod’u olan', value: models.filter(FILTERS.pods.match).length, sub: 'Hazır olmayan pod', tone: 'critical' }
      : { key: 'gpu', label: 'Ayrılan GPU', value: gpus, sub: migTotal ? `+ ${migTotal} MIG dilimi` : 'min. replika × GPU' },
  ]

  const healthRows = useMemo(() => groupRows(models, (m) => m.namespace, modelTone, NAMESPACE_ROWS), [models])
  const gpuRows = useMemo(() => groupRows(models, (m) => m.namespace, () => 'gpu', NAMESPACE_ROWS, allocatedGpu), [models])
  const migRows = useMemo(() => groupRows(migSlices, (s) => s.profile, () => 'mig', NAMESPACE_ROWS, (s) => s.count), [migSlices])
  const visible = useMemo(() => models.filter((m) => active.match(m) && matchesQuery(query, m.name, m.namespace, m.runtime, m.modelFormat)), [models, active, query])

  return <div className="page">
    <KpiRow items={kpis} active={filter} onSelect={(key) => navigate({ filter: key })} />
    <div className="grid-2">
      <Panel title="Namespace bazında durum" subtitle="Sağlıklı / sorunlu model sayısı · çubuğa tıklayınca liste filtrelenir">
        <StackedBars rows={healthRows} series={HEALTH} empty="Model bulunamadı."
          selected={namespaceSelection(namespace, filter, HEALTH.map((s) => s.key))}
          onSelect={(pick) => navigate({ ...namespacePick(pick), filter: pick.series ?? 'all' })} />
      </Panel>
      <Panel title="GPU / MIG dağılımı" subtitle="Minimum replikada ayrılan tam GPU ve MIG dilimleri · tıklayınca liste filtrelenir">
        <h3 className="chart-heading">Tam GPU · namespace bazında</h3>
        <StackedBars rows={gpuRows} series={[{ key: 'gpu', label: 'GPU', color }]} empty="Tam GPU kullanan model yok."
          selected={namespaceSelection(namespace, filter, ['gpu'])} onSelect={(pick) => navigate({ ...namespacePick(pick), filter: 'gpu' })} />
        {migRows.length ? <>
          <h3 className="chart-heading">MIG dilimleri · profil bazında</h3>
          <StackedBars rows={migRows} series={[{ key: 'mig', label: 'MIG dilimi', color }]} empty="" onSelect={() => navigate({ filter: 'gpu' })} />
        </> : null}
      </Panel>
    </div>
    <Panel title="Modeller" subtitle="Satırı açarak runtime, image, endpoint ve pod ayrıntılarını görün"
      actions={<SearchInput value={query} onChange={setQuery} placeholder="Model, namespace, runtime ara" />}>
      <FilterBar namespace={namespace} filter={filter} filterLabel={active.label} shown={visible.length} total={models.length} navigate={navigate} />
      <DataTable rows={visible} columns={[...columns.slice(0, 3), podsColumn, ...columns.slice(3)]} rowKey={(m) => `${m.kind}/${m.namespace}/${m.name}`} detail={(m) => <ModelDetail model={m} />}
        empty="Bu filtrelerle eşleşen model yok." initialSort={{ key: 'state', direction: 1 }} />
    </Panel>
  </div>
}
