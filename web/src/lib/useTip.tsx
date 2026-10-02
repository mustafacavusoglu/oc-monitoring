import { useCallback, useState, type MouseEvent, type ReactNode } from 'react'

type TipState = { x: number; y: number; content: ReactNode } | null

/** One floating tooltip per chart, following the pointer. */
export function useTip() {
  const [tip, setTip] = useState<TipState>(null)
  const show = useCallback((event: MouseEvent, content: ReactNode) => setTip({ x: event.clientX, y: event.clientY, content }), [])
  const hide = useCallback(() => setTip(null), [])
  const node = tip ? <div className="tip" role="tooltip" style={{ left: tip.x, top: tip.y }}>{tip.content}</div> : null
  return { show, hide, node }
}
