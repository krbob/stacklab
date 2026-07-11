import { useCallback, useMemo, useState } from 'react'
import { getStacks, updateStacksMaintenance } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { useJobStream } from '@/hooks/use-job-stream'
import { MaintenanceImages } from '@/components/maintenance-images'
import { MaintenanceCleanup } from '@/components/maintenance-cleanup'
import { MaintenanceNetworks } from '@/components/maintenance-networks'
import { MaintenanceVolumes } from '@/components/maintenance-volumes'
import { StepCards } from '@/components/step-cards'
import { PageHeader } from '@/components/page-header'
import { ConfirmDialog } from '@/components/confirm-dialog'
import type { StackListItem } from '@/lib/api-types'
import { cn } from '@/lib/cn'

type MaintenanceTab = 'update' | 'images' | 'networks' | 'volumes' | 'cleanup'
type TargetMode = 'all' | 'selected'

const maintenanceTabs: ReadonlyArray<readonly [MaintenanceTab, string]> = [
  ['update', 'Update'],
  ['images', 'Images'],
  ['networks', 'Networks'],
  ['volumes', 'Volumes'],
  ['cleanup', 'Cleanup'],
]

const stepStatusColors: Record<string, string> = {
  running: 'text-[var(--run)]',
  succeeded: 'text-[var(--ok)]',
  failed: 'text-[var(--danger)]',
  queued: 'text-stone-500',
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
  const [confirmDangerousUpdate, setConfirmDangerousUpdate] = useState(false)
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

  const requestStart = useCallback(() => {
    if (pruneAfter && pruneVolumes) {
      setConfirmDangerousUpdate(true)
      return
    }
    void handleStart()
  }, [handleStart, pruneAfter, pruneVolumes])

  const [activeTab, setActiveTab] = useState<MaintenanceTab>('update')

  return (
    <div className="flex flex-col gap-4" style={{ minHeight: 'calc(100vh - 120px)' }}>
      {/* Tab bar */}
      <PageHeader
        kicker="System"
        title="Maintenance"
        actions={
          <div className="flex max-w-full gap-1 overflow-x-auto" role="tablist" aria-label="Maintenance views">
            {maintenanceTabs.map(([key, label], index) => (
              <button
                key={key}
                id={`maintenance-tab-${key}`}
                role="tab"
                aria-selected={activeTab === key}
                aria-controls={`maintenance-panel-${key}`}
                tabIndex={activeTab === key ? 0 : -1}
                onClick={() => setActiveTab(key)}
                onKeyDown={(event) => {
                  let nextIndex: number
                  if (event.key === 'ArrowRight') nextIndex = (index + 1) % maintenanceTabs.length
                  else if (event.key === 'ArrowLeft') nextIndex = (index - 1 + maintenanceTabs.length) % maintenanceTabs.length
                  else if (event.key === 'Home') nextIndex = 0
                  else if (event.key === 'End') nextIndex = maintenanceTabs.length - 1
                  else return
                  event.preventDefault()
                  const nextTab = maintenanceTabs[nextIndex][0]
                  setActiveTab(nextTab)
                  document.getElementById(`maintenance-tab-${nextTab}`)?.focus()
                }}
                className={cn('shrink-0 whitespace-nowrap rounded-md border px-3 py-1.5 text-xs transition', activeTab === key ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}
              >
                {label}
              </button>
            ))}
          </div>
        }
      />

      <div id="maintenance-panel-images" role="tabpanel" aria-labelledby="maintenance-tab-images" hidden={activeTab !== 'images'}>
        <MaintenanceImages />
      </div>

      <div id="maintenance-panel-networks" role="tabpanel" aria-labelledby="maintenance-tab-networks" hidden={activeTab !== 'networks'}>
        <MaintenanceNetworks />
      </div>

      <div id="maintenance-panel-volumes" role="tabpanel" aria-labelledby="maintenance-tab-volumes" hidden={activeTab !== 'volumes'}>
        <MaintenanceVolumes />
      </div>

      <div id="maintenance-panel-cleanup" role="tabpanel" aria-labelledby="maintenance-tab-cleanup" hidden={activeTab !== 'cleanup'}>
        <MaintenanceCleanup />
      </div>

      <div id="maintenance-panel-update" role="tabpanel" aria-labelledby="maintenance-tab-update" hidden={activeTab !== 'update'}>
    <div className="flex flex-col gap-4 lg:flex-row">
      {/* Left: workflow setup */}
      <div className="w-full shrink-0 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)] lg:flex lg:w-80 lg:flex-col">
        <h2 className="text-lg font-medium text-[var(--text)]">Update stacks</h2>
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
          <label className="flex items-center gap-2 text-xs text-[var(--warning)]">
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
            <label className="ml-5 flex items-center gap-2 text-xs text-[var(--danger)]">
              <input type="checkbox" checked={pruneVolumes} onChange={(e) => setPruneVolumes(e.target.checked)} disabled={running} className="rounded" />
              Include volumes in prune
            </label>
          )}
        </div>

        {/* Start button */}
        <button
          data-testid="maintenance-start"
          onClick={requestStart}
          disabled={running || !canStart}
          className="mt-5 w-full rounded-lg bg-[var(--accent)] px-4 py-3 text-sm font-medium text-black transition hover:brightness-105 disabled:opacity-40"
        >
          {running ? 'Running...' : 'Start update'}
        </button>

        {error && <p className="mt-2 text-xs text-[var(--danger)]">{error}</p>}
      </div>

      {confirmDangerousUpdate && (
        <ConfirmDialog
          title="Start update and prune volumes?"
          message="This will update the selected stack scope and then remove unused Docker volumes. Volume removal is irreversible."
          items={[
            targetMode === 'all' ? 'target: all stacks' : `target: ${selectedIds.size} selected stack(s)`,
            'after update: prune unused volumes',
            removeOrphans ? 'remove orphans: enabled' : 'remove orphans: disabled',
          ]}
          confirmLabel="Start update"
          confirmingLabel="Starting..."
          confirming={startPending}
          onCancel={() => setConfirmDangerousUpdate(false)}
          onConfirm={() => {
            void handleStart().then(() => setConfirmDangerousUpdate(false))
          }}
        />
      )}

      {/* Right: progress */}
      <div aria-busy={startPending || (jobId !== null && jobState === null)} className="flex min-w-0 flex-1 flex-col rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <h2 className="text-lg font-medium text-[var(--text)]">Progress</h2>

        {!jobId && (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-sm text-[var(--muted)]">Configure and start an update workflow.</p>
          </div>
        )}

        {jobId && (
          <div className="mt-4 flex flex-col gap-3">
            {/* Job state header */}
            <div className="flex items-center gap-2" role="status" aria-live="polite" aria-atomic="true">
              {jobState === 'running' && <span className="inline-block size-2 animate-pulse rounded-full bg-[var(--run)]" aria-hidden="true" />}
              <span className={cn('text-sm font-medium', stepStatusColors[jobState ?? ''] ?? 'text-[var(--muted)]')}>
                {jobState === 'running' ? 'Running' : jobState === 'succeeded' ? 'Succeeded' : jobState === 'failed' ? 'Failed' : jobState ?? 'Starting'}
              </span>
            </div>

            {/* Step cards */}
            <StepCards events={events} />
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
    running: 'text-[var(--ok)]',
    stopped: 'text-stone-500',
    partial: 'text-[var(--warning)]',
    error: 'text-[var(--danger)]',
    defined: 'text-[var(--muted)]',
  }

  return (
    <label className="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-xs transition hover:bg-[rgba(255,255,255,0.03)]">
      <input type="checkbox" checked={checked} onChange={onChange} disabled={disabled} className="rounded" />
      <span className={cn('inline-block size-1.5 rounded-full', stateColors[stack.runtime_state] ?? 'bg-stone-600')} />
      <span className="text-[var(--text)]">{stack.name}</span>
      <span className="text-[var(--muted)]">{stack.service_count.running}/{stack.service_count.defined}</span>
    </label>
  )
}
