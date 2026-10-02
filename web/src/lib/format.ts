const LOCALE = 'tr-TR'
const dateTime = new Intl.DateTimeFormat(LOCALE, { dateStyle: 'short', timeStyle: 'short' })
const hourMinute = new Intl.DateTimeFormat(LOCALE, { hour: '2-digit', minute: '2-digit' })
const relative = new Intl.RelativeTimeFormat(LOCALE, { numeric: 'auto', style: 'short' })

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ['day', 86_400_000], ['hour', 3_600_000], ['minute', 60_000], ['second', 1_000],
]

const toDate = (value?: string) => {
  if (!value) return undefined
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? undefined : date
}

export const formatDateTime = (value?: string) => {
  const date = toDate(value)
  return date ? dateTime.format(date) : '—'
}

export const formatTime = (date: Date) => hourMinute.format(date)

/** "3 dk. önce" / "2 sa. sonra" relative to now. */
export function formatRelative(value?: string, now = Date.now()) {
  const date = toDate(value)
  if (!date) return '—'
  const diff = date.getTime() - now
  const [unit, size] = UNITS.find(([, ms]) => Math.abs(diff) >= ms) ?? UNITS[UNITS.length - 1]
  return relative.format(Math.round(diff / size), unit)
}

/** Compact duration such as "1 sa 4 dk" or "42 sn". */
export function formatDuration(start?: string, end?: string, now = Date.now()) {
  const from = toDate(start)
  if (!from) return '—'
  let seconds = Math.max(0, Math.round(((toDate(end)?.getTime() ?? now) - from.getTime()) / 1000))
  const parts: string[] = []
  for (const [label, size] of [['g', 86_400], ['sa', 3_600], ['dk', 60], ['sn', 1]] as const) {
    if (seconds >= size || (label === 'sn' && !parts.length)) {
      parts.push(`${Math.floor(seconds / size)} ${label}`)
      seconds %= size
    }
    if (parts.length === 2) break
  }
  return parts.join(' ')
}

export const formatNumber = (value: number) => value.toLocaleString(LOCALE)

export function matchesQuery(query: string, ...fields: (string | undefined)[]) {
  const needle = query.trim().toLocaleLowerCase(LOCALE)
  return !needle || fields.some((field) => field?.toLocaleLowerCase(LOCALE).includes(needle))
}
