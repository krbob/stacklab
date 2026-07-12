import { getMeta } from '@/lib/api-client'
import { useActivity } from '@/hooks/use-activity'
import { useApi } from '@/hooks/use-api'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import type { ActiveJobItem } from '@/lib/api-types'
import { cn } from '@/lib/cn'

function jobChipLabel(job: ActiveJobItem): string {
  const step = job.current_step
  if (step) {
    const target = step.target_stack_id ? ` · ${step.target_stack_id}` : ''
    return `${step.action}${target} · ${step.index}/${step.total}`
  }
  return job.stack_id ? `${job.action} · ${job.stack_id}` : job.action
}

// k9s-style status strip: static host facts plus a live background-job chip
// fed by the activity stream (Z5 — the system state is always one glance away).
export function HostStrip() {
  const { data: meta, error: metaError, loading: metaLoading, refetch: refetchMeta } = useApi(() => getMeta(), [])
  const activity = useActivity()
  const { openJob } = useJobDrawer()

  const activeJobs = activity.response?.items ?? []
  const primary = activeJobs[0] ?? null
  const degraded = activity.freshness === 'stale' || activity.freshness === 'unavailable'

  return (
    <div className="hidden items-center gap-4 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] px-4 py-2 font-mono text-xs text-[var(--muted)] shadow-[var(--shadow)] lg:flex">
      {meta ? (
        <>
          <span className="text-[var(--accent)]">stacklab {meta.app.version}</span>
          <span>docker {meta.docker.engine_version}</span>
          <span>compose {meta.docker.compose_version}</span>
        </>
      ) : metaError ? (
        <span className="flex min-w-0 items-center gap-2" role="alert">
          <span className="max-w-72 truncate text-[var(--danger)]" title={metaError.message}>
            Host metadata unavailable: {metaError.message}
          </span>
          <button
            type="button"
            aria-label="Retry host metadata"
            onClick={refetchMeta}
            className="shrink-0 rounded border border-[var(--danger)]/30 px-2 py-0.5 text-[var(--danger)] hover:bg-[var(--danger)]/10"
          >
            Retry
          </button>
        </span>
      ) : (
        <span role="status" aria-live="polite">
          {metaLoading ? 'host metadata loading' : 'host metadata unavailable'}
        </span>
      )}
      <span className="ml-auto flex items-center gap-2">
        <span
          aria-hidden="true"
          className={cn(
            'inline-block size-1.5 rounded-full',
            degraded
              ? 'bg-[var(--warning)]'
              : primary
                ? 'animate-pulse bg-[var(--run)]'
                : 'bg-[var(--ok)]',
          )}
        />
        {primary ? (
          <button
            onClick={() => openJob(primary.id)}
            className="text-[var(--accent)] hover:underline"
          >
            {activeJobs.length > 1 && `${activeJobs.length} jobs · `}
            {jobChipLabel(primary)}
            {degraded && ' · stale'}
          </button>
        ) : degraded ? (
          <span className="text-[var(--warning)]">
            {activity.freshness === 'stale' ? 'activity stale · retrying' : 'activity unavailable · retrying'}
          </span>
        ) : activity.freshness === 'loading' ? (
          <span>activity loading</span>
        ) : (
          <span>idle</span>
        )}
      </span>
    </div>
  )
}
