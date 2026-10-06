import { useCallback, useEffect, useState } from 'react'

export const PAGES = ['overview', 'llm', 'ml', 'custom', 'batch', 'projects'] as const
export type Page = (typeof PAGES)[number]
export type Route = { page: Page; namespace: string; filter: string }

export const ALL_NAMESPACES = 'all'
export const NO_FILTER = 'all'

/** Route lives in the hash (#/batch?ns=foo&f=critical) so filtered views can be shared as links. */
function parse(): Route {
  const [path, query = ''] = window.location.hash.replace(/^#\/?/, '').split('?')
  const params = new URLSearchParams(query)
  const page = (PAGES as readonly string[]).includes(path) ? (path as Page) : 'overview'
  return { page, namespace: params.get('ns') || ALL_NAMESPACES, filter: params.get('f') || NO_FILTER }
}

export type Navigate = (patch: Partial<Route>) => void

export function useRoute(): [Route, Navigate] {
  const [route, setRoute] = useState(parse)
  useEffect(() => {
    const onChange = () => setRoute(parse())
    window.addEventListener('hashchange', onChange)
    return () => window.removeEventListener('hashchange', onChange)
  }, [])
  const navigate = useCallback<Navigate>((patch) => {
    const current = parse()
    // A filter belongs to its page: changing page drops it unless one is given.
    const next = { ...current, filter: patch.page && patch.page !== current.page ? NO_FILTER : current.filter, ...patch }
    const params = new URLSearchParams()
    if (next.namespace !== ALL_NAMESPACES) params.set('ns', next.namespace)
    if (next.filter !== NO_FILTER) params.set('f', next.filter)
    const query = params.toString()
    window.location.hash = `/${next.page}${query ? `?${query}` : ''}`
  }, [])
  return [route, navigate]
}
