import { formatDateTime, formatDuration } from '../../lib/format'
import { statusInfo } from '../../lib/status'
import { useTip } from '../../lib/useTip'
import type { RunSummary } from '../../types'

const MAX_RUNS = 10

/** Recent runs as a strip of status cells, oldest left, newest right. */
export function RunHistory({ runs }: { runs: RunSummary[] }) {
  const tip = useTip()
  if (!runs.length) return <span className="muted">—</span>
  const recent = runs.slice(0, MAX_RUNS).reverse()
  return <div className="run-history" role="img" aria-label={recent.map((run) => statusInfo(run.phase).label).join(', ')}>
    {recent.map((run) => <span key={run.name} className={`run-cell tone-${statusInfo(run.phase).tone}`}
      onMouseMove={(event) => tip.show(event, <>
        <strong>{run.name}</strong>
        <span>{statusInfo(run.phase).label} · {formatDuration(run.startedAt, run.finishedAt)}</span>
        <span>{formatDateTime(run.startedAt)}</span>
      </>)}
      onMouseLeave={tip.hide} />)}
    {tip.node}
  </div>
}
