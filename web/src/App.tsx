import { useCallback, useEffect, useMemo, useState } from 'react'
import { fetchDashboard } from './api'
import type { CronWorkflow, DashboardResponse, ImageResult, Pod, SourceHealth } from './types'

type Theme = 'light' | 'dark'

function formatDate(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('tr-TR', { dateStyle: 'short', timeStyle: 'short' }).format(date)
}

function label(value?: string) {
  const normalized = (value || 'Unavailable').toLowerCase()
  const labels: Record<string, string> = {
    succeeded: 'Başarılı', failed: 'Başarısız', error: 'Hata', running: 'Çalışıyor', pending: 'Bekliyor',
    unavailable: 'Veri yok', present: 'Mevcut', missing: 'Bulunamadı', unknown: 'Bilinmiyor',
    suspended: 'Askıda', active: 'Aktif', ready: 'Hazır', syncing: 'Senkronize ediliyor', degraded: 'Sorun var',
  }
  return labels[normalized] || value || 'Veri yok'
}

function StatePill({ value }: { value?: string }) {
  const key = (value || 'unavailable').toLowerCase().replace(/[^a-z0-9]+/g, '-')
  return <span className={`pill pill-${key}`}>{label(value)}</span>
}

function SourceStatus({ name, source }: { name: string; source: SourceHealth }) {
  return <span className="source-status" title={source.error || `Son başarılı güncelleme: ${formatDate(source.lastSuccess)}`}>
    <span className={`source-dot source-${source.state.toLowerCase()}`} />{name}: {label(source.state)}
  </span>
}

function PodDetails({ pod }: { pod: Pod }) {
  return <div className="pod-row">
    <div><strong>{pod.name}</strong>{pod.containerStates?.length ? <small>{pod.containerStates.join(' · ')}</small> : null}</div>
    <StatePill value={pod.phase} />
  </div>
}

function ImageDetails({ image }: { image: ImageResult }) {
  return <div className="image-row">
    <code title={image.reference}>{image.reference}</code>
    <div><StatePill value={image.status} />{image.error ? <small className="detail-error">{image.error}</small> : null}</div>
  </div>
}

function WorkflowDetails({ workflow }: { workflow: CronWorkflow }) {
  return <div className="details-grid">
    <section className="detail-section">
      <h3>Zamanlama</h3>
      <dl>
        <div><dt>Schedule</dt><dd>{workflow.schedules.length ? workflow.schedules.join(' · ') : '—'}</dd></div>
        <div><dt>Saat dilimi</dt><dd>{workflow.timezone || '—'}</dd></div>
        <div><dt>Son tetiklenme</dt><dd>{formatDate(workflow.lastScheduledAt)}</dd></div>
        {workflow.scheduleError ? <div><dt>Zamanlama hatası</dt><dd className="detail-error">{workflow.scheduleError}</dd></div> : null}
      </dl>
    </section>
    <section className="detail-section">
      <h3>Son Workflow</h3>
      {workflow.lastRun ? <>
        <dl>
          <div><dt>Ad</dt><dd>{workflow.lastRun.name}</dd></div>
          <div><dt>Başlangıç</dt><dd>{formatDate(workflow.lastRun.startedAt || workflow.lastRun.createdAt)}</dd></div>
          <div><dt>Bitiş</dt><dd>{formatDate(workflow.lastRun.finishedAt)}</dd></div>
        </dl>
        <h4>Pod’lar ({workflow.lastRun.pods.length})</h4>
        {workflow.lastRun.pods.length ? workflow.lastRun.pods.map((pod) => <PodDetails key={pod.name} pod={pod} />) : <p className="muted">Pod bulunamadı.</p>}
      </> : <p className="muted">Henüz workflow çalışmamış.</p>}
    </section>
    <section className="detail-section detail-images">
      <h3>Image’lar ({workflow.images.length})</h3>
      {workflow.images.length ? workflow.images.map((image) => <ImageDetails key={image.reference} image={image} />) : <p className="muted">Image bilgisi bulunamadı.</p>}
    </section>
  </div>
}

function matchesRun(workflow: CronWorkflow, filter: string) {
  if (filter === 'all') return true
  const phase = (workflow.lastRun?.phase || 'unavailable').toLowerCase()
  if (filter === 'failed') return phase === 'failed' || phase === 'error'
  if (filter === 'running') return workflow.active || phase === 'running'
  return phase === filter
}

