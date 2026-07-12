import { useCallback, useEffect, useRef, useState } from 'react'
import { getHostMetrics, getHostOverview } from '@/lib/api-client'
import type { HostMetricsResponse, HostOverviewResponse } from '@/lib/api-types'
import { latestMetricSampleTimestamp, mergeHostMetrics } from '@/lib/host-metrics'
import { PageHeader } from '@/components/page-header'
import { HostMetricsDashboard } from '@/pages/host/host-metrics-dashboard'
import { HostOverviewCards } from '@/pages/host/host-overview-cards'
import { StacklabLogs } from '@/pages/host/stacklab-logs'

const OVERVIEW_POLL_INTERVAL_MS = 5_000
const METRICS_POLL_INTERVAL_MS = 1_000

export function HostPage() {
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

  return (
    <div className="flex flex-col gap-4">
      {/* Overview cards */}
      <section aria-busy={overviewLoading || metricsLoading} className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <PageHeader kicker="System" title="Host" />

        {overviewLoading && !overview && (
          <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <span className="sr-only" role="status" aria-live="polite">Loading host data...</span>
            {[1, 2, 3, 4].map((i) => (
              <div key={i} className="h-32 animate-pulse rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />
            ))}
          </div>
        )}

        {overviewError && (
          <div className="mt-4 rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">
            Failed to load host overview: {overviewError.message}
          </div>
        )}

        {overview && (
          <>
            <HostOverviewCards overview={overview} metrics={metrics} nowMs={nowMs} fetchedAtMs={overviewUpdatedAt} />
            <HostMetricsDashboard
              metrics={metrics}
              overview={overview}
              loading={metricsLoading}
              error={metricsError}
            />
          </>
        )}
      </section>

      {/* Stacklab logs */}
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <StacklabLogs />
      </section>
    </div>
  )
}
