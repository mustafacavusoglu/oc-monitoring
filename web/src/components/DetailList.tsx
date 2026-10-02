import type { ReactNode } from 'react'

/** Label/value pairs for expanded rows; empty values are skipped. */
export function DetailList({ items }: { items: [string, ReactNode][] }) {
  return <dl className="detail-list">
    {items.filter(([, value]) => value !== undefined && value !== null && value !== '').map(([label, value]) =>
      <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}
  </dl>
}
