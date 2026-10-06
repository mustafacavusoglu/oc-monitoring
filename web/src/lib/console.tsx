import { createContext, useContext } from 'react'
import { Icon } from '../components/Icon'
import type { Console } from '../types'

export const ConsoleContext = createContext<Console>({ url: '', refs: {} })

/** Kinds without a CRD ref: a pod page and a project (namespace) page. */
type CoreKind = 'Pod' | 'Project'

/** OpenShift console URL of a resource, or undefined when it cannot be linked. */
export function consoleHref(console: Console, kind: string, namespace: string, name = ''): string | undefined {
  if (!console.url) return undefined
  const ns = encodeURIComponent(namespace)
  if (kind === 'Project') return `${console.url}/k8s/cluster/projects/${ns}`
  const ref = kind === 'Pod' ? 'pods' : console.refs[kind]
  return ref && name ? `${console.url}/k8s/ns/${ns}/${ref}/${encodeURIComponent(name)}` : undefined
}

/** A small "open in the OpenShift console" link that opens a new tab. */
export function ConsoleLink({ kind, namespace, name }: { kind: string | CoreKind; namespace: string; name?: string }) {
  const href = consoleHref(useContext(ConsoleContext), kind, namespace, name)
  if (!href) return null
  return <a className="console-link" href={href} target="_blank" rel="noreferrer" title={`${kind} kaynağını OpenShift console’da yeni sekmede aç`}
    aria-label={`${name ?? namespace} — OpenShift console’da aç`} onClick={(event) => event.stopPropagation()}>
    <Icon name="external" size={13} />
  </a>
}
