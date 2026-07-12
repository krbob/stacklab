import { PageHeader } from '@/components/page-header'
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
  } = useHostObservability()

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
