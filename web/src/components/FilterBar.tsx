import { ALL_NAMESPACES, NO_FILTER, type Navigate } from '../lib/useRoute'
import { Icon } from './Icon'

/** Active filters above a list, each removable, so a viewer always sees why rows are hidden. */
export function FilterBar({ namespace, filter, filterLabel, shown, total, navigate }: {
  namespace: string; filter: string; filterLabel?: string; shown: number; total: number; navigate: Navigate
}) {
  const chips = [
    namespace !== ALL_NAMESPACES && { key: 'ns', label: `Namespace: ${namespace}`, clear: { namespace: ALL_NAMESPACES } },
    filter !== NO_FILTER && { key: 'f', label: `Durum: ${filterLabel ?? filter}`, clear: { filter: NO_FILTER } },
  ].filter(Boolean) as { key: string; label: string; clear: Parameters<Navigate>[0] }[]
  if (!chips.length) return <p className="filter-hint">{total} kayıt · Grafiklere veya üstteki kartlara tıklayarak listeyi filtreleyebilirsiniz.</p>
  return <div className="filter-bar" role="status">
    <span>{shown} / {total} kayıt gösteriliyor</span>
    {chips.map((chip) => <button key={chip.key} type="button" className="chip" onClick={() => navigate(chip.clear)} aria-label={`${chip.label} filtresini kaldır`}>
      {chip.label}<Icon name="close" size={13} />
    </button>)}
    {chips.length > 1 ? <button type="button" className="chip-clear" onClick={() => navigate({ namespace: ALL_NAMESPACES, filter: NO_FILTER })}>Tümünü temizle</button> : null}
  </div>
}
