import { useEffect, useState } from 'react'
import { getStackStats } from '@/lib/api-client'
import type { StackStatsResponse } from '@/lib/api-types'
import { updateDashboardHistory, type DashboardHistory } from '@/lib/dashboard-history'

export function useDashboardStats() {
  const [state, setState] = useState<{
    data: StackStatsResponse | null
    error: Error | null
    history: DashboardHistory
    nowMs: number
  }>(() => ({ data: null, error: null, history: {}, nowMs: Date.now() }))

  useEffect(() => {
    const controller = new AbortController()
    let inFlight = false

    async function poll() {
      if (document.hidden || inFlight) return
      inFlight = true
      try {
        const next = await getStackStats(controller.signal)
        if (controller.signal.aborted) return
        const nowMs = Date.now()
        setState((previous) => ({
          data: next,
          error: null,
          history: updateDashboardHistory(previous.history, next, nowMs),
          nowMs,
        }))
      } catch (error) {
        if (controller.signal.aborted) return
        const nowMs = Date.now()
        setState((previous) => ({
          ...previous,
          error: error instanceof Error ? error : new Error('Failed to load resource usage'),
          history: updateDashboardHistory(previous.history, null, nowMs),
          nowMs,
        }))
      } finally {
        inFlight = false
      }
    }

    void poll()
    const interval = window.setInterval(() => { void poll() }, 1_000)
    document.addEventListener('visibilitychange', poll)
    return () => {
      controller.abort()
      window.clearInterval(interval)
      document.removeEventListener('visibilitychange', poll)
    }
  }, [])

  return state
}
