import { formatNumber } from '../../lib/format'
import { useTip } from '../../lib/useTip'
import { Legend, type Series } from './Legend'

export type BarRow = { key: string; label: string; values: Record<string, number> }

/** Horizontal stacked bars sharing one scale; a single series drops the legend. */
export function StackedBars({ rows, series, empty, unit = '' }: { rows: BarRow[]; series: Series[]; empty: string; unit?: string }) {
  const tip = useTip()
  const totals = rows.map((row) => series.reduce((sum, item) => sum + (row.values[item.key] ?? 0), 0))
  const max = Math.max(0, ...totals)
  if (!max) return <p className="chart-empty">{empty}</p>

  return <div className="stacked">
    {series.length > 1 ? <Legend series={series} /> : null}
    <div className="stacked-rows">
      {rows.map((row, index) => <div key={row.key} className="stacked-row">
        <span className="stacked-label" title={row.label}>{row.label}</span>
        <div className="stacked-track" style={{ width: `${(totals[index] / max) * 100}%` }}>
          {series.filter((item) => row.values[item.key]).map((item) => <span key={item.key} className="stacked-segment"
            style={{ flexGrow: row.values[item.key], background: item.color }}
            onMouseMove={(event) => tip.show(event, <><strong>{row.label}</strong><span>{item.label}: {formatNumber(row.values[item.key])}{unit}</span></>)}
            onMouseLeave={tip.hide} />)}
        </div>
        <span className="stacked-total">{formatNumber(totals[index])}{unit}</span>
      </div>)}
    </div>
    {tip.node}
  </div>
}
