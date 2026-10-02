import { formatTime } from '../../lib/format'
import { useTip } from '../../lib/useTip'

export type TimelineItem = { key: string; label: string; detail: string; at: number }

const TICK_HOURS = 4
const MIN_GAP_PERCENT = 2.5

/** Upcoming events on a now → now+hours axis; overlapping dots move to new lanes. */
export function Timeline({ items, now, hours, empty }: { items: TimelineItem[]; now: number; hours: number; empty: string }) {
  const tip = useTip()
  const span = hours * 3_600_000
  const placed = items
    .filter((item) => item.at >= now && item.at <= now + span)
    .sort((a, b) => a.at - b.at)
    .map((item) => ({ ...item, left: ((item.at - now) / span) * 100, lane: 0 }))
  const laneEnds: number[] = []
  for (const item of placed) {
    const lane = laneEnds.findIndex((end) => item.left - end >= MIN_GAP_PERCENT)
    item.lane = lane === -1 ? laneEnds.length : lane
    laneEnds[item.lane] = item.left
  }
  const ticks = Array.from({ length: hours / TICK_HOURS + 1 }, (_, i) => i * TICK_HOURS)

  if (!placed.length) return <p className="chart-empty">{empty}</p>
  return <div className="timeline">
    <div className="timeline-plot" style={{ height: `${Math.max(laneEnds.length, 1) * 16 + 8}px` }}>
      {ticks.map((hour) => <span key={hour} className="timeline-grid" style={{ left: `${(hour / hours) * 100}%` }} />)}
      {placed.map((item) => <button key={item.key} type="button" className="timeline-dot"
        style={{ left: `${item.left}%`, top: `${item.lane * 16 + 4}px` }} aria-label={`${item.label} ${item.detail}`}
        onMouseMove={(event) => tip.show(event, <><strong>{item.label}</strong><span>{item.detail}</span></>)}
        onMouseLeave={tip.hide} />)}
    </div>
    <div className="timeline-axis">
      {ticks.map((hour) => <span key={hour} style={{ left: `${(hour / hours) * 100}%` }}>
        {hour === 0 ? 'Şimdi' : formatTime(new Date(now + hour * 3_600_000))}
      </span>)}
    </div>
    <p className="chart-note">{placed.length} CronWorkflow’un sonraki çalışması</p>
    {tip.node}
  </div>
}
