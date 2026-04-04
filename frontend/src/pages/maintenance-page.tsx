import { useCallback, useMemo, useState } from 'react'
import { getStacks, updateStacksMaintenance } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { useJobStream } from '@/hooks/use-job-stream'
import { MaintenanceImages } from '@/components/maintenance-images'
import { MaintenanceCleanup } from '@/components/maintenance-cleanup'
import type { StackListItem } from '@/lib/api-types'
import type { JobEvent } from '@/lib/ws-types'
import { cn } from '@/lib/cn'

type MaintenanceTab = 'update' | 'images' | 'cleanup'
type TargetMode = 'all' | 'selected'

const stepStatusColors: Record<string, string> = {
  running: 'text-sky-400',
  succeeded: 'text-emerald-400',
  failed: 'text-red-400',
  queued: 'text-zinc-500',
}

export function MaintenancePage() {
  const { data: stacksData } = useApi(() => getStacks(), [])

  const [targetMode, setTargetMode] = useState<TargetMode>('all')
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [pullImages, setPullImages] = useState(true)
  const [buildImages, setBuildImages] = useState(true)
  const [removeOrphans, setRemoveOrphans] = useState(true)
  const [pruneAfter, setPruneAfter] = useState(false)
  const [pruneVolumes, setPruneVolumes] = useState(false)

  const [jobId, setJobId] = useState<string | null>(null)
  const [startPending, setStartPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const { events, state: jobState } = useJobStream({ jobId })

  const stacks = useMemo(() => stacksData?.items ?? [], [stacksData])
  const isTerminal = jobState === 'succeeded' || jobState === 'failed' || jobState === 'cancelled' || jobState === 'timed_out'
  const running = startPending || (jobId !== null && !isTerminal)
  const canStart = targetMode === 'all' ? stacks.length > 0 : selectedIds.size > 0

  const toggleStack = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const selectAll = useCallback(() => {
    setSelectedIds(new Set(stacks.map((s) => s.id)))
  }, [stacks])

  const deselectAll = useCallback(() => {
    setSelectedIds(new Set())
  }, [])

  const handleStart = useCallback(async () => {
    setStartPending(true)
    setError(null)
    setJobId(null)
    try {
      const result = await updateStacksMaintenance({
        target: {
          mode: targetMode,
          stack_ids: targetMode === 'selected' ? Array.from(selectedIds) : undefined,
        },
        options: {
          pull_images: pullImages,
          build_images: buildImages,
          remove_orphans: removeOrphans,
          prune_after: {
            enabled: pruneAfter,
            include_volumes: pruneAfter ? pruneVolumes : false,
          },
        },
      })
      setJobId(result.job.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to start maintenance')
    } finally {
      setStartPending(false)
    }
  }, [targetMode, selectedIds, pullImages, buildImages, removeOrphans, pruneAfter, pruneVolumes])

  const [activeTab, setActiveTab] = useState<MaintenanceTab>('update')

  return (
    <div className="flex flex-col gap-4" style={{ minHeight: 'calc(100vh - 120px)' }}>
      {/* Tab bar */}
      <div className="flex items-center gap-2">
        <h2 className="text-2xl font-semibold tracking-[-0.04em] text-[var(--text)]">Maintenance</h2>
        <div className="ml-4 flex gap-1">
          {([['update', 'Update'], ['images', 'Images'], ['cleanup', 'Cleanup']] as const).map(([key, label]) => (
            <button
              key={key}
              onClick={() => setActiveTab(key)}
              className={cn('rounded-full border px-3 py-1.5 text-xs transition', activeTab === key ? 'border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      <div className={activeTab === 'images' ? '' : 'hidden'}>
        <MaintenanceImages />
      </div>

      <div className={activeTab === 'cleanup' ? '' : 'hidden'}>
        <MaintenanceCleanup />
      </div>

      <div className={activeTab === 'update' ? '' : 'hidden'}>
    <div className="flex flex-col gap-4 lg:flex-row">
      {/* Left: workflow setup */}
      <div className="w-full shrink-0 rounded-[28px] border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)] lg:flex lg:w-80 lg:flex-col">
        <h3 className="text-lg font-medium text-[var(--text)]">Update stacks</h3>
        <p className="mt-2 text-xs text-[var(--muted)]">Pull images, build, and restart selected stacks.</p>

        {/* Target mode */}
        <div className="mt-5 space-y-2">
          <label className="flex cursor-pointer items-center gap-2 text-sm text-[var(--text)]">
            <input type="radio" checked={targetMode === 'all'} onChange={() => setTargetMode('all')} disabled={running} className="accent-[var(--accent)]" />
            All stacks ({stacks.length})
          </label>
          <label className="flex cursor-pointer items-center gap-2 text-sm text-[var(--text)]">
            <input type="radio" checked={targetMode === 'selected'} onChange={() => setTargetMode('selected')} disabled={running} className="accent-[var(--accent)]" />
            Selected stacks
          </label>
        </div>

        {/* Stack checklist */}
        {targetMode === 'selected' && (
          <div className="mt-3">
            <div className="mb-2 flex gap-2">
              <button onClick={selectAll} disabled={running} className="text-xs text-[var(--accent)] hover:underline disabled:opacity-40">Select all</button>
              <button onClick={deselectAll} disabled={running} className="text-xs text-[var(--muted)] hover:underline disabled:opacity-40">Deselect all</button>
            </div>
            <div className="max-h-48 space-y-1 overflow-y-auto">
              {stacks.map((stack) => (
                <StackCheckbox key={stack.id} stack={stack} checked={selectedIds.has(stack.id)} onChange={() => toggleStack(stack.id)} disabled={running} />
              ))}
            </div>
          </div>
        )}

        {/* Options */}
        <div className="mt-5 space-y-2 border-t border-[var(--panel-border)] pt-4">
          <label className="flex items-center gap-2 text-xs text-[var(--text)]">
            <input type="checkbox" checked={pullImages} onChange={(e) => setPullImages(e.target.checked)} disabled={running} className="rounded" />
            Pull images
          </label>
          <label className="flex items-center gap-2 text-xs text-[var(--text)]">
            <input type="checkbox" checked={buildImages} onChange={(e) => setBuildImages(e.target.checked)} disabled={running} className="rounded" />
            Build images
          </label>
          <label className="flex items-center gap-2 text-xs text-[var(--text)]">
            <input type="checkbox" checked={removeOrphans} onChange={(e) => setRemoveOrphans(e.target.checked)} disabled={running} className="rounded" />
            Remove orphan containers
          </label>
          <label className="flex items-center gap-2 text-xs text-amber-400">
            <input
              type="checkbox"
              checked={pruneAfter}
              onChange={(e) => {
                const checked = e.target.checked
                setPruneAfter(checked)
                if (!checked) setPruneVolumes(false)
              }}
              disabled={running}
              className="rounded"
            />
            Run prune after update
          </label>
          {pruneAfter && (
            <label className="ml-5 flex items-center gap-2 text-xs text-red-400">
              <input type="checkbox" checked={pruneVolumes} onChange={(e) => setPruneVolumes(e.target.checked)} disabled={running} className="rounded" />
              Include volumes in prune
            </label>
          )}
        </div>

        {/* Start button */}
        <button
          data-testid="maintenance-start"
          onClick={handleStart}
          disabled={running || !canStart}
          className="mt-5 w-full rounded-2xl bg-[linear-gradient(135deg,rgba(79,209,197,0.9),rgba(20,184,166,0.95))] px-4 py-3 text-sm font-medium text-[#042328] transition hover:brightness-105 disabled:opacity-40"
        >
          {running ? 'Running...' : 'Start update'}
        </button>

        {error && <p className="mt-2 text-xs text-red-400">{error}</p>}
      </div>

      {/* Right: progress */}
      <div className="flex min-w-0 flex-1 flex-col rounded-[28px] border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <h3 className="text-lg font-medium text-[var(--text)]">Progress</h3>

        {!jobId && (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-sm text-[var(--muted)]">Configure and start an update workflow.</p>
          </div>
        )}

        {jobId && (
          <div className="mt-4 flex flex-col gap-3">
            {/* Job state header */}
            <div className="flex items-center gap-2">
              {jobState === 'running' && <span className="inline-block size-2 animate-pulse rounded-full bg-sky-400" />}
              <span className={cn('text-sm font-medium', stepStatusColors[jobState ?? ''] ?? 'text-[var(--muted)]')}>
                {jobState === 'running' ? 'Running' : jobState === 'succeeded' ? 'Succeeded' : jobState === 'failed' ? 'Failed' : jobState ?? 'Starting'}
              </span>
              {events.length > 0 && events[events.length - 1].step && (
                <span className="text-xs text-[var(--muted)]">
                  Step {events[events.length - 1].step!.index}/{events[events.length - 1].step!.total}
                </span>
              )}
            </div>

            {/* Step list */}
            <div className="space-y-1">
              {events.filter((e) => e.event === 'job_step_started' || e.event === 'job_step_finished').map((event, i) => (
                <StepRow key={i} event={event} />
              ))}
            </div>

            {/* Raw output */}
            <div className="max-h-64 overflow-y-auto rounded-[16px] border border-[var(--panel-border)] bg-[rgba(0,0,0,0.3)] p-3 font-mono text-xs leading-5">
              {events.map((event, i) => {
                if (event.event === 'job_step_started' || event.event === 'job_step_finished') return null
                return (
                  <div key={i} className={cn(
                    event.event === 'job_error' ? 'text-red-400' :
                    event.event === 'job_warning' ? 'text-amber-400' :
                    'text-[var(--muted)]',
                  )}>
                    {event.message}
                    {event.data && <span className="text-[var(--text)]"> {event.data}</span>}
                  </div>
                )
              })}
              {events.length === 0 && <div className="text-[var(--muted)]">Waiting for events...</div>}
            </div>
          </div>
        )}
      </div>
    </div>
      </div>
    </div>
  )
}

function StackCheckbox({ stack, checked, onChange, disabled }: {
  stack: StackListItem
  checked: boolean
  onChange: () => void
  disabled: boolean
}) {
  const stateColors: Record<string, string> = {
    running: 'text-emerald-400',
    stopped: 'text-zinc-500',
    partial: 'text-amber-400',
    error: 'text-red-400',
    defined: 'text-zinc-600',
  }

  return (
    <label className="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-xs transition hover:bg-[rgba(255,255,255,0.03)]">
      <input type="checkbox" checked={checked} onChange={onChange} disabled={disabled} className="rounded" />
      <span className={cn('inline-block size-1.5 rounded-full', stateColors[stack.runtime_state] ?? 'bg-zinc-600')} />
      <span className="text-[var(--text)]">{stack.name}</span>
      <span className="text-[var(--muted)]">{stack.service_count.running}/{stack.service_count.defined}</span>
    </label>
  )
}

function StepRow({ event }: { event: JobEvent }) {
  const step = event.step
  if (!step) return null

  const isFinished = event.event === 'job_step_finished'
  const state = isFinished ? 'succeeded' : 'running'

  return (
    <div className="flex items-center gap-2 rounded-lg px-2 py-1 text-xs">
      <span className={cn('w-3 shrink-0 font-mono font-bold', stepStatusColors[state])}>
        {state === 'succeeded' ? '✓' : state === 'running' ? '▶' : '·'}
      </span>
      <span className="text-[var(--text)]">{step.target_stack_id ?? '—'}</span>
      <span className="text-[var(--muted)]">{step.action}</span>
    </div>
  )
}
