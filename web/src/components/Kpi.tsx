import type { Tone } from '../lib/status'
import { formatNumber } from '../lib/format'

export type KpiItem = { key: string; label: string; value: number; sub?: string; tone?: Tone }

/** A row of stat tiles; clicking one applies its filter (or navigates). */
export function KpiRow({ items, active, onSelect }: { items: KpiItem[]; active?: string; onSelect?: (key: string) => void }) {
  return <div className="kpi-row">
    {items.map((item) => <button key={item.key} type="button" disabled={!onSelect}
      className={`kpi ${item.tone && item.value ? `kpi-${item.tone}` : ''} ${active === item.key ? 'selected' : ''}`}
      aria-pressed={onSelect ? active === item.key : undefined} onClick={() => onSelect?.(item.key)}>
      <span className="kpi-label">{item.label}</span>
      <strong className="kpi-value">{formatNumber(item.value)}</strong>
      {item.sub ? <span className="kpi-sub">{item.sub}</span> : null}
    </button>)}
  </div>
}
