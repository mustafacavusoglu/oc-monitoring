import type { DashboardResponse } from './types'

export async function fetchDashboard(signal?: AbortSignal): Promise<DashboardResponse> {
  const response = await fetch('/api/dashboard', {
    headers: { Accept: 'application/json' },
    signal,
  })
  if (!response.ok) throw new Error(`Dashboard API HTTP ${response.status}`)
  return response.json() as Promise<DashboardResponse>
}
