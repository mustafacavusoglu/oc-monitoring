import { statusInfo, type Tone } from '../lib/status'

const GLYPHS: Record<Tone, string> = { good: '✓', warning: '!', critical: '✕', info: '●', neutral: '–' }

/** Status is always icon + label, never color alone. */
export function StatusBadge({ value, title }: { value?: string; title?: string }) {
  const { label, tone } = statusInfo(value)
  return <span className={`badge tone-${tone}`} title={title}><span aria-hidden="true">{GLYPHS[tone]}</span>{label}</span>
}
