import { formatNumber } from '../../lib/format'
import { useTip } from '../../lib/useTip'
import type { Series } from './Legend'

export type Slice = Series & { value: number }

const RADIUS = 46
const CIRCUMFERENCE = 2 * Math.PI * RADIUS
const GAP = 2

/** Part-to-whole for a handful of categories, with a direct-labelled legend. */
export function Donut({ slices, centerLabel }: { slices: Slice[]; centerLabel: string }) {
  const tip = useTip()
  const total = slices.reduce((sum, slice) => sum + slice.value, 0)
  const visible = slices.filter((slice) => slice.value > 0)
  let offset = 0

  return <div className="donut">
    <svg viewBox="0 0 120 120" role="img" aria-label={`${centerLabel}: ${visible.map((s) => `${s.label} ${s.value}`).join(', ')}`}>
      <circle cx="60" cy="60" r={RADIUS} className="donut-track" />
      {visible.map((slice) => {
        const length = (slice.value / total) * CIRCUMFERENCE
        const dash = visible.length > 1 ? Math.max(length - GAP, 0.5) : length
        const circle = <circle key={slice.key} cx="60" cy="60" r={RADIUS} stroke={slice.color}
          strokeDasharray={`${dash} ${CIRCUMFERENCE - dash}`} strokeDashoffset={-offset} className="donut-slice"
          onMouseMove={(event) => tip.show(event, <><strong>{slice.label}</strong><span>{slice.value} · %{Math.round((slice.value / total) * 100)}</span></>)}
          onMouseLeave={tip.hide} />
        offset += length
        return circle
      })}
      <text x="60" y="58" className="donut-value">{formatNumber(total)}</text>
      <text x="60" y="74" className="donut-label">{centerLabel}</text>
    </svg>
    <ul className="donut-legend">
      {slices.map((slice) => <li key={slice.key}>
        <span className="swatch" style={{ background: slice.color }} />
        <span>{slice.label}</span>
        <strong>{formatNumber(slice.value)}</strong>
      </li>)}
    </ul>
    {tip.node}
  </div>
}
