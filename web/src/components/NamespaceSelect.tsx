import { useEffect, useRef, useState } from 'react'
import { matchesQuery } from '../lib/format'
import { ALL_NAMESPACES } from '../lib/useRoute'
import { Icon } from './Icon'

const label = (value: string) => (value === ALL_NAMESPACES ? 'Tüm namespace’ler' : value)

/** Searchable namespace picker; "all" is always the first option. */
export function NamespaceSelect({ namespaces, value, onChange }: { namespaces: string[]; value: string; onChange: (value: string) => void }) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const root = useRef<HTMLDivElement>(null)
  const options = [ALL_NAMESPACES, ...namespaces].filter((item) => item === ALL_NAMESPACES || matchesQuery(query, item))

  useEffect(() => {
    if (!open) return
    const onPointer = (event: MouseEvent) => {
      if (event.target instanceof Node && !root.current?.contains(event.target)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const choose = (item: string) => {
    onChange(item)
    setOpen(false)
    setQuery('')
  }

  return <div className="ns-select" ref={root}>
    <button type="button" className="ns-trigger" aria-haspopup="listbox" aria-expanded={open} onClick={() => setOpen(!open)}>
      <span className="ns-caption">Namespace</span>
      <span className="ns-value">{label(value)}</span>
      <Icon name="chevron" size={14} />
    </button>
    {open ? <div className="ns-menu">
      <input autoFocus placeholder="Namespace ara…" aria-label="Namespace ara" value={query} onChange={(event) => setQuery(event.target.value)}
        onKeyDown={(event) => { if (event.key === 'Enter' && options.length) choose(options[options.length > 1 ? 1 : 0]) }} />
      <span className="ns-count">{options.length - 1} / {namespaces.length} namespace</span>
      <div className="ns-options" role="listbox" aria-label="Namespace’ler">
        {options.map((item) => <button key={item} type="button" role="option" aria-selected={value === item} onClick={() => choose(item)}>
          {label(item)}
        </button>)}
      </div>
    </div> : null}
  </div>
}
