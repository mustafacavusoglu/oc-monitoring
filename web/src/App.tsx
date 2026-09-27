import { useEffect, useMemo, useState } from 'react'
import { fetchDashboard } from './api'
import type { CronWorkflow, DashboardResponse, ImageResult, SourceHealth } from './types'

type Filter = 'all' | string

function formatDate(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? '—'
    : new Intl.DateTimeFormat('tr-TR', { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

function stateLabel(value?: string) {
  if (!value) return 'Unavailable'
  return value
}

function StatePill({ value }: { value: string }) {
  const normalized = value.toLowerCase().replace(/[^a-z0-9]+/g, '-')
  return <span className={`pill pill-${normalized}`}>{value}</span>
}

function SourceCard({ name, source }: { name: string; source: SourceHealth }) {
  const state = source.state || 'unknown'
  return (
    <div className="source-card">
      <span className={`source-dot source-${state}`} />
      <div>
        <div className="source-name">{name}</div>
        <div className="source-detail">
          {state === 'ready' ? 'Healthy' : state}
          {source.lastSuccess ? ` · updated ${formatDate(source.lastSuccess)}` : ''}
          {source.error ? ` · ${source.error}` : ''}
        </div>
      </div>
    </div>
  )
}

function ImageList({ images }: { images: ImageResult[] }) {
  if (!images.length) return <span className="muted">No image reference</span>
  return (
    <div className="image-list">
      {images.map((image) => (
        <div className="image-line" key={image.reference} title={image.error || image.reference}>
          <code>{image.reference}</code>
          <StatePill value={image.status} />
        </div>
      ))}
    </div>
  )
}

function WorkflowCard({ workflow }: { workflow: CronWorkflow }) {
  const phase = workflow.lastRun?.phase || 'Unavailable'
  const schedule = workflow.schedules.length ? workflow.schedules.join('\n') : 'No schedule'
  const podCount = workflow.lastRun?.pods.length ?? 0
  return (
    <article className="workflow-card">
      <div className="workflow-heading">
        <div>
          <div className="namespace-label">{workflow.namespace}</div>
          <h2>{workflow.name}</h2>
        </div>
        <div className="heading-pills">
          <StatePill value={workflow.suspended ? 'Suspended' : workflow.active ? 'Active' : 'Enabled'} />
          <StatePill value={stateLabel(phase)} />
        </div>
      </div>

      <div className="workflow-grid">
        <section className="detail-block">
          <div className="detail-label">Schedule <span>{workflow.timezone || 'UTC'}</span></div>
          <pre>{schedule}</pre>
          {workflow.scheduleError && <div className="inline-warning">{workflow.scheduleError}</div>}
        </section>
        <section className="detail-block">
          <div className="detail-label">Next scheduled time</div>
          <div className="detail-value">{workflow.suspended ? 'Suspended' : formatDate(workflow.nextScheduledAt)}</div>
        </section>
        <section className="detail-block">
          <div className="detail-label">Last scheduled time</div>
          <div className="detail-value">{formatDate(workflow.lastScheduledAt)}</div>
        </section>
        <section className="detail-block">
          <div className="detail-label">Last run</div>
          {workflow.lastRun ? (
            <>
              <div className="detail-value">{formatDate(workflow.lastRun.scheduledAt || workflow.lastRun.createdAt)}</div>
              <div className="detail-subtle">{workflow.lastRun.name}</div>
            </>
          ) : <div className="detail-value muted">No retained run</div>}
        </section>
      </div>

      <div className="workflow-lower">
        <section className="lower-block">
          <div className="detail-label">Related pods <span>{podCount}</span></div>
          {podCount ? (
            <div className="pod-list">
              {workflow.lastRun?.pods.map((pod) => (
                <div className="pod-line" key={pod.name}>
                  <div><strong>{pod.name}</strong>{pod.containerStates?.length ? <small>{pod.containerStates.join(' · ')}</small> : null}</div>
                  <StatePill value={pod.phase || 'Unknown'} />
                </div>
              ))}
            </div>
          ) : <div className="muted">No pod data for the last run</div>}
        </section>
        <section className="lower-block image-block">
          <div className="detail-label">Image status <span>{workflow.images.length}</span></div>
          <ImageList images={workflow.images} />
        </section>
      </div>
    </article>
  )
}

function matchesStatus(workflow: CronWorkflow, selected: Filter) {
  if (selected === 'all') return true
  return (workflow.lastRun?.phase || 'Unavailable').toLowerCase() === selected.toLowerCase()
}

function matchesImage(workflow: CronWorkflow, selected: Filter) {
  if (selected === 'all') return true
  return workflow.images.some((image) => image.status === selected)
}

export default function App() {
  const [dashboard, setDashboard] = useState<DashboardResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [requestError, setRequestError] = useState('')
  const [namespace, setNamespace] = useState<Filter>('all')
  const [workflowStatus, setWorkflowStatus] = useState<Filter>('all')
  const [imageStatus, setImageStatus] = useState<Filter>('all')

  useEffect(() => {
    let active = true
    let busy = false
    let controller: AbortController | undefined
    const load = async () => {
      if (busy) return
      busy = true
      controller = new AbortController()
      setRefreshing(true)
      try {
        const response = await fetchDashboard(controller.signal)
        if (!active) return
        setDashboard(response)
        setRequestError('')
      } catch (error) {
        if (active && !(error instanceof DOMException && error.name === 'AbortError')) {
          setRequestError(error instanceof Error ? error.message : 'Dashboard verisi alınamadı')
        }
      } finally {
        busy = false
        if (active) {
          setLoading(false)
          setRefreshing(false)
        }
      }
    }
    void load()
    const timer = window.setInterval(() => void load(), 30_000)
    return () => {
      active = false
      window.clearInterval(timer)
      controller?.abort()
    }
  }, [])

  const workflows = dashboard?.cronWorkflows ?? []
  const visibleWorkflows = useMemo(() => workflows.filter((workflow) =>
    (namespace === 'all' || workflow.namespace === namespace)
    && matchesStatus(workflow, workflowStatus)
    && matchesImage(workflow, imageStatus),
  ), [workflows, namespace, workflowStatus, imageStatus])
  const failedCount = workflows.filter((workflow) => ['failed', 'error'].includes((workflow.lastRun?.phase || '').toLowerCase())).length
  const missingImages = workflows.reduce((count, workflow) => count + workflow.images.filter((image) => image.status === 'missing').length, 0)

  return (
    <main className="shell">
      <header className="page-header">
        <div className="topline"><span className="brand-mark">A</span> PLATFORM OPERATIONS <span className="live-indicator"><i /> LIVE</span></div>
        <div className="header-row">
          <div>
            <h1>Workflow Monitor</h1>
            <p>OpenShift üzerinde çalışan BCH CronWorkflow kaynaklarının çalışma ve image sağlığı.</p>
          </div>
          <div className="updated-card">
            <span>{refreshing ? 'Refreshing' : 'Last update'}</span>
            <strong>{dashboard ? formatDate(dashboard.generatedAt) : 'Waiting for data'}</strong>
          </div>
        </div>
      </header>

      {requestError && <div className="banner banner-error">Dashboard verisi alınamadı: {requestError}</div>}

      <section className="source-strip" aria-label="Source health">
        <SourceCard name="Azure project source" source={dashboard?.projectSource ?? { state: 'syncing' }} />
        <SourceCard name="OpenShift cluster" source={dashboard?.clusterSource ?? { state: 'syncing' }} />
        <SourceCard name="Container registry" source={dashboard?.registrySource ?? { state: 'syncing' }} />
      </section>

      <section className="summary-grid" aria-label="Summary">
        <div className="summary-card"><span>CRONWORKFLOWS</span><strong>{workflows.length}</strong><small>Monitored BCH namespaces</small></div>
        <div className="summary-card"><span>ACTIVE RUNS</span><strong>{workflows.filter((workflow) => workflow.active).length}</strong><small>Currently active CronWorkflows</small></div>
        <div className="summary-card"><span>FAILED LAST RUNS</span><strong className={failedCount ? 'value-danger' : ''}>{failedCount}</strong><small>Failed or errored latest runs</small></div>
        <div className="summary-card"><span>MISSING IMAGES</span><strong className={missingImages ? 'value-danger' : ''}>{missingImages}</strong><small>Registry returned not found</small></div>
      </section>

      <section className="list-section">
        <div className="list-header">
          <div><div className="eyebrow">MONITORING</div><h2>CronWorkflows <span>{visibleWorkflows.length}</span></h2></div>
          <div className="filters">
            <label>Namespace
              <select value={namespace} onChange={(event) => setNamespace(event.target.value)}>
                <option value="all">All namespaces</option>
                {dashboard?.namespaces.map((item) => <option value={item} key={item}>{item}</option>)}
              </select>
            </label>
            <label>Workflow status
              <select value={workflowStatus} onChange={(event) => setWorkflowStatus(event.target.value)}>
                <option value="all">All statuses</option>
                <option value="succeeded">Succeeded</option>
                <option value="failed">Failed</option>
                <option value="error">Error</option>
                <option value="running">Running</option>
                <option value="pending">Pending</option>
                <option value="unavailable">Unavailable</option>
              </select>
            </label>
            <label>Image status
              <select value={imageStatus} onChange={(event) => setImageStatus(event.target.value)}>
                <option value="all">All image statuses</option>
                <option value="present">Present</option>
                <option value="missing">Missing</option>
                <option value="unknown">Unknown</option>
              </select>
            </label>
          </div>
        </div>

        {loading ? (
          <div className="empty-state"><div className="spinner" /><strong>Loading workflow state</strong><span>Project ve cluster verisi bekleniyor.</span></div>
        ) : visibleWorkflows.length ? (
          <div className="workflow-list">{visibleWorkflows.map((workflow) => <WorkflowCard workflow={workflow} key={`${workflow.namespace}/${workflow.name}`} />)}</div>
        ) : (
          <div className="empty-state"><strong>{workflows.length ? 'No workflows match these filters' : 'No CronWorkflows found'}</strong><span>{workflows.length ? 'Filtreleri değiştirerek tekrar deneyin.' : 'Azure project kaynağı BCH namespace’lerini bulduğunda workflowlar burada görünür.'}</span></div>
        )}
        <div className="refresh-note">Data refreshes automatically every 30 seconds</div>
      </section>
    </main>
  )
}
