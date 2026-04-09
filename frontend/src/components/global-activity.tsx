import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { getActiveJobs } from '@/lib/api-client'
import type { ActiveJobItem, ActiveJobsResponse } from '@/lib/api-types'
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

function jobRoute(job: ActiveJobItem): string {
  if (job.action === 'update_stacks' || job.action === 'prune') return '/maintenance'
  if (job.stack_id) return `/stacks/${job.stack_id}`
  return '/audit'
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
        // We don't have the full job data for completed ones — create synthetic entries
        setRecentlyCompleted((prev) => [
          ...prev,
          ...vanished.map((id) => ({ id, action: 'completed', state: 'succeeded' as const, stack_id: null, requested_at: new Date().toISOString(), started_at: null })),
        ])
        // Auto-remove after linger
        setTimeout(() => {
          setRecentlyCompleted((prev) => prev.filter((j) => !vanished.includes(j.id)))
        }, COMPLETED_LINGER_MS)
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
          activeCount > 0 ? 'animate-pulse bg-sky-400' : 'bg-emerald-400',
        )} />
        <span className="text-[var(--text)]">
          {activeCount > 0
            ? activeCount === 1
              ? jobLabel(primaryJob!)
              : `${activeCount} running`
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
              <div key={job.id} className="flex items-center gap-2 rounded-md px-2 py-1.5 text-xs text-emerald-400">
                <span className="size-1.5 rounded-full bg-emerald-400" />
                <span>Completed</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function JobRow({ job }: { job: ActiveJobItem }) {
  const target = job.current_step?.target_stack_id ?? job.stack_id
  const action = job.current_step?.action ?? job.action
  const elapsed = job.started_at ? formatElapsed(job.started_at) : '—'
  const route = jobRoute(job)

  return (
    <Link
      to={route}
      className="flex items-center gap-2 rounded-md px-2 py-1.5 text-xs transition hover:bg-[rgba(255,255,255,0.05)]"
    >
      <span className={cn(
        'size-1.5 shrink-0 rounded-full',
        job.state === 'running' ? 'animate-pulse bg-sky-400' : 'bg-amber-400',
      )} />
      <span className="min-w-0 flex-1 truncate text-[var(--text)]">
        {action}
        {target && <span className="text-[var(--muted)]"> · {target}</span>}
      </span>
      {job.current_step && (
        <span className="shrink-0 text-[var(--muted)]">{job.current_step.index}/{job.current_step.total}</span>
      )}
      <span className="shrink-0 text-[var(--muted)]">{elapsed}</span>
    </Link>
  )
}
