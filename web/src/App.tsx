import { useEffect, useMemo, useState } from 'react'
import { SOURCE_LABELS, Sidebar, type NavItem } from './components/Sidebar'
import { Topbar, type Theme } from './components/Topbar'
import { batchIssues, modelIssues } from './lib/status'
import type { Sources } from './types'
import { useDashboard } from './lib/useDashboard'
import { ALL_NAMESPACES, useRoute, type Page } from './lib/useRoute'
import { Batch } from './pages/Batch'
import { Models } from './pages/Models'
import { Overview } from './pages/Overview'

const THEME_KEY = 'mlops-dashboard-theme'

const PAGE_TEXT: Record<Page, { title: string; subtitle: string }> = {
  overview: { title: 'Genel bakış', subtitle: 'Tüm model ve batch kaynaklarının özeti' },
  llm: { title: 'LLM modelleri', subtitle: 'LLMInferenceService ve vLLM runtime kullanan InferenceService’ler' },
  ml: { title: 'ML modelleri', subtitle: 'Triton runtime kullanan InferenceService’ler' },
  batch: { title: 'Batch modelleri', subtitle: 'Argo CronWorkflow’lar, son çalışmalar ve Nexus image kontrolü' },
}

function initialTheme(): Theme {
  try {
    const stored = localStorage.getItem(THEME_KEY)
    if (stored === 'light' || stored === 'dark') return stored
  } catch { /* storage may be unavailable */ }
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export default function App() {
  const { data, error, refreshing, refresh } = useDashboard()
  const [{ page, namespace }, navigate] = useRoute()
  const [theme, setTheme] = useState(initialTheme)
  const [menuOpen, setMenuOpen] = useState(false)

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    try { localStorage.setItem(THEME_KEY, theme) } catch { /* storage may be unavailable */ }
  }, [theme])

  const { models, cronWorkflows } = useMemo(() => {
    const inScope = <T extends { namespace: string }>(items: T[] = []) =>
      namespace === ALL_NAMESPACES ? items : items.filter((item) => item.namespace === namespace)
    return { models: inScope(data?.models), cronWorkflows: inScope(data?.cronWorkflows) }
  }, [data, namespace])

  const navItems = useMemo<NavItem[]>(() => {
    const llm = models.filter((m) => m.type === 'llm')
    const ml = models.filter((m) => m.type === 'ml')
    const problems = (list: typeof models) => list.filter((m) => modelIssues(m).length).length
    const batchProblems = cronWorkflows.filter((w) => batchIssues(w).length).length
    return [
      { page: 'overview', label: 'Genel bakış', icon: 'overview', alerts: problems(models) + batchProblems },
      { page: 'llm', label: 'LLM modelleri', icon: 'llm', count: llm.length, alerts: problems(llm) },
      { page: 'ml', label: 'ML modelleri', icon: 'ml', count: ml.length, alerts: problems(ml) },
      { page: 'batch', label: 'Batch modelleri', icon: 'batch', count: cronWorkflows.length, alerts: batchProblems },
    ]
  }, [models, cronWorkflows])

  const goTo = (next: Page) => {
    navigate({ page: next })
    setMenuOpen(false)
  }

  return <div className="layout">
    <Sidebar items={navItems} page={page} sources={data?.sources} open={menuOpen} onNavigate={goTo} onClose={() => setMenuOpen(false)} />
    <div className="main">
      <Topbar {...PAGE_TEXT[page]} namespaces={data?.namespaces ?? []} namespace={namespace} onNamespace={(value) => navigate({ namespace: value })}
        generatedAt={data?.generatedAt} refreshing={refreshing} onRefresh={() => void refresh()}
        theme={theme} onTheme={() => setTheme(theme === 'light' ? 'dark' : 'light')} onMenu={() => setMenuOpen(true)} />
      <main className="content">
        {data ? (Object.keys(SOURCE_LABELS) as (keyof Sources)[]).filter((key) => data.sources[key].state === 'degraded').map((key) =>
          <div key={key} className="notice notice-warning" role="status"><strong>{SOURCE_LABELS[key]}:</strong> {data.sources[key].error}</div>) : null}
        {error ? <div className="notice" role="alert">Veri yenilenemedi: {error}{data ? ' · Son alınan veriler gösteriliyor.' : ''}</div> : null}
        {!data ? (error ? null : <div className="loading">Veriler yükleniyor…</div>)
          : page === 'overview' ? <Overview models={models} cronWorkflows={cronWorkflows} onNavigate={goTo} />
          : page === 'batch' ? <Batch cronWorkflows={cronWorkflows} now={Date.parse(data.generatedAt)} />
          : <Models key={page} type={page} models={models.filter((m) => m.type === page)} />}
      </main>
    </div>
  </div>
}
