import type { ReactNode } from 'react'

export function Panel({ title, subtitle, actions, children, className = '' }: {
  title: string; subtitle?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string
}) {
  return <section className={`panel ${className}`}>
    <header className="panel-header">
      <div><h2>{title}</h2>{subtitle ? <p>{subtitle}</p> : null}</div>
      {actions ? <div className="panel-actions">{actions}</div> : null}
    </header>
    {children}
  </section>
}
