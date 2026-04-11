import { useEffect } from 'react'
import { X } from 'lucide-react'
import { getJob, getJobEvents } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import { StepCards } from '@/components/step-cards'
import type { JobHistoryEvent } from '@/lib/api-types'
import type { JobEvent } from '@/lib/ws-types'
import { cn } from '@/lib/cn'

const stateColors: Record<string, string> = {
  queued: 'text-zinc-500',
  running: 'text-sky-400',
  succeeded: 'text-emerald-400',
  failed: 'text-red-400',
  cancel_requested: 'text-amber-400',
  cancelled: 'text-[var(--muted)]',
  timed_out: 'text-red-400',
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

  const { data: jobData, error: jobError, loading: jobLoading } = useApi(
    () => getJob(jobId),
    [jobId],
  )

  const { data: eventsData, error: eventsError, loading: eventsLoading } = useApi(
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

  return (
    <>
      {/* Backdrop */}
      <div className="fixed inset-0 z-40 bg-black/40" onClick={closeJob} />

      {/* Drawer */}
      <div className="fixed inset-y-0 right-0 z-50 flex w-full max-w-lg flex-col border-l border-[var(--panel-border)] bg-[var(--panel)] shadow-lg">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-[var(--panel-border)] px-5 py-4">
          <div>
            <h3 className="text-lg font-medium text-[var(--text)]">Job detail</h3>
            {job && (
              <div className="mt-1 flex items-center gap-2 text-xs text-[var(--muted)]">
                <span className="font-mono">{job.action}</span>
                {job.stack_id && <span>· {job.stack_id}</span>}
                <span className={stateColors[job.state] ?? 'text-[var(--muted)]'}>{job.state}</span>
              </div>
            )}
          </div>
          <button onClick={closeJob} className="rounded-md p-1 text-[var(--muted)] transition hover:bg-[rgba(255,255,255,0.05)] hover:text-[var(--text)]">
            <X className="size-5" />
          </button>
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
            <div className="rounded-md border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">
              {error.message}
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
                  <h4 className="mb-2 text-xs font-medium text-[var(--muted)]">Events</h4>
                  {events.some((e) => e.step) ? (
                    <StepCards events={events.map(toJobEvent)} />
                  ) : (
                    <div className="space-y-0.5 rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.25)] p-3 font-mono text-xs leading-5">
                      {events.map((event) => (
                        <div key={event.sequence} className={cn(
                          event.event === 'job_error' ? 'text-red-400' :
                          event.event === 'job_warning' ? 'text-amber-400' :
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
