import { useEffect, useState } from 'react'
import { CircleStop, X } from 'lucide-react'
import { cancelJob, getJob, getJobEvents } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import { StepCards } from '@/components/step-cards'
import type { JobHistoryEvent } from '@/lib/api-types'
import type { JobEvent } from '@/lib/ws-types'
import { cn } from '@/lib/cn'

const stateColors: Record<string, string> = {
  queued: 'text-stone-500',
  running: 'text-[var(--run)]',
  succeeded: 'text-[var(--ok)]',
  failed: 'text-[var(--danger)]',
  cancel_requested: 'text-[var(--warning)]',
  cancelled: 'text-[var(--muted)]',
  timed_out: 'text-[var(--danger)]',
}

function toJobEvent(e: JobHistoryEvent): JobEvent {
  return {
    job_id: e.job_id,
    stack_id: null,
    action: '',
    state: e.state,
    event: e.event,
    message: e.message ?? '',
    data: e.data,
    step: e.step ? { index: e.step.index, total: e.step.total, action: e.step.action, target_stack_id: e.step.target_stack_id } : null,
    timestamp: e.timestamp,
  }
}

export function JobDetailDrawer() {
  const { jobId } = useJobDrawer()

  if (!jobId) return null

  return <JobDetailDrawerContent key={jobId} jobId={jobId} />
}

