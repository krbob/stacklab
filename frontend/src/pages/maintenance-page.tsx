import { useCallback, useEffect, useMemo, useState } from 'react'
import { getGlobalAudit, getStacks, updateStacksMaintenance } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { useJobStream } from '@/hooks/use-job-stream'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import { MaintenanceImages } from '@/components/maintenance-images'
import { MaintenanceCleanup } from '@/components/maintenance-cleanup'
import { MaintenanceNetworks } from '@/components/maintenance-networks'
import { MaintenanceVolumes } from '@/components/maintenance-volumes'
import { AsyncState } from '@/components/async-state'
import { StepCards } from '@/components/step-cards'
import { PageHeader } from '@/components/page-header'
import { ConfirmDialog } from '@/components/confirm-dialog'
import type { AuditEntry, MaintenanceUpdateStacksRequest, StackListItem } from '@/lib/api-types'
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
  queued: 'text-[var(--muted)]',
}

export function MaintenancePage() {
  const {
    data: stacksData,
    error: stacksError,
    loading: stacksLoading,
    refetch: refetchStacks,
  } = useApi(() => getStacks(), [])
  const {
    data: auditData,
    error: auditError,
    loading: auditLoading,
    refetch: refetchAudit,
  } = useApi(() => getGlobalAudit({ limit: 25 }), [])
  const { openJob } = useJobDrawer()

  const [activeTab, setActiveTab] = useState<MaintenanceTab>('update')
  const [mountedTabs, setMountedTabs] = useState<Set<MaintenanceTab>>(() => new Set(['update']))

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
  const recentMaintenance = useMemo(
    () => (auditData?.items ?? []).filter((entry) => entry.action === 'update_stacks' || entry.action === 'prune').slice(0, 5),
    [auditData],
  )
  const isTerminal = jobState === 'succeeded' || jobState === 'failed' || jobState === 'cancelled' || jobState === 'timed_out'
  const running = startPending || (jobId !== null && !isTerminal)
  const canStart = stacksData !== null
    && (targetMode === 'all' ? stacks.length > 0 : selectedIds.size > 0)
  const stacksLoadError = stacksError
    ? new Error(`Failed to load stacks: ${stacksError.message}`)
    : null

  const activateTab = useCallback((tab: MaintenanceTab) => {
    setActiveTab(tab)
    setMountedTabs((current) => {
      if (current.has(tab)) return current
      const next = new Set(current)
      next.add(tab)
      return next
    })
  }, [])

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
    let selectedStackIds: MaintenanceUpdateStacksRequest['target']['stack_ids']
    if (targetMode === 'selected') {
      const [firstStackId, ...remainingStackIds] = Array.from(selectedIds)
      if (firstStackId === undefined) {
        setError('Select at least one stack to update')
        return
      }
      selectedStackIds = [firstStackId, ...remainingStackIds]
    }
    setStartPending(true)
    setError(null)
    setJobId(null)
    try {
      const result = await updateStacksMaintenance({
        target: {
          mode: targetMode,
          stack_ids: selectedStackIds,
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
    setConfirmDangerousUpdate(true)
  }, [])

  const updateScope = [
    pullImages && 'Pull configured service images.',
    buildImages && 'Build services with local build definitions.',
    removeOrphans && 'Remove orphan containers after deployment.',
    pruneAfter && `Prune unused Docker resources after update${pruneVolumes ? ', including volumes' : ''}.`,
  ].filter((item): item is string => Boolean(item))
  const updateImpact = [
    'Selected stacks will be deployed again and services may restart.',
    buildImages && 'Image builds will consume host CPU, storage, and network resources.',
    removeOrphans && 'Containers no longer present in Compose definitions will be removed.',
    pruneAfter && pruneVolumes && 'Unused Docker volume data selected by prune will be deleted permanently.',
  ].filter((item): item is string => Boolean(item))

  useEffect(() => {
    if (jobId && isTerminal) refetchAudit()
  }, [isTerminal, jobId, refetchAudit])

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
                onClick={() => activateTab(key)}
                onKeyDown={(event) => {
                  let nextIndex: number
                  if (event.key === 'ArrowRight') nextIndex = (index + 1) % maintenanceTabs.length
                  else if (event.key === 'ArrowLeft') nextIndex = (index - 1 + maintenanceTabs.length) % maintenanceTabs.length
                  else if (event.key === 'Home') nextIndex = 0
                  else if (event.key === 'End') nextIndex = maintenanceTabs.length - 1
                  else return
                  event.preventDefault()
                  const nextTab = maintenanceTabs[nextIndex][0]
                  activateTab(nextTab)
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

      {mountedTabs.has('images') && (
        <div id="maintenance-panel-images" role="tabpanel" aria-labelledby="maintenance-tab-images" hidden={activeTab !== 'images'}>
          <MaintenanceImages />
        </div>
      )}

      {mountedTabs.has('networks') && (
        <div id="maintenance-panel-networks" role="tabpanel" aria-labelledby="maintenance-tab-networks" hidden={activeTab !== 'networks'}>
          <MaintenanceNetworks />
        </div>
      )}

      {mountedTabs.has('volumes') && (
        <div id="maintenance-panel-volumes" role="tabpanel" aria-labelledby="maintenance-tab-volumes" hidden={activeTab !== 'volumes'}>
          <MaintenanceVolumes />
        </div>
      )}

      {mountedTabs.has('cleanup') && (
        <div id="maintenance-panel-cleanup" role="tabpanel" aria-labelledby="maintenance-tab-cleanup" hidden={activeTab !== 'cleanup'}>
          <MaintenanceCleanup />
        </div>
      )}

      <div id="maintenance-panel-update" role="tabpanel" aria-labelledby="maintenance-tab-update" hidden={activeTab !== 'update'}>
    <div className="flex flex-col gap-4 lg:flex-row">
      {/* Left: workflow setup */}
      <div className="w-full shrink-0 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)] lg:flex lg:w-80 lg:flex-col">
        <h2 className="text-lg font-medium text-[var(--text)]">Update stacks</h2>
        <p className="mt-2 text-xs text-[var(--muted)]">Pull images, build, and restart selected stacks.</p>

        <div className="mt-5">
          <AsyncState
            loading={stacksLoading}
            error={stacksLoadError}
            hasData={stacksData !== null}
            isEmpty={stacksData !== null && stacks.length === 0}
            loadingLabel="Loading stack update scope."
            emptyMessage="No stacks available to update."
            onRetry={refetchStacks}
            retryLabel="Retry stack list"
            loadingFallback={(
              <div className="space-y-2" data-testid="maintenance-stacks-loading">
                <div className="h-5 animate-pulse rounded bg-[rgba(255,255,255,0.05)]" />
                <div className="h-5 w-2/3 animate-pulse rounded bg-[rgba(255,255,255,0.04)]" />
              </div>
            )}
          >
            {/* Target mode */}
            <div className="space-y-2">
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
          </AsyncState>
        </div>

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
          title={pruneAfter && pruneVolumes ? 'Start update and prune volumes?' : 'Review stack update'}
          message="Review the target, update options, and recovery path before starting."
          review={{
            target: targetMode === 'all'
              ? `All ${stacks.length} stacks`
              : Array.from(selectedIds).sort().join(', '),
            scope: updateScope.length > 0 ? updateScope : ['Redeploy without pull, build, orphan removal, or prune.'],
            impact: updateImpact,
            snapshot: pruneAfter && pruneVolumes
              ? 'Stack definitions remain unchanged, but no automatic volume snapshot is created.'
              : 'Stack definitions remain unchanged; no automatic runtime snapshot is created.',
            recovery: pruneAfter && pruneVolumes
              ? 'Restore deleted volume data from an external backup, then redeploy the affected stacks.'
              : 'Redeploy prior image references and configuration; image rollback is not automatic.',
          }}
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

        {!jobId && stacksData && (
          <div className="mt-4 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
            <h3 className="text-sm font-medium text-[var(--text)]">Ready to run</h3>
            <p className="mt-1 text-xs text-[var(--muted)]">
              {targetMode === 'all'
                ? `${stacks.length} stack${stacks.length === 1 ? '' : 's'} in scope`
                : `${selectedIds.size} of ${stacks.length} stacks selected`}
            </p>
            <ul className="mt-3 grid gap-1 text-xs text-[var(--muted)] sm:grid-cols-2">
              <li>{pullImages ? '✓ Pull images' : '– Skip image pull'}</li>
              <li>{buildImages ? '✓ Build local images' : '– Skip builds'}</li>
              <li>{removeOrphans ? '✓ Remove orphans' : '– Keep orphans'}</li>
              <li>{pruneAfter ? `✓ Cleanup${pruneVolumes ? ' including volumes' : ''}` : '– No cleanup after update'}</li>
            </ul>
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

        <RecentMaintenance
          entries={recentMaintenance}
          loading={auditLoading}
          error={auditError}
          hasData={auditData !== null}
          onRetry={refetchAudit}
          onOpenJob={openJob}
        />
      </div>
    </div>
      </div>
    </div>
  )
}

function RecentMaintenance({ entries, loading, error, hasData, onRetry, onOpenJob }: {
  entries: AuditEntry[]
  loading: boolean
  error: Error | null
  hasData: boolean
  onRetry: () => void
  onOpenJob: (jobId: string) => void
}) {
  const resultColors: Record<string, string> = {
    succeeded: 'text-[var(--ok)]',
    failed: 'text-[var(--danger)]',
    timed_out: 'text-[var(--danger)]',
    cancelled: 'text-[var(--muted)]',
  }

  const loadError = error ? new Error(`Failed to load recent maintenance: ${error.message}`) : null

  return (
    <div
      data-testid="recent-maintenance"
      aria-busy={loading}
      className="mt-6 border-t border-[var(--panel-border)] pt-4"
    >
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-medium text-[var(--text)]">Recent maintenance</h3>
        <span className="text-xs text-[var(--muted)]">durable audit history</span>
      </div>
      <div className="mt-3">
        <AsyncState
          loading={loading}
          error={loadError}
          hasData={hasData}
          isEmpty={hasData && entries.length === 0}
          loadingLabel="Loading recent maintenance."
          emptyMessage="No update or cleanup run has finished yet."
          emptyFallback={(
            <p className="text-xs text-[var(--muted)]">No update or cleanup run has finished yet.</p>
          )}
          onRetry={onRetry}
          retryLabel="Retry recent maintenance"
          loadingFallback={(
            <div className="space-y-1">
              <div className="h-8 animate-pulse rounded-md bg-[rgba(255,255,255,0.03)]" />
              <div className="h-8 animate-pulse rounded-md bg-[rgba(255,255,255,0.03)]" />
            </div>
          )}
        >
          {entries.length > 0 && (
            <div className="space-y-1">
              {entries.map((entry) => {
                const content = (
                  <>
                    <span className="font-medium text-[var(--text)]">{entry.action === 'prune' ? 'Cleanup' : 'Update stacks'}</span>
                    <span className={cn('ml-auto', resultColors[entry.result] ?? 'text-[var(--muted)]')}>{entry.result}</span>
                    <span className="w-28 text-right tabular-nums text-[var(--muted)]">{new Date(entry.requested_at).toLocaleString()}</span>
                  </>
                )
                return entry.job_id ? (
                  <button
                    type="button"
                    key={entry.id}
                    onClick={() => onOpenJob(entry.job_id!)}
                    className="flex w-full items-center gap-3 rounded-md border border-[var(--panel-border)] px-3 py-2 text-left text-xs transition hover:border-[rgba(245,165,36,0.25)] hover:bg-[rgba(255,255,255,0.03)]"
                  >
                    {content}
                  </button>
                ) : (
                  <div key={entry.id} className="flex items-center gap-3 rounded-md border border-[var(--panel-border)] px-3 py-2 text-xs">
                    {content}
                  </div>
                )
              })}
            </div>
          )}
        </AsyncState>
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
  const stateDotColors: Record<string, string> = {
    running: 'bg-[var(--ok)]',
    stopped: 'bg-[var(--muted)]',
    partial: 'bg-[var(--warning)]',
    error: 'bg-[var(--danger)]',
    defined: 'bg-[var(--muted)]',
  }
  const stateTextColors: Record<string, string> = {
    running: 'text-[var(--ok)]',
    partial: 'text-[var(--warning)]',
    error: 'text-[var(--danger)]',
  }

  return (
    <label className="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-xs transition hover:bg-[rgba(255,255,255,0.03)]">
      <input type="checkbox" checked={checked} onChange={onChange} disabled={disabled} className="rounded" />
      <span aria-hidden="true" className={cn('inline-block size-1.5 rounded-full', stateDotColors[stack.runtime_state] ?? 'bg-[var(--muted)]')} />
      <span className="min-w-0 flex-1 truncate text-[var(--text)]">{stack.name}</span>
      <span className={cn('shrink-0 text-[var(--muted)]', stateTextColors[stack.runtime_state])}>
        {stack.runtime_state.replace('_', ' ')} · {stack.service_count.running}/{stack.service_count.defined}
      </span>
    </label>
  )
}
