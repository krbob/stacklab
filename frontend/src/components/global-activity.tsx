import { useCallback, useEffect, useRef, useState } from 'react'
import { getActiveJobs, getJob } from '@/lib/api-client'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import type { ActiveJobItem, ActiveJobsResponse, JobDetail } from '@/lib/api-types'
import { cn } from '@/lib/cn'

const POLL_INTERVAL = 3_000
const COMPLETED_LINGER_MS = 5_000

function formatElapsed(startedAt: string): string {
  const seconds = Math.floor((Date.now() - new Date(startedAt).getTime()) / 1000)
  if (seconds < 60) return `${seconds}s`
  const mins = Math.floor(seconds / 60)
  if (mins < 60) return `${mins}m ${seconds % 60}s`
  return `${Math.floor(mins / 60)}h ${mins % 60}m`
}

function jobLabel(job: ActiveJobItem): string {
  const target = job.current_step?.target_stack_id ?? job.stack_id
  const action = job.current_step?.action ?? job.action
  return target ? `${action} · ${target}` : action
}


function toActiveJobItem(job: JobDetail): ActiveJobItem {
  return {
    id: job.id,
    stack_id: job.stack_id,
    action: job.action,
    state: job.state,
    requested_at: job.requested_at,
    started_at: job.started_at,
    workflow: job.workflow,
    current_step: null,
    latest_event: null,
  }
}

export function GlobalActivity() {
  const [response, setResponse] = useState<ActiveJobsResponse | null>(null)
  const [open, setOpen] = useState(false)
  const [recentlyCompleted, setRecentlyCompleted] = useState<ActiveJobItem[]>([])
  const prevIdsRef = useRef<Set<string>>(new Set())
  const popoverRef = useRef<HTMLDivElement>(null)

  const poll = useCallback(async () => {
    try {
      const data = await getActiveJobs()
      setResponse(data)

      // Detect jobs that disappeared (completed)
      const currentIds = new Set(data.items.map((j) => j.id))
      const vanished = Array.from(prevIdsRef.current).filter((id) => !currentIds.has(id))
      prevIdsRef.current = currentIds

      if (vanished.length > 0) {
        const resolvedJobs = await Promise.all(
          vanished.map(async (id) => {
            try {
              const { job } = await getJob(id)
              return toActiveJobItem(job)
            } catch {
              return null
            }
          }),
        )

        const completed = resolvedJobs.filter((job): job is ActiveJobItem => job !== null)
        if (completed.length === 0) return

        setRecentlyCompleted((prev) => {
          const merged = new Map(prev.map((job) => [job.id, job]))
          for (const job of completed) {
            merged.set(job.id, job)
          }
          return Array.from(merged.values())
        })

        const transientIds = completed
          .filter((job) => ['succeeded', 'cancelled'].includes(job.state))
          .map((job) => job.id)
        if (transientIds.length > 0) {
          setTimeout(() => {
            setRecentlyCompleted((prev) => prev.filter((job) => !transientIds.includes(job.id)))
          }, COMPLETED_LINGER_MS)
        }
      }
    } catch {
      // Silently ignore poll failures
    }
  }, [])

  useEffect(() => {
    const initial = setTimeout(poll, 0)
    const interval = setInterval(poll, POLL_INTERVAL)
    return () => { clearTimeout(initial); clearInterval(interval) }
  }, [poll])

  // Close popover on outside click
  useEffect(() => {
    if (!open) return
    function handleClick(e: MouseEvent) {
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [open])

  // Tick elapsed time display
  const [, setTick] = useState(0)
  useEffect(() => {
    if (!response || response.summary.active_count === 0) return
    const interval = setInterval(() => setTick((t) => t + 1), 1000)
    return () => clearInterval(interval)
  }, [response])

  const activeCount = response?.summary.active_count ?? 0
  const activeItems = response?.items ?? []
  const primaryJob = activeItems[0] ?? null
  const failedRecent = recentlyCompleted.find((job) => job.state === 'failed' || job.state === 'timed_out')

  if (activeCount === 0 && recentlyCompleted.length === 0) return null

  return (
    <div className="relative" ref={popoverRef}>
      {/* Collapsed indicator */}
      <button
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-xs transition hover:bg-[rgba(255,255,255,0.05)]"
      >
        <span className={cn(
          'inline-block size-2 rounded-full',
          activeCount > 0 ? 'animate-pulse bg-sky-400' : failedRecent ? 'bg-red-400' : 'bg-emerald-400',
        )} />
        <span className="text-[var(--text)]">
          {activeCount > 0
            ? activeCount === 1
              ? jobLabel(primaryJob!)
              : `${activeCount} running`
            : failedRecent
              ? `Failed · ${jobLabel(failedRecent)}`
              : 'Done'}
        </span>
        {primaryJob?.started_at && (
          <span className="ml-auto text-[var(--muted)]">{formatElapsed(primaryJob.started_at)}</span>
        )}
      </button>

      {/* Popover */}
      {open && (
        <div className="absolute bottom-full left-0 mb-2 w-72 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-3 shadow-lg">
          <div className="mb-2 text-xs font-medium text-[var(--text)]">Activity</div>

          {activeItems.length === 0 && recentlyCompleted.length === 0 && (
            <p className="text-xs text-[var(--muted)]">No active operations.</p>
          )}

          <div className="space-y-1">
            {activeItems.map((job) => (
              <JobRow key={job.id} job={job} />
            ))}
            {recentlyCompleted.map((job) => (
              <JobRow key={job.id} job={job} terminal />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function JobRow({ job, terminal = false }: { job: ActiveJobItem; terminal?: boolean }) {
  const { openJob } = useJobDrawer()
  const target = job.current_step?.target_stack_id ?? job.stack_id
  const action = job.current_step?.action ?? job.action
  const elapsed = job.started_at ? formatElapsed(job.started_at) : '—'
  const isFailure = job.state === 'failed' || job.state === 'timed_out'
  const isSuccess = job.state === 'succeeded' || job.state === 'cancelled'

  return (
    <button
      onClick={() => openJob(job.id)}
      className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-xs transition hover:bg-[rgba(255,255,255,0.05)]"
    >
      <span className={cn(
        'size-1.5 shrink-0 rounded-full',
        job.state === 'running'
          ? 'animate-pulse bg-sky-400'
          : job.state === 'queued' || job.state === 'cancel_requested'
            ? 'bg-amber-400'
            : isFailure
              ? 'bg-red-400'
              : 'bg-emerald-400',
      )} />
      <span className="min-w-0 flex-1 truncate text-left text-[var(--text)]">
        {action}
        {target && <span className="text-[var(--muted)]"> · {target}</span>}
      </span>
      {job.current_step && !terminal && (
        <span className="shrink-0 text-[var(--muted)]">{job.current_step.index}/{job.current_step.total}</span>
      )}
      {terminal && (
        <span className={cn('shrink-0', isFailure ? 'text-red-400' : isSuccess ? 'text-emerald-400' : 'text-[var(--muted)]')}>
          {isFailure ? 'Failed' : isSuccess ? 'Done' : job.state}
        </span>
      )}
      <span className="shrink-0 text-[var(--muted)]">{elapsed}</span>
    </button>
  )
}