function JobDetailDrawerContent({ jobId }: { jobId: string }) {
  const { closeJob } = useJobDrawer()
  const [canceling, setCanceling] = useState(false)
  const [cancelError, setCancelError] = useState<string | null>(null)

  const { data: jobData, error: jobError, loading: jobLoading, refetch: refetchJob } = useApi(
    () => getJob(jobId),
    [jobId],
  )

  const { data: eventsData, error: eventsError, loading: eventsLoading, refetch: refetchEvents } = useApi(
    () => getJobEvents(jobId),
    [jobId],
  )

  // Close on Escape
  useEffect(() => {
    if (!jobId) return
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') closeJob()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [jobId, closeJob])

  const job = jobData?.job ?? null
  const events = eventsData?.items ?? []
  const retained = eventsData?.retained ?? true
  const retentionMessage = eventsData?.message
  const loading = jobLoading || eventsLoading
  const error = jobError || eventsError
  const cancellable = job?.state === 'queued' || job?.state === 'running'
  const terminal = job ? ['succeeded', 'failed', 'cancelled', 'timed_out'].includes(job.state) : true

  useEffect(() => {
    if (terminal) return
    const interval = window.setInterval(() => {
      refetchJob()
      refetchEvents()
    }, 1000)
    return () => window.clearInterval(interval)
  }, [terminal, refetchJob, refetchEvents])

  async function handleCancel() {
    if (!job || canceling) return
    setCanceling(true)
    setCancelError(null)
    try {
      await cancelJob(job.id)
      refetchJob()
    } catch (error) {
      setCancelError(error instanceof Error ? error.message : 'Failed to cancel job')
    } finally {
      setCanceling(false)
    }
  }

  return (
    <>
      {/* Backdrop */}
      <div className="fixed inset-0 z-40 bg-black/40" onClick={closeJob} />

      {/* Drawer — safe-area insets keep the close button clear of the iOS
          status bar and home indicator. */}
      <div
        className="fixed inset-y-0 right-0 z-50 flex w-full max-w-lg flex-col border-l border-[var(--panel-border)] bg-[var(--panel)] shadow-lg"
        style={{
          paddingTop: 'env(safe-area-inset-top)',
          paddingBottom: 'env(safe-area-inset-bottom)',
        }}
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-[var(--panel-border)] px-5 py-4">
          <div>
            <h2 className="text-lg font-medium text-[var(--text)]">Job detail</h2>
            {job && (
              <div className="mt-1 flex items-center gap-2 text-xs text-[var(--muted)]">
                <span className="font-mono">{job.action}</span>
                {job.stack_id && <span>· {job.stack_id}</span>}
                <span className={stateColors[job.state] ?? 'text-[var(--muted)]'}>{job.state}</span>
              </div>
            )}
          </div>
          <div className="flex items-center gap-2">
            {cancellable && (
              <button
                onClick={handleCancel}
                disabled={canceling}
                aria-label="Cancel job"
                className="inline-flex items-center gap-1 rounded-md border border-[var(--panel-border)] px-2 py-1 text-xs text-[var(--warning)] transition hover:border-[var(--warning)]/50 hover:text-[var(--text)] disabled:cursor-not-allowed disabled:opacity-50"
              >
                <CircleStop className="size-3.5" />
                {canceling ? 'Cancelling...' : 'Cancel'}
              </button>
            )}
            <button
              onClick={closeJob}
              aria-label="Close job detail"
              className="flex size-9 items-center justify-center rounded-md text-[var(--muted)] transition hover:bg-[rgba(255,255,255,0.05)] hover:text-[var(--text)]"
            >
              <X className="size-5" />
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto px-5 py-4">
          {loading && (
            <div className="space-y-3">
              <div className="h-6 w-32 animate-pulse rounded bg-[rgba(255,255,255,0.05)]" />
              <div className="h-20 animate-pulse rounded bg-[rgba(255,255,255,0.03)]" />
            </div>
          )}

          {error && (
            <div className="rounded-md border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">
              {error.message}
            </div>
          )}

          {cancelError && (
            <div className="mb-4 rounded-md border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">
              {cancelError}
            </div>
          )}

          {job && !loading && (
            <div className="space-y-4">
              {/* Metadata */}
              <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 font-mono text-xs">
                <span className="text-[var(--muted)]">Job</span>
                <span className="text-[var(--text)]">{job.id}</span>
                <span className="text-[var(--muted)]">Action</span>
                <span className="text-[var(--text)]">{job.action}</span>
                {job.stack_id && (
                  <>
                    <span className="text-[var(--muted)]">Stack</span>
                    <span className="text-[var(--text)]">{job.stack_id}</span>
                  </>
                )}
                <span className="text-[var(--muted)]">State</span>
                <span className={stateColors[job.state] ?? 'text-[var(--text)]'}>{job.state}</span>
                <span className="text-[var(--muted)]">Requested</span>
                <span className="text-[var(--text)]">{new Date(job.requested_at).toLocaleString()}</span>
                {job.started_at && (
                  <>
                    <span className="text-[var(--muted)]">Started</span>
                    <span className="text-[var(--text)]">{new Date(job.started_at).toLocaleString()}</span>
                  </>
                )}
                {job.finished_at && (
                  <>
                    <span className="text-[var(--muted)]">Finished</span>
                    <span className="text-[var(--text)]">{new Date(job.finished_at).toLocaleString()}</span>
                  </>
                )}
              </div>

              {/* Retention notice */}
              {!retained && (
                <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-4 py-3 text-xs text-[var(--muted)]">
                  {retentionMessage ?? 'Detailed output is no longer retained.'}
                </div>
              )}

              {/* Events / step cards */}
              {retained && events.length > 0 && (
                <div>
                  <h3 className="mb-2 text-xs font-medium text-[var(--muted)]">Events</h3>
                  {events.some((e) => e.step) ? (
                    <StepCards events={events.map(toJobEvent)} />
                  ) : (
                    <div className="space-y-0.5 rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.25)] p-3 font-mono text-xs leading-5">
                      {events.map((event) => (
                        <div key={event.sequence} className={cn(
                          event.event === 'job_error' ? 'text-[var(--danger)]' :
                          event.event === 'job_warning' ? 'text-[var(--warning)]' :
                          'text-[var(--muted)]',
                        )}>
                          {event.message}
                          {event.data && <span className="text-[var(--text)]"> {event.data}</span>}
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}

              {retained && events.length === 0 && !eventsLoading && (
                <p className="text-xs text-[var(--muted)]">No events recorded for this job.</p>
              )}
            </div>
          )}
        </div>
      </div>
    </>
  )
}