export default function App() {
  const [dashboard, setDashboard] = useState<DashboardResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState('')
  const [namespace, setNamespace] = useState('all')
  const [runFilter, setRunFilter] = useState('all')
  const [imageFilter, setImageFilter] = useState('all')
  const [expanded, setExpanded] = useState<string | null>(null)
  const [theme, setTheme] = useState<Theme>(() => {
    try { return localStorage.getItem('cron-dashboard-theme') === 'dark' ? 'dark' : 'light' } catch { return 'light' }
  })

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    try { localStorage.setItem('cron-dashboard-theme', theme) } catch { /* Storage may be disabled. */ }
  }, [theme])

  const refresh = useCallback(async () => {
    setRefreshing(true)
    const controller = new AbortController()
    try {
      const data = await fetchDashboard(controller.signal)
      setDashboard(data)
      setError('')
    } catch (cause) {
      if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(cause instanceof Error ? cause.message : 'Dashboard verisi alınamadı.')
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    fetchDashboard(controller.signal).then((data) => {
      if (active) { setDashboard(data); setError('') }
    }).catch((cause: unknown) => {
      if (active && !(cause instanceof DOMException && cause.name === 'AbortError')) setError(cause instanceof Error ? cause.message : 'Dashboard verisi alınamadı.')
    }).finally(() => { if (active) { setLoading(false); setRefreshing(false) } })
    const interval = window.setInterval(() => { void refresh() }, 30_000)
    return () => { active = false; controller.abort(); window.clearInterval(interval) }
  }, [refresh])

  const workflows = dashboard?.cronWorkflows || []
  const namespaces = dashboard?.namespaces || []
  const summary = useMemo(() => ({
    total: workflows.length,
    running: workflows.filter((item) => item.active || item.lastRun?.phase.toLowerCase() === 'running').length,
    failed: workflows.filter((item) => ['failed', 'error'].includes(item.lastRun?.phase.toLowerCase() || '')).length,
    missing: workflows.filter((item) => item.images.some((image) => image.status.toLowerCase() === 'missing')).length,
  }), [workflows])
  const visibleWorkflows = useMemo(() => workflows.filter((item) =>
    (namespace === 'all' || item.namespace === namespace) && matchesRun(item, runFilter) &&
    (imageFilter === 'all' || item.images.some((image) => image.status.toLowerCase() === imageFilter))),
  [workflows, namespace, runFilter, imageFilter])

  const applySummaryFilter = (filter: 'all' | 'running' | 'failed' | 'missing') => {
    setNamespace('all')
    setRunFilter(filter === 'missing' ? 'all' : filter)
    setImageFilter(filter === 'missing' ? 'missing' : 'all')
    setExpanded(null)
  }

  return <main className="app-shell">
    <header className="page-header">
      <div><p className="eyebrow">OPENSHIFT · ARGO WORKFLOWS</p><h1>CronWorkflow izleme</h1></div>
      <div className="header-actions">
        <span className="updated-at">{dashboard ? `Güncelleme: ${formatDate(dashboard.generatedAt)}` : 'Canlı durum'}</span>
        <button className="button button-secondary theme-toggle" onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')} aria-label={theme === 'light' ? 'Koyu temaya geç' : 'Açık temaya geç'}>
          {theme === 'light' ? '☾ Koyu' : '☀ Açık'}
        </button>
        <button className="button button-secondary refresh-button" onClick={() => void refresh()} disabled={refreshing}>{refreshing ? 'Yenileniyor…' : 'Yenile'}</button>
      </div>
    </header>

    {dashboard ? <div className="source-line" aria-label="Veri kaynakları">
      <SourceStatus name="Projeler" source={dashboard.projectSource} />
      <SourceStatus name="OpenShift" source={dashboard.clusterSource} />
      <SourceStatus name="Registry" source={dashboard.registrySource} />
    </div> : null}

    {error ? <div className="notice notice-error" role="alert">Veri yenilenemedi: {error}{dashboard ? ' · Önceki veriler gösteriliyor.' : ''}</div> : null}
    {loading && !dashboard ? <div className="empty-state">CronWorkflow verileri yükleniyor…</div> : null}

    {dashboard ? <>
      <section className="summary-grid" aria-label="Özet">
        <button className={`summary-card ${runFilter === 'all' && imageFilter === 'all' ? 'selected' : ''}`} onClick={() => applySummaryFilter('all')}>
          <span>CronWorkflow</span><strong>{summary.total}</strong>
        </button>
        <button className={`summary-card ${runFilter === 'running' ? 'selected' : ''}`} onClick={() => applySummaryFilter('running')}>
          <span>Çalışan</span><strong>{summary.running}</strong>
        </button>
        <button className={`summary-card ${runFilter === 'failed' ? 'selected' : ''}`} onClick={() => applySummaryFilter('failed')}>
          <span>Başarısız son çalışma</span><strong>{summary.failed}</strong>
        </button>
        <button className={`summary-card ${imageFilter === 'missing' ? 'selected' : ''}`} onClick={() => applySummaryFilter('missing')}>
          <span>Eksik image</span><strong>{summary.missing}</strong>
        </button>
      </section>

      <section className="workflows-section">
        <div className="section-heading"><div><h2>CronWorkflow’lar</h2><span>{visibleWorkflows.length} kayıt</span></div>
          <div className="filters">
            <label>Namespace <select value={namespace} onChange={(event) => setNamespace(event.target.value)}>
              <option value="all">Tüm namespace’ler</option>{namespaces.map((item) => <option key={item} value={item}>{item}</option>)}
            </select></label>
            <label>Son çalışma <select value={runFilter} onChange={(event) => setRunFilter(event.target.value)}>
              <option value="all">Tümü</option><option value="running">Çalışıyor</option><option value="succeeded">Başarılı</option><option value="failed">Başarısız / hata</option><option value="pending">Bekliyor</option><option value="unavailable">Veri yok</option>
            </select></label>
            <label>Image <select value={imageFilter} onChange={(event) => setImageFilter(event.target.value)}>
              <option value="all">Tümü</option><option value="present">Mevcut</option><option value="missing">Bulunamadı</option><option value="unknown">Bilinmiyor</option>
            </select></label>
          </div>
        </div>
        <div className="table-wrap"><table>
          <thead><tr><th>CronWorkflow</th><th>Durum</th><th>Son çalışma</th><th>Son çalışma zamanı</th><th>Sonraki zamanlama</th><th></th></tr></thead>
          <tbody>{visibleWorkflows.map((workflow) => {
            const key = `${workflow.namespace}/${workflow.name}`
            const isExpanded = expanded === key
            const phase = workflow.lastRun?.phase || (workflow.active ? 'Running' : 'Unavailable')
            return <FragmentRow key={key} workflow={workflow} phase={phase} isExpanded={isExpanded} onToggle={() => setExpanded(isExpanded ? null : key)} />
          })}
          {!visibleWorkflows.length ? <tr><td className="no-results" colSpan={6}>Bu filtrelerle eşleşen CronWorkflow yok.</td></tr> : null}</tbody>
        </table></div>
      </section>
    </> : null}
    <footer className="page-footer">30 saniyede bir otomatik güncellenir{refreshing ? ' · Güncelleniyor' : ''}</footer>
  </main>
}

function FragmentRow({ workflow, phase, isExpanded, onToggle }: { workflow: CronWorkflow; phase: string; isExpanded: boolean; onToggle: () => void }) {
  return <>
    <tr className={isExpanded ? 'workflow-row expanded' : 'workflow-row'}>
      <td><strong>{workflow.name}</strong><small>{workflow.namespace}</small></td>
      <td><div className="row-status"><StatePill value={phase} />{workflow.suspended ? <span className="suspended-label">Askıda</span> : null}</div></td>
      <td>{workflow.lastRun?.name || '—'}</td>
      <td>{formatDate(workflow.lastRun?.startedAt || workflow.lastRun?.createdAt || workflow.lastScheduledAt)}</td>
      <td>{formatDate(workflow.nextScheduledAt)}</td>
      <td><button className="button button-link" aria-expanded={isExpanded} onClick={onToggle}>{isExpanded ? 'Kapat' : 'Detay'}</button></td>
    </tr>
    {isExpanded ? <tr className="details-row"><td colSpan={6}><WorkflowDetails workflow={workflow} /></td></tr> : null}
  </>
}
