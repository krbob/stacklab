import { useCallback, useEffect, useRef, useState } from 'react'
import { getHostMetrics, getHostOverview } from '@/lib/api-client'
import type { HostMetricsResponse, HostOverviewResponse } from '@/lib/api-types'
import { latestMetricSampleTimestamp, mergeHostMetrics } from '@/lib/host-metrics'

const OVERVIEW_POLL_INTERVAL_MS = 5_000
const METRICS_POLL_INTERVAL_MS = 1_000

export function useHostObservability() {
  const [overview, setOverview] = useState<HostOverviewResponse | null>(null)
  const [overviewError, setOverviewError] = useState<Error | null>(null)
  const [overviewLoading, setOverviewLoading] = useState(true)
  const [overviewUpdatedAt, setOverviewUpdatedAt] = useState<number | null>(null)
  const [metrics, setMetrics] = useState<HostMetricsResponse | null>(null)
  const [metricsError, setMetricsError] = useState<Error | null>(null)
  const [metricsLoading, setMetricsLoading] = useState(true)
  const [nowMs, setNowMs] = useState(() => Date.now())
  const [pageVisible, setPageVisible] = useState(() => !document.hidden)
  const initialOverviewLoadRef = useRef(true)
  const initialMetricsLoadRef = useRef(true)
  const metricsRef = useRef<HostMetricsResponse | null>(null)
  const overviewInFlightRef = useRef(false)
  const metricsInFlightRef = useRef(false)

  const loadOverview = useCallback(async () => {
    if (overviewInFlightRef.current) return
    overviewInFlightRef.current = true

    if (initialOverviewLoadRef.current) {
      setOverviewLoading(true)
    }

    try {
      const nextOverview = await getHostOverview()
      setOverview(nextOverview)
      setOverviewError(null)
      setOverviewUpdatedAt(Date.now())
    } catch (error) {
      setOverviewError(error instanceof Error ? error : new Error('Failed to load host overview'))
    } finally {
      overviewInFlightRef.current = false
      initialOverviewLoadRef.current = false
      setOverviewLoading(false)
    }
  }, [])

  const loadMetrics = useCallback(async () => {
    if (metricsInFlightRef.current) return
    metricsInFlightRef.current = true

    if (initialMetricsLoadRef.current) {
      setMetricsLoading(true)
    }

    try {
      const since = latestMetricSampleTimestamp(metricsRef.current)
      const nextMetrics = await getHostMetrics(since ? { since } : undefined)
      setMetrics((previous) => {
        const merged = previous && since ? mergeHostMetrics(previous, nextMetrics) : nextMetrics
        metricsRef.current = merged
        return merged
      })
      setMetricsError(null)
    } catch (error) {
      setMetricsError(error instanceof Error ? error : new Error('Failed to load host metrics'))
    } finally {
      metricsInFlightRef.current = false
      initialMetricsLoadRef.current = false
      setMetricsLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadOverview()
    void loadMetrics()
  }, [loadMetrics, loadOverview])

  // Auto-refresh overview metadata.
  useEffect(() => {
    if (!pageVisible) return
    const interval = setInterval(() => {
      void loadOverview()
    }, OVERVIEW_POLL_INTERVAL_MS)
    return () => clearInterval(interval)
  }, [loadOverview, pageVisible])

  // Keep host metrics in dash-like active mode while this page is open.
  useEffect(() => {
    if (!pageVisible) return
    const interval = setInterval(() => {
      void loadMetrics()
    }, METRICS_POLL_INTERVAL_MS)
    return () => clearInterval(interval)
  }, [loadMetrics, pageVisible])

  useEffect(() => {
    function handleWindowFocus() {
      void loadOverview()
      void loadMetrics()
    }

    function handleVisibilityChange() {
      const visible = !document.hidden
      setPageVisible(visible)
      if (visible) {
        void loadOverview()
        void loadMetrics()
      }
    }

    window.addEventListener('focus', handleWindowFocus)
    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => {
      window.removeEventListener('focus', handleWindowFocus)
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  }, [loadMetrics, loadOverview])

  useEffect(() => {
    if (!overview || !pageVisible) return
    const interval = setInterval(() => setNowMs(Date.now()), 1_000)
    return () => clearInterval(interval)
  }, [overview, pageVisible])

  return {
    overview,
    overviewError,
    overviewLoading,
    overviewUpdatedAt,
    metrics,
    metricsError,
    metricsLoading,
    nowMs,
  }
}
