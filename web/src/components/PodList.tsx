import { formatRelative } from '../lib/format'
import type { Pod } from '../types'
import { StatusBadge } from './StatusBadge'

export function PodList({ pods }: { pods: Pod[] }) {
  if (!pods.length) return <p className="muted">Pod bulunamadı.</p>
  return <ul className="item-list">{pods.map((pod) => <li key={pod.name}>
    <div className="stack">
      <strong>{pod.name}</strong>
      <small className="muted">
        {pod.containers ? `${pod.ready}/${pod.containers} hazır · ${pod.restarts} restart` : null}
        {pod.startedAt ? ` · ${formatRelative(pod.startedAt)} başladı` : null}
        {pod.node ? ` · ${pod.node}` : null}
      </small>
      {pod.containerStates?.length ? <small className="muted">{pod.containerStates.join(' · ')}</small> : null}
    </div>
    <StatusBadge value={pod.phase} />
  </li>)}</ul>
}
