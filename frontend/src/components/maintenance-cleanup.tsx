import { useCallback, useEffect, useState } from 'react'
import { getMaintenancePrunePreview, runMaintenancePrune } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { useJobStream } from '@/hooks/use-job-stream'
import type { JobEvent } from '@/lib/ws-types'
import { cn } from '@/lib/cn'

function formatBytes(bytes: number): string {
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

const stepStatusColors: Record<string, string> = {
  running: 'text-sky-400',
  succeeded: 'text-emerald-400',
  failed: 'text-red-400',
  queued: 'text-zinc-500',
}

export function MaintenanceCleanup() {
  const [pruneImages, setPruneImages] = useState(true)
  const [pruneBuildCache, setPruneBuildCache] = useState(true)
  const [pruneStopped, setPruneStopped] = useState(false)
  const [pruneVolumes, setPruneVolumes] = useState(false)

  const [jobId, setJobId] = useState<string | null>(null)
  const [startPending, setStartPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const { data: preview, loading: previewLoading, refetch: refetchPreview } = useApi(
    () => getMaintenancePrunePreview({ images: true, build_cache: true, stopped_containers: true, volumes: true }),
    [],
  )

  const { events, state: jobState } = useJobStream({ jobId })
  const isTerminal = jobState === 'succeeded' || jobState === 'failed' || jobState === 'cancelled' || jobState === 'timed_out'
  const running = startPending || (jobId !== null && !isTerminal)

  const handlePrune = useCallback(async () => {
    setStartPending(true)
    setError(null)
    setJobId(null)
    try {
      const result = await runMaintenancePrune({
        scope: {
          images: pruneImages,
          build_cache: pruneBuildCache,
          stopped_containers: pruneStopped,
          volumes: pruneVolumes,
        },
      })
      setJobId(result.job.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to start cleanup')
    } finally {
      setStartPending(false)
    }
  }, [pruneImages, pruneBuildCache, pruneStopped, pruneVolumes])

  useEffect(() => {
    if (jobId && jobState === 'succeeded') {
      refetchPreview()
    }
  }, [jobId, jobState, refetchPreview])

  const p = preview?.preview

  return (
    <div className="flex flex-col gap-4 lg:flex-row" style={{ minHeight: '400px' }}>
      {/* Left: scope + preview */}
      <div className="w-full shrink-0 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)] lg:flex lg:w-80 lg:flex-col">
        <h3 className="text-lg font-medium text-[var(--text)]">Cleanup</h3>
        <p className="mt-1 text-xs text-[var(--muted)]">Remove unused Docker resources.</p>

        {/* Scope checkboxes */}
        <div className="mt-4 space-y-2">
          <ScopeCheckbox label="Unused images" checked={pruneImages} onChange={setPruneImages} disabled={running} count={p?.images.count} bytes={p?.images.reclaimable_bytes} />
          <ScopeCheckbox label="Build cache" checked={pruneBuildCache} onChange={setPruneBuildCache} disabled={running} count={p?.build_cache.count} bytes={p?.build_cache.reclaimable_bytes} />
          <ScopeCheckbox label="Stopped containers" checked={pruneStopped} onChange={setPruneStopped} disabled={running} count={p?.stopped_containers.count} bytes={p?.stopped_containers.reclaimable_bytes} color="text-amber-400" />
          <ScopeCheckbox label="Unused volumes" checked={pruneVolumes} onChange={setPruneVolumes} disabled={running} count={p?.volumes.count} bytes={p?.volumes.reclaimable_bytes} color="text-red-400" />
        </div>

        {/* Total reclaimable */}
        {p && (
          <div className="mt-4 border-t border-[var(--panel-border)] pt-3 text-xs text-[var(--muted)]">
            Total reclaimable: <span className="font-mono text-[var(--text)]">{formatBytes(p.total_reclaimable_bytes)}</span>
          </div>
        )}

        {previewLoading && <p className="mt-2 text-xs text-[var(--muted)]">Loading preview...</p>}

        {/* Prune button */}
        <button
          data-testid="maintenance-prune"
          onClick={handlePrune}
          disabled={running || (!pruneImages && !pruneBuildCache && !pruneStopped && !pruneVolumes)}
          className="mt-4 w-full rounded-2xl border border-red-400/30 bg-red-400/10 px-4 py-3 text-sm font-medium text-red-400 transition hover:bg-red-400/20 disabled:opacity-40"
        >
          {running ? 'Cleaning...' : 'Run cleanup'}
        </button>

        {error && <p className="mt-2 text-xs text-red-400">{error}</p>}
      </div>

      {/* Right: progress */}
      <div className="flex min-w-0 flex-1 flex-col rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <h4 className="text-sm font-medium text-[var(--text)]">Progress</h4>

        {!jobId && (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-sm text-[var(--muted)]">Select scope and run cleanup.</p>
          </div>
        )}

        {jobId && (
          <div className="mt-3 flex flex-col gap-3">
            <div className="flex items-center gap-2">
              {jobState === 'running' && <span className="inline-block size-2 animate-pulse rounded-full bg-sky-400" />}
              <span className={cn('text-sm font-medium', stepStatusColors[jobState ?? ''] ?? 'text-[var(--muted)]')}>
                {jobState === 'running' ? 'Running' : jobState === 'succeeded' ? 'Succeeded' : jobState === 'failed' ? 'Failed' : jobState ?? 'Starting'}
              </span>
            </div>

            <div className="space-y-1">
              {events.filter((e) => e.event === 'job_step_started' || e.event === 'job_step_finished').map((event, i) => (
                <PruneStepRow key={i} event={event} />
              ))}
            </div>

            <div className="max-h-48 overflow-y-auto rounded-[12px] border border-[var(--panel-border)] bg-[rgba(0,0,0,0.3)] p-3 font-mono text-xs leading-5">
              {events.filter((e) => e.event !== 'job_step_started' && e.event !== 'job_step_finished').map((event, i) => (
                <div key={i} className={cn(event.event === 'job_error' ? 'text-red-400' : 'text-[var(--muted)]')}>
                  {event.message}
                  {event.data && <span className="text-[var(--text)]"> {event.data}</span>}
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

function ScopeCheckbox({ label, checked, onChange, disabled, count, bytes, color }: {
  label: string
  checked: boolean
  onChange: (v: boolean) => void
  disabled: boolean
  count?: number
  bytes?: number
  color?: string
}) {
  return (
    <label className={cn('flex cursor-pointer items-center gap-2 text-xs', color ?? 'text-[var(--text)]')}>
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} disabled={disabled} className="rounded" />
      <span>{label}</span>
      {count != null && bytes != null && (
        <span className="ml-auto text-[var(--muted)]">{count} · {formatBytes(bytes)}</span>
      )}
    </label>
  )
}

function PruneStepRow({ event }: { event: JobEvent }) {
  const step = event.step
  if (!step) return null
  const isFinished = event.event === 'job_step_finished'
  const state = isFinished ? 'succeeded' : 'running'
  return (
    <div className="flex items-center gap-2 text-xs">
      <span className={cn('w-3 shrink-0 font-mono font-bold', stepStatusColors[state])}>
        {state === 'succeeded' ? '✓' : '▶'}
      </span>
      <span className="text-[var(--text)]">{step.action.replace(/_/g, ' ')}</span>
    </div>
  )
}
