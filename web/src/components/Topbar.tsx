import { useEffect, useState } from 'react'
import { formatRelative } from '../lib/format'
import { Icon } from './Icon'
import { NamespaceSelect } from './NamespaceSelect'

export type Theme = 'light' | 'dark'

/** Re-renders only itself every few seconds, not the whole dashboard. */
function UpdatedAgo({ at }: { at?: string }) {
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 5_000)
    return () => window.clearInterval(id)
  }, [])
  return <span className="updated">{at ? `Güncellendi ${formatRelative(at, now)}` : 'Yükleniyor…'}</span>
}

export function Topbar({ title, subtitle, namespaces, namespace, onNamespace, generatedAt, refreshing, onRefresh, theme, onTheme, onMenu }: {
  title: string; subtitle: string
  namespaces: string[]; namespace: string; onNamespace: (value: string) => void
  generatedAt?: string; refreshing: boolean; onRefresh: () => void
  theme: Theme; onTheme: () => void; onMenu: () => void
}) {
  return <header className="topbar">
    <button type="button" className="icon-button menu-button" aria-label="Menüyü aç" onClick={onMenu}><Icon name="menu" /></button>
    <div className="topbar-title"><h1>{title}</h1><p>{subtitle}</p></div>
    <div className="topbar-actions">
      <NamespaceSelect namespaces={namespaces} value={namespace} onChange={onNamespace} />
      <UpdatedAgo at={generatedAt} />
      <button type="button" className={`icon-button ${refreshing ? 'spinning' : ''}`} aria-label="Yenile" title="Yenile" onClick={onRefresh} disabled={refreshing}>
        <Icon name="refresh" />
      </button>
      <button type="button" className="icon-button" aria-label={theme === 'light' ? 'Koyu tema' : 'Açık tema'} title="Tema" onClick={onTheme}>
        <Icon name={theme === 'light' ? 'moon' : 'sun'} />
      </button>
    </div>
  </header>
}
