import { Fragment, useMemo, useState, type ReactNode } from 'react'
import { ConsoleLink } from '../lib/console'
import { Icon } from './Icon'

export type Column<T> = {
  key: string
  header: string
  render: (row: T) => ReactNode
  /** Enables sorting by this column. */
  sortValue?: (row: T) => string | number
  /** Hidden on narrow screens to keep the table readable. */
  secondary?: boolean
}

type Sort = { key: string; direction: 1 | -1 }

export function DataTable<T>({ rows, columns, rowKey, detail, empty, initialSort }: {
  rows: T[]
  columns: Column<T>[]
  rowKey: (row: T) => string
  /** Optional expandable detail for a row. */
  detail?: (row: T) => ReactNode
  empty: string
  initialSort?: Sort
}) {
  const [sort, setSort] = useState<Sort | undefined>(initialSort)
  const [expanded, setExpanded] = useState<string | null>(null)

  const sorted = useMemo(() => {
    const value = columns.find((column) => column.key === sort?.key)?.sortValue
    if (!sort || !value) return rows
    return [...rows].sort((a, b) => {
      const left = value(a)
      const right = value(b)
      const order = typeof left === 'number' && typeof right === 'number'
        ? left - right
        : String(left).localeCompare(String(right), 'tr-TR')
      return order * sort.direction
    })
  }, [rows, columns, sort])

  const toggleSort = (key: string) => setSort((current) =>
    current?.key === key ? { key, direction: current.direction === 1 ? -1 : 1 } : { key, direction: 1 })
  const colSpan = columns.length + (detail ? 1 : 0)

  return <div className="table-wrap">
    <table>
      <thead><tr>
        {columns.map((column) => <th key={column.key} className={column.secondary ? 'secondary' : undefined}
          aria-sort={sort?.key === column.key ? (sort.direction === 1 ? 'ascending' : 'descending') : undefined}>
          {column.sortValue
            ? <button type="button" className="sort" onClick={() => toggleSort(column.key)}>
                {column.header}<span aria-hidden="true">{sort?.key === column.key ? (sort.direction === 1 ? '↑' : '↓') : ''}</span>
              </button>
            : column.header}
        </th>)}
        {detail ? <th><span className="visually-hidden">Detay</span></th> : null}
      </tr></thead>
      <tbody>
        {sorted.map((row) => {
          const key = rowKey(row)
          const open = expanded === key
          return <Fragment key={key}>
            <tr className={open ? 'expanded' : undefined}>
              {columns.map((column) => <td key={column.key} className={column.secondary ? 'secondary' : undefined}>{column.render(row)}</td>)}
              {detail ? <td className="row-toggle">
                <button type="button" className="icon-button" aria-expanded={open} aria-label={open ? 'Detayı kapat' : 'Detayı aç'}
                  onClick={() => setExpanded(open ? null : key)}><Icon name="chevron" size={16} /></button>
              </td> : null}
            </tr>
            {open && detail ? <tr className="detail-row"><td colSpan={colSpan}>{detail(row)}</td></tr> : null}
          </Fragment>
        })}
        {!sorted.length ? <tr><td className="empty" colSpan={colSpan}>{empty}</td></tr> : null}
      </tbody>
    </table>
  </div>
}

/** Primary cell: bold name with the namespace beneath; with kind, a console link. */
export function NameCell({ name, namespace, kind }: { name: string; namespace: string; kind?: string }) {
  return <div className="name-cell">
    <strong>{name}{kind ? <ConsoleLink kind={kind} namespace={namespace} name={kind === 'Project' ? undefined : name} /> : null}</strong>
    <small>{namespace}</small>
  </div>
}
