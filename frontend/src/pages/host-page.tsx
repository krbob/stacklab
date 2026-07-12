import { PageHeader } from '@/components/page-header'
import { AsyncState } from '@/components/async-state'
import { SystemHealthCenter } from '@/components/system-health-center'
import { HostMetricsDashboard } from '@/pages/host/host-metrics-dashboard'
import { HostOverviewCards } from '@/pages/host/host-overview-cards'
import { StacklabLogs } from '@/pages/host/stacklab-logs'
import { useHostObservability } from '@/pages/host/use-host-observability'

export function HostPage() {
  const {
    overview,
    overviewError,
    overviewLoading,
    overviewUpdatedAt,
    metrics,
    metricsError,
    metricsLoading,
    nowMs,
    retryOverview,
    retryMetrics,
  } = useHostObservability()
  const overviewLoadError = overviewError
    ? new Error(`Failed to load host overview: ${overviewError.message}`)
    : null

  return (
    <div className="flex flex-col gap-4">
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <PageHeader kicker="System" title="Host" />
      </section>

      <SystemHealthCenter />

      {/* Overview cards */}
      <section aria-label="Host overview" aria-busy={overviewLoading || metricsLoading} className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <div className="[&>div:first-child]:mt-0">
          <AsyncState
            loading={overviewLoading}
            error={overviewLoadError}
            hasData={overview !== null}
            isEmpty={false}
            loadingLabel="Loading host overview."
            emptyMessage="Host overview unavailable."
            onRetry={retryOverview}
            retryLabel="Retry host overview"
            loadingFallback={(
              <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                {[1, 2, 3, 4].map((i) => (
                  <div key={i} className="h-32 animate-pulse rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />
                ))}
              </div>
            )}
          >
            {overview && (
              <>
                <HostOverviewCards overview={overview} metrics={metrics} nowMs={nowMs} fetchedAtMs={overviewUpdatedAt} />
                <HostMetricsDashboard
                  metrics={metrics}
                  overview={overview}
                  loading={metricsLoading}
                  error={metricsError}
                  onRetry={retryMetrics}
                />
              </>
            )}
          </AsyncState>
        </div>
      </section>

      {/* Stacklab logs */}
      <section id="stacklab-logs" className="scroll-mt-4 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <StacklabLogs />
      </section>
    </div>
  )
}
