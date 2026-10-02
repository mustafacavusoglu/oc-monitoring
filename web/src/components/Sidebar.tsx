import { formatDateTime } from '../lib/format'
import { statusInfo } from '../lib/status'
import type { Page } from '../lib/useRoute'
import type { Sources } from '../types'
import { Icon, type IconName } from './Icon'

export type NavItem = { page: Page; label: string; icon: IconName; count?: number; alerts?: number }

const SOURCE_LABELS: Record<keyof Sources, string> = {
  models: 'Model kaynakları',
  batch: 'Batch kaynakları',
  projects: 'Proje listesi',
  registry: 'Nexus registry',
}

export function Sidebar({ items, page, sources, open, onNavigate, onClose }: {
  items: NavItem[]; page: Page; sources?: Sources; open: boolean; onNavigate: (page: Page) => void; onClose: () => void
}) {
  return <>
    <aside className={`sidebar ${open ? 'open' : ''}`} aria-label="Ana menü">
      <div className="brand">
        <span className="brand-mark" aria-hidden="true">◆</span>
        <div><strong>MLOps Monitor</strong><small>OpenShift</small></div>
        <button type="button" className="icon-button sidebar-close" aria-label="Menüyü kapat" onClick={onClose}><Icon name="close" /></button>
      </div>
      <nav>
        {items.map((item) => <button key={item.page} type="button" className={`nav-item ${page === item.page ? 'active' : ''}`}
          aria-current={page === item.page ? 'page' : undefined} onClick={() => onNavigate(item.page)}>
          <Icon name={item.icon} />
          <span className="nav-label">{item.label}</span>
          {item.alerts ? <span className="nav-alert" title={`${item.alerts} sorun`}>{item.alerts}</span> : null}
          {item.count !== undefined ? <span className="nav-count">{item.count}</span> : null}
        </button>)}
      </nav>
      {sources ? <div className="sources">
        <h3>Veri kaynakları</h3>
        {(Object.keys(SOURCE_LABELS) as (keyof Sources)[]).map((key) => {
          const source = sources[key]
          const { label, tone } = statusInfo(source.state)
          return <div key={key} className="source" title={source.error || `Son başarılı: ${formatDateTime(source.lastSuccess)}`}>
            <span className={`dot tone-${tone}`} aria-hidden="true" />
            <span>{SOURCE_LABELS[key]}</span>
            <small>{label}</small>
          </div>
        })}
      </div> : null}
    </aside>
    {open ? <div className="scrim" onClick={onClose} aria-hidden="true" /> : null}
  </>
}
