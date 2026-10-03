import { useCallback, useEffect, useState } from 'react'

export const PAGES = ['overview', 'llm', 'ml', 'custom', 'batch', 'projects'] as const
export type Page = (typeof PAGES)[number]
export type Route = { page: Page; namespace: string }

export const ALL_NAMESPACES = 'all'

/** Route lives in the hash (#/batch?ns=foo) so filtered views can be shared as links. */
function parse(): Route {
  const [path, query = ''] = window.location.hash.replace(/^#\/?/, '').split('?')
  const page = (PAGES as readonly string[]).includes(path) ? (path as Page) : 'overview'
  return { page, namespace: new URLSearchParams(query).get('ns') || ALL_NAMESPACES }
}

export function useRoute(): [Route, (patch: Partial<Route>) => void] {
  const [route, setRoute] = useState(parse)
  useEffect(() => {
    const onChange = () => setRoute(parse())
    window.addEventListener('hashchange', onChange)
    return () => window.removeEventListener('hashchange', onChange)
  }, [])
  const navigate = useCallback((patch: Partial<Route>) => {
    const next = { ...parse(), ...patch }
    const query = next.namespace === ALL_NAMESPACES ? '' : `?ns=${encodeURIComponent(next.namespace)}`
    window.location.hash = `/${next.page}${query}`
  }, [])
  return [route, navigate]
}
