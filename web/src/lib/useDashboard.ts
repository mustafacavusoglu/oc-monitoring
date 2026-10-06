import { useCallback, useEffect, useRef, useState } from 'react'
import { fetchDashboard } from '../api'
import type { DashboardResponse } from '../types'

// UI pacing only: the regular interval comes from the server (UI_REFRESH_INTERVAL).
const RETRY_MS = 10_000
const FAST_REFRESH_MS = 3_000

/** Data that is still settling (informers syncing, images being checked) is polled faster. */
const isSettling = (data: DashboardResponse) =>
  Object.values(data.sources).some((source) => source.state === 'syncing') ||
  data.cronWorkflows.some((workflow) => workflow.images.some((image) => image.status === 'checking'))

/**
 * Polls /api/dashboard. Polling pauses while the tab is hidden and resumes
 * with an immediate refresh when it becomes visible again.
 */
export function useDashboard() {
  const [data, setData] = useState<DashboardResponse | null>(null)
  const [error, setError] = useState('')
  const [refreshing, setRefreshing] = useState(false)
  const timer = useRef<number | undefined>(undefined)
  const controller = useRef<AbortController | null>(null)
  const missedWhileHidden = useRef(false)
  const loaded = useRef(false)

  const refresh = useCallback(async () => {
    window.clearTimeout(timer.current)
    // The first load always runs; later refreshes wait until the tab is visible.
    if (document.hidden && loaded.current) {
      missedWhileHidden.current = true
      return
    }
    controller.current?.abort()
    const current = new AbortController()
    controller.current = current
    setRefreshing(true)
    let next = RETRY_MS
    try {
      const response = await fetchDashboard(current.signal)
      setData(response)
      loaded.current = true
      setError('')
      next = isSettling(response) ? FAST_REFRESH_MS : response.refreshIntervalSeconds * 1000
    } catch (cause) {
      if (current.signal.aborted) return
      setError(cause instanceof Error ? cause.message : 'Dashboard verisi alınamadı.')
    } finally {
      if (controller.current === current) setRefreshing(false)
    }
    timer.current = window.setTimeout(() => void refresh(), next)
  }, [])

  useEffect(() => {
    void refresh()
    const onVisibility = () => {
      if (!document.hidden && missedWhileHidden.current) {
        missedWhileHidden.current = false
        void refresh()
      }
    }
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      document.removeEventListener('visibilitychange', onVisibility)
      window.clearTimeout(timer.current)
      controller.current?.abort()
    }
  }, [refresh])

  return { data, error, refreshing, refresh }
}
