import { useEffect, useState } from 'react'
import { getStackStats } from '@/lib/api-client'
import type { StackStatsResponse } from '@/lib/api-types'

export function useDashboardStats() {
  const [data, setData] = useState<StackStatsResponse | null>(null)
  const [error, setError] = useState<Error | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    let inFlight = false

    async function poll() {
      if (document.hidden || inFlight) return
      inFlight = true
      try {
        const next = await getStackStats(controller.signal)
        if (controller.signal.aborted) return
        setData(next)
        setError(null)
      } catch (error) {
        if (controller.signal.aborted) return
        setError(error instanceof Error ? error : new Error('Failed to load resource usage'))
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

  return { data, error }
}
