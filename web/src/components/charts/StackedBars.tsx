import { formatNumber } from '../../lib/format'
import { useTip } from '../../lib/useTip'
import { Legend, type Series } from './Legend'

export type BarRow = { key: string; label: string; values: Record<string, number> }

/** A clicked segment or row label; series is undefined for a whole row. */
export type BarPick = { row: string; series?: string }

/**
 * Horizontal stacked bars sharing one scale; a single series drops the legend.
 * With onSelect, segments and row labels filter the page; `selected` keeps the
 * picked part bright and dims the rest.
 */
export function StackedBars({ rows, series, empty, unit = '', onSelect, selected }: {
  rows: BarRow[]; series: Series[]; empty: string; unit?: string
  onSelect?: (pick: BarPick) => void; selected?: Partial<BarPick>
}) {
  const tip = useTip()
  const totals = rows.map((row) => series.reduce((sum, item) => sum + (row.values[item.key] ?? 0), 0))
  const max = Math.max(0, ...totals)
  if (!max) return <p className="chart-empty">{empty}</p>

  return <div className="stacked">
    {series.length > 1 ? <Legend series={series} /> : null}
    <div className="stacked-rows">
      {rows.map((row, index) => <div key={row.key} className="stacked-row">
        {onSelect
          ? <button type="button" className="stacked-label link" title={`${row.label} için filtrele`} onClick={() => onSelect({ row: row.key })}>{row.label}</button>
          : <span className="stacked-label" title={row.label}>{row.label}</span>}
        <div className="stacked-track" style={{ width: `${(totals[index] / max) * 100}%` }}>
          {series.filter((item) => row.values[item.key]).map((item) => {
            const dimmed = selected && ((selected.row !== undefined && selected.row !== row.key) || (selected.series !== undefined && selected.series !== item.key))
            const label = `${row.label} · ${item.label}: ${formatNumber(row.values[item.key])}${unit}`
            return <button key={item.key} type="button" tabIndex={onSelect ? 0 : -1} disabled={!onSelect}
              className={`stacked-segment ${dimmed ? 'dimmed' : ''}`} aria-label={label}
              style={{ flexGrow: row.values[item.key], background: item.color }}
              onClick={() => onSelect?.({ row: row.key, series: item.key })}
              onMouseMove={(event) => tip.show(event, <><strong>{row.label}</strong><span>{item.label}: {formatNumber(row.values[item.key])}{unit}</span>{onSelect ? <small>Filtrelemek için tıklayın</small> : null}</>)}
              onMouseLeave={tip.hide} />
          })}
        </div>
        <span className="stacked-total">{formatNumber(totals[index])}{unit}</span>
      </div>)}
    </div>
    {tip.node}
  </div>
}
