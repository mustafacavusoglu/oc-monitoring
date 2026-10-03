import type { BarRow } from '../components/charts/StackedBars'
import type { Series } from '../components/charts/Legend'

/** Categorical slots 1-4 (adjacent-validated order), fixed per type on every chart. */
export const TYPE_SERIES: Series[] = [
  { key: 'llm', label: 'LLM', color: 'var(--series-llm)' },
  { key: 'ml', label: 'ML', color: 'var(--series-ml)' },
  { key: 'batch', label: 'Batch', color: 'var(--series-batch)' },
  { key: 'custom', label: 'Custom Serve', color: 'var(--series-custom)' },
]

export const HEALTH_SERIES: Series[] = [
  { key: 'good', label: 'Sağlıklı', color: 'var(--good)' },
  { key: 'critical', label: 'Sorunlu', color: 'var(--critical)' },
  { key: 'info', label: 'Devam ediyor', color: 'var(--info)' },
  { key: 'neutral', label: 'Bilinmiyor', color: 'var(--neutral)' },
]

const OTHER = 'Diğer'

/**
 * Groups items into bar rows by `group`, summing `weight` per `bucket`.
 * Rows are sorted by total; past `limit` they fold into one "Diğer" row.
 */
export function groupRows<T>(items: T[], group: (item: T) => string, bucket: (item: T) => string, limit: number, weight: (item: T) => number = () => 1): BarRow[] {
  const groups = new Map<string, Record<string, number>>()
  for (const item of items) {
    const values = groups.get(group(item)) ?? {}
    values[bucket(item)] = (values[bucket(item)] ?? 0) + weight(item)
    groups.set(group(item), values)
  }
  const total = (values: Record<string, number>) => Object.values(values).reduce((sum, n) => sum + n, 0)
  const rows = [...groups].map(([label, values]) => ({ key: label, label, values }))
    .filter((row) => total(row.values) > 0)
    .sort((a, b) => total(b.values) - total(a.values) || a.label.localeCompare(b.label))
  if (rows.length <= limit) return rows
  const other: BarRow = { key: OTHER, label: `${OTHER} (${rows.length - limit + 1})`, values: {} }
  for (const row of rows.slice(limit - 1)) {
    for (const [key, value] of Object.entries(row.values)) other.values[key] = (other.values[key] ?? 0) + value
  }
  return [...rows.slice(0, limit - 1), other]
}
