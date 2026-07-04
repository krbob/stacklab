import { useEffect, useRef } from 'react'
import { useJobStream } from '@/hooks/use-job-stream'
import type { JobEvent } from '@/lib/ws-types'
import { cn } from '@/lib/cn'

interface ProgressPanelProps {
  jobId: string | null
  /** Pre-subscribed stream from the owner component. The backend keys job
      subscriptions by stream_id, so two subscribers to the same job would
      tear each other down — the owner subscribes once and shares. */
  stream?: { events: JobEvent[]; state: string | null }
  onDone?: (state: string) => void
  onClose?: () => void
}

const stateLabels: Record<string, { label: string; color: string }> = {
  queued: { label: 'Queued', color: 'text-[var(--muted)]' },
  running: { label: 'Running', color: 'text-[var(--run)]' },
  succeeded: { label: 'Succeeded', color: 'text-[var(--ok)]' },
  failed: { label: 'Failed', color: 'text-[var(--danger)]' },
  cancel_requested: { label: 'Cancelling', color: 'text-[var(--warning)]' },
  cancelled: { label: 'Cancelled', color: 'text-[var(--muted)]' },
  timed_out: { label: 'Timed out', color: 'text-[var(--danger)]' },
}

const eventIcons: Record<string, string> = {
  job_started: '▶',
  job_step_started: '→',
  job_step_finished: '✓',
  job_progress: '·',
  job_log: ' ',
  job_warning: '⚠',
  job_error: '✗',
  job_finished: '■',
}

export function ProgressPanel({ jobId, stream, onDone, onClose }: ProgressPanelProps) {
  const internal = useJobStream({ jobId: stream ? null : jobId })
  const events = stream ? stream.events : internal.events
  const state = stream ? stream.state : internal.state
  const scrollRef = useRef<HTMLDivElement>(null)
  const prevStateRef = useRef<string | null>(null)

  // Auto-scroll
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [events])

  // Notify parent when job finishes
  useEffect(() => {
    if (!state || state === prevStateRef.current) return
    prevStateRef.current = state

    const terminal = ['succeeded', 'failed', 'cancelled', 'timed_out']
    if (terminal.includes(state)) {
      onDone?.(state)
    }
  }, [state, onDone])

  if (!jobId) return null

  const stateInfo = state ? stateLabels[state] : null
  const hasWarning = events.some((e) => e.event === 'job_warning')
  const latestProgress = [...events].reverse().find((e) => e.progress)?.progress ?? null
  const visibleEvents = events.filter((e) => !(e.event === 'job_progress' && e.progress))

  return (
    <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(0,0,0,0.2)] p-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          {state === 'running' && (
            <span className="inline-block size-2 animate-pulse rounded-full bg-[var(--run)]" />
          )}
          <span className={cn('text-sm font-medium', stateInfo?.color ?? 'text-[var(--muted)]')}>
            {stateInfo?.label ?? 'Pending'}
          </span>
          {hasWarning && state === 'succeeded' && (
            <span className="text-xs text-[var(--warning)]">with warnings</span>
          )}
        </div>

        <div className="flex items-center gap-3">
          {events.length > 0 && (
            <span className="text-xs text-[var(--muted)]">
              {events[0].action}
              {events[0].step && ` · step ${events[0].step.index}/${events[0].step.total}`}
            </span>
          )}
          {onClose && (
            <button
              onClick={onClose}
              aria-label="Close output"
              className="rounded px-1.5 text-xs text-[var(--muted)] transition hover:text-[var(--text)]"
            >
              ✕
            </button>
          )}
        </div>
      </div>

      {/* Structured progress meter (latest wins) */}
      {latestProgress && state === 'running' && latestProgress.total > 0 && (
        <div className="mt-3">
          <div className="flex items-center gap-2 font-mono text-[11px] tabular-nums text-[var(--muted)]">
            <span className="shrink-0">
              {latestProgress.completed}/{latestProgress.total} {latestProgress.unit}
            </span>
            <span className="h-1 flex-1 overflow-hidden rounded-full bg-[rgba(255,255,255,0.07)]">
              <span
                className="block h-full bg-[var(--accent)] transition-[width] duration-300"
                style={{ width: `${Math.min(100, Math.round((latestProgress.completed / latestProgress.total) * 100))}%` }}
              />
            </span>
          </div>
          {latestProgress.detail && (
            <div className="mt-1 truncate font-mono text-[10px] text-[var(--muted)] opacity-70">{latestProgress.detail}</div>
          )}
        </div>
      )}

      {/* Event stream */}
      <div
        ref={scrollRef}
        className="mt-3 max-h-64 overflow-y-auto font-mono text-xs leading-5"
      >
        {visibleEvents.map((event, i) => (
          <div
            key={i}
            className={cn(
              'flex gap-2',
              event.event === 'job_warning' && 'text-[var(--warning)]',
              event.event === 'job_error' && 'text-[var(--danger)]',
              event.event !== 'job_warning' && event.event !== 'job_error' && 'text-[var(--muted)]',
            )}
          >
            <span className="shrink-0 w-3 text-center">{eventIcons[event.event] ?? '·'}</span>
            <span className="whitespace-pre-wrap break-all">
              {event.message}
              {event.data && (
                <span className="ml-1 text-[var(--text)]">{event.data}</span>
              )}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}
