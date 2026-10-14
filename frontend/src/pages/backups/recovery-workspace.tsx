import { useEffect, useState } from 'react'
import { useApi } from '@/hooks/use-api'
import { useJobStream } from '@/hooks/use-job-stream'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  backupDownloadURL,
  backupArchiveURL,
  deleteBackupRestore,
  compareBackupSnapshots,
  getBackupCatalog,
  getBackupRestoreCatalog,
  getBackupRestores,
  getBackupSnapshots,
  restoreBackupSnapshot,
  type BackupCatalog,
  type BackupCatalogQuery,
  type BackupComparison,
  type BackupDestination,
  type BackupJobResponse,
  type BackupPlan,
  type BackupRestoreResult,
  type BackupSnapshot,
} from '@/lib/backup-api'
import {
  BackupDate,
  BackupError,
  BackupPanel,
  BackupSize,
  BackupStatus,
  buttonClass,
  Field,
  inputClass,
  primaryClass,
} from './backup-primitives'
import { ApplyRecoveryDialog } from './apply-recovery-dialog'
import { SourceLabel } from './source-label'
import { formatBackupDate } from './backup-formatters'

type SelectedFile = { source_id: string; path: string }
const terminal = ['succeeded', 'failed', 'cancelled', 'timed_out']

export function RecoveryWorkspace({
  destinations,
  initialDestinationId,
  onDestinationChange,
  plans = [],
  toolsAvailable = true,
  rcloneAvailable = true,
  refreshKey = 0,
  snapshotRevisions = {},
  restoreRevisions = {},
  applyBlocked,
  active = true,
  helperReady,
  helperError,
  onStarted,
}: {
  destinations: BackupDestination[]
  initialDestinationId?: string
  onDestinationChange?: (id: string) => void
  plans?: BackupPlan[]
  toolsAvailable?: boolean
  rcloneAvailable?: boolean
  refreshKey?: number
  snapshotRevisions?: Record<string, number>
  restoreRevisions?: Record<string, number>
  applyBlocked?: string
  active?: boolean
  helperReady?: boolean
  helperError?: string
  onStarted: (result: BackupJobResponse, destinationId: string, test: boolean) => void
}) {
  const [destinationId, setDestinationId] = useState(
    initialDestinationId ?? destinations.find((item) => item.kind === 'local')?.id ?? '',
  )
  useEffect(() => {
    if (initialDestinationId !== undefined) setDestinationId(initialDestinationId)
  }, [initialDestinationId])
  const [visited, setVisited] = useState<string[]>(() => destinationId ? [destinationId] : [])
  useEffect(() => {
    if (destinationId) setVisited((previous) => previous.includes(destinationId) ? previous : [...previous, destinationId])
  }, [destinationId])
  return (
    <div className="space-y-4">
      <BackupPanel title="Recover data">
        <p className="mb-4 text-sm leading-6 text-[var(--muted)]">
          Choose a recovery set, inspect its files, then restore to a separate directory. Applying recovered data to a
          stack always has its own mapping and review.
        </p>
        <Field label="Recovery destination">
          <select
            className={inputClass}
            value={destinationId}
            onChange={(event) => {
              setDestinationId(event.target.value)
              onDestinationChange?.(event.target.value)
            }}
          >
            <option value="">Select a destination</option>
            {destinations.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name}
              </option>
            ))}
          </select>
        </Field>
        {!destinations.length && (
          <p className="mt-3 text-sm text-[var(--muted)]">
            Add a repository destination before browsing recovery sets.
          </p>
        )}
      </BackupPanel>
      {destinations.filter((item) => visited.includes(item.id) || item.id === destinationId).map((destination) => (
        <div key={destination.id} hidden={destination.id !== destinationId}>
          <DestinationRecovery
            destination={destination}
            plans={plans}
            active={active && destination.id === destinationId}
            refreshKey={refreshKey}
            snapshotRevision={snapshotRevisions[destination.id] ?? 0}
            restoreRevision={restoreRevisions[destination.id] ?? 0}
            applyBlocked={applyBlocked}
            helperReady={helperReady}
            helperError={helperError}
            toolsAvailable={toolsAvailable && (destination.kind !== 'rclone' || rcloneAvailable)}
            onStarted={(result, test) => onStarted(result, destination.id, test)}
          />
        </div>
      ))}
    </div>
  )
}

function DestinationRecovery({
  destination,
  plans,
  toolsAvailable,
  active,
  refreshKey,
  snapshotRevision,
  restoreRevision,
  applyBlocked,
  helperReady,
  helperError,
  onStarted,
}: {
  destination: BackupDestination
  plans: BackupPlan[]
  toolsAvailable: boolean
  active: boolean
  refreshKey: number
  snapshotRevision: number
  restoreRevision: number
  applyBlocked?: string
  helperReady?: boolean
  helperError?: string
  onStarted: (result: BackupJobResponse, test: boolean) => void
}) {
  const snapshots = useApi(
    (signal) => (toolsAvailable ? getBackupSnapshots(destination.id, signal) : Promise.resolve({ items: [] })),
    [destination.id, toolsAvailable, refreshKey, snapshotRevision],
    { enabled: active, staleTime: 60000 },
  )
  const restores = useApi((signal) => getBackupRestores(signal), [refreshKey, restoreRevision], { enabled: active, staleTime: 30000 })
  const { refetch: refreshRestores } = restores
  useEffect(() => {
    if (!active) return
    const refresh = () => { if (document.visibilityState !== 'hidden') refreshRestores() }
    const timer = window.setInterval(refresh, 30000)
    window.addEventListener('focus', refresh)
    document.addEventListener('visibilitychange', refresh)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('focus', refresh)
      document.removeEventListener('visibilitychange', refresh)
    }
  }, [active, refreshRestores])
  const { openJob } = useJobDrawer()
  const [snapshotId, setSnapshotId] = useState('')
  const [restoreId, setRestoreId] = useState<string | null>(null)
  const [selectionReset, setSelectionReset] = useState(0)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [jobId, setJobId] = useState<string | null>(null)
  const [apply, setApply] = useState<BackupRestoreResult | null>(null)
  const [remove, setRemove] = useState<BackupRestoreResult | null>(null)
  const [removeError, setRemoveError] = useState<string | null>(null)
  const [removing, setRemoving] = useState(false)
  const { state: jobState } = useJobStream({ jobId })
  const running = !!jobId && !terminal.includes(jobState ?? '')
  const results = (restores.data?.items ?? []).filter((item) => item.destination_id === destination.id)
  const automaticallySelected = results.find((item) => item.job_id === jobId)
  const chosen = restoreId === null ? automaticallySelected : results.find((item) => item.id === restoreId)
  const selectedSnapshot = snapshots.data?.items.find((item) => item.id === snapshotId)
  useEffect(() => {
    if (!jobId) return
    if (terminal.includes(jobState ?? '')) refreshRestores()
    if (running) {
      const timer = window.setInterval(refreshRestores, 3000)
      return () => window.clearInterval(timer)
    }
  }, [jobId, jobState, running, refreshRestores])

  async function restore(files?: SelectedFile[], sourceIds?: string[], test = true) {
    if (!snapshotId || pending || running || !toolsAvailable) return
    setPending(true)
    setError(null)
    try {
      const result = await restoreBackupSnapshot(destination.id, {
        snapshot_id: snapshotId,
        test: test && !files?.length && !sourceIds?.length,
        ...(files?.length ? { files } : {}),
        ...(sourceIds?.length ? { source_ids: sourceIds } : {}),
      })
      setRestoreId(null)
      setSelectionReset((value) => value + 1)
      setJobId(result.job.id)
      onStarted(result, test && !files?.length && !sourceIds?.length)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to start recovery')
    } finally {
      setPending(false)
    }
  }

  return (
    <>
      <BackupPanel
        title="1. Choose and inspect a recovery set"
        actions={
          <button className={buttonClass} disabled={!toolsAvailable || snapshots.loading} onClick={snapshots.refetch}>
            Refresh snapshots
          </button>
        }
      >
        <BackupError error={error ?? snapshots.error?.message ?? null} />
        {!toolsAvailable && (
          <p className="text-sm text-[var(--warning)]">
            Install the required backup tools before accessing this repository.
          </p>
        )}
        {snapshots.loading && !snapshots.data && (
          <p role="status" className="text-sm text-[var(--muted)]">
            Loading recovery sets…
          </p>
        )}
        {snapshots.data && (
          <Field label="Recovery set">
            <select
              className={inputClass}
              value={snapshotId}
              disabled={pending || running}
              onChange={(event) => setSnapshotId(event.target.value)}
            >
              <option value="">Select a snapshot</option>
              {snapshotId && !selectedSnapshot && <option value={snapshotId} disabled>Unavailable snapshot {snapshotId.slice(0, 8)}</option>}
              {(snapshots.data.items ?? []).map((snapshot) => (
                <option key={snapshot.id} value={snapshot.id}>
                  {formatBackupDate(snapshot.time)} · {snapshot.short_id || snapshot.id.slice(0, 8)} · {snapshot.recovery_status ?? 'unknown'} ·{' '}
                  {plans.find((plan) => snapshot.tags?.includes(`plan:${plan.id}`))?.name ??
                    snapshot.tags?.find((tag) => tag.startsWith('plan:'))?.slice(5) ??
                    snapshot.hostname}
                </option>
              ))}
            </select>
          </Field>
        )}
        {snapshotId && snapshots.data && !selectedSnapshot && <p role="status" className="mt-3 text-sm text-[var(--warning)]">
          The selected snapshot {snapshotId.slice(0, 8)} is no longer in this repository. It may have been removed by
          retention or cleanup. Select another recovery set to continue.
        </p>}
        {toolsAvailable && snapshots.data && !snapshots.data.items?.length && (
          <p className="mt-3 text-sm text-[var(--muted)]">No snapshots found in this repository.</p>
        )}
        {selectedSnapshot && (
          <div className="mt-4 space-y-4">
            <p className="break-all text-xs text-[var(--muted)]">
              Captured <BackupDate value={selectedSnapshot.time} /> · {(selectedSnapshot.tags ?? []).join(', ')}
            </p>
            <p className="text-xs text-[var(--muted)]">Recovery objects: <BackupStatus value={selectedSnapshot.recovery_status ?? 'unknown'} />{selectedSnapshot.classified_at && <> · Classified <BackupDate value={selectedSnapshot.classified_at} /></>}</p>
            {selectedSnapshot.recovery_status && !['complete', 'unknown'].includes(selectedSnapshot.recovery_status) && <p className="text-sm text-[var(--warning)]">This snapshot is {selectedSnapshot.recovery_status}. It is preserved by automatic retention and is not evidence of a complete recovery set.</p>}
            {selectedSnapshot.restore_tested_at && (
              <p className="text-xs text-[var(--muted)]">Complete retrieval test of this snapshot: <BackupDate value={selectedSnapshot.restore_tested_at} /></p>
            )}
            <FileBrowser
              key={snapshotId}
              destinationId={destination.id}
              snapshot={selectedSnapshot}
              snapshots={snapshots.data?.items ?? []}
              disabled={pending || running}
              selectionReset={selectionReset}
              onRestore={(files, sourceIds) => {
                void restore(files, sourceIds)
              }}
            />
            <div className="space-y-2 rounded-md border border-[var(--panel-border)] p-3">
              <button
                className={primaryClass}
                disabled={pending || running}
                onClick={() => {
                  void restore()
                }}
              >
                {pending ? 'Starting…' : 'Restore complete set and test retrieval'}
              </button>
              <p className="text-xs leading-5 text-[var(--muted)]">
                Creates a private directory on this host and verifies the full recovery set. Disk space and provider
                transfer charges apply. Application startup is a separate recovery exercise.
              </p>
              <button
                className={buttonClass}
                disabled={pending || running}
                onClick={() => {
                  void restore(undefined, undefined, false)
                }}
              >
                Retrieve complete set for offline recovery
              </button>
              <p className="text-xs leading-5 text-[var(--muted)]">
                Retrieves original archives without claiming a recovery test. Use this when preserved metadata requires
                offline extraction.
              </p>
            </div>
          </div>
        )}
        {jobId && (
          <div
            className="mt-4 flex flex-wrap items-center gap-2 rounded-md border border-[var(--panel-border)] p-3"
            role="status"
          >
            <span className="text-sm text-[var(--text)]">Isolated restore</span>
            <BackupStatus value={jobState ?? 'queued'} />
            <button className={buttonClass} onClick={() => openJob(jobId)}>
              Operation details
            </button>
            {jobState === 'succeeded' && (
              <span className="text-xs text-[var(--muted)]">Completed result selected below.</span>
            )}
            {terminal.includes(jobState ?? '') && jobState !== 'succeeded' && (
              <span className="text-xs text-[var(--warning)]">
                Open details to inspect the failure. You can retry from the selected snapshot.
              </span>
            )}
          </div>
        )}
      </BackupPanel>
      <BackupPanel
        title="2. Browse completed restores and apply data"
        actions={
          <button className={buttonClass} onClick={refreshRestores}>
            Refresh restored data
          </button>
        }
      >
        <BackupError error={restores.error?.message ?? null} />
        {restores.loading && !restores.data && (
          <p role="status" className="text-sm text-[var(--muted)]">
            Loading restored data…
          </p>
        )}
        {results.length ? (
          <Field label="Completed isolated restore">
            <select
              className={inputClass}
              value={chosen?.id ?? ''}
              onChange={(event) => setRestoreId(event.target.value)}
            >
              <option value="">Select a restore</option>
              {results.map((item) => (
                <option key={item.id} value={item.id}>
                  {formatBackupDate(item.created_at)} · {item.snapshot_id.slice(0, 8)} ·{' '}
                  {item.test ? 'complete test' : item.selected_files?.length ? 'selected files' : item.manifest && (!item.source_ids?.length || item.source_ids.length === item.manifest.sources.length) ? 'complete set (offline retrieval)' : 'selected sources'}
                  {item.available === false ? ' · unavailable' : ''}
                </option>
              ))}
            </select>
          </Field>
        ) : (
          <p className="text-sm text-[var(--muted)]">
            Completed isolated restores will appear here automatically. You can return to them after leaving this page.
          </p>
        )}
        {chosen && (
          <div className="mt-4 space-y-4">
            <p className="break-all text-xs text-[var(--muted)]">
              Restored <BackupDate value={chosen.created_at} /> · {chosen.target}
            </p>
            {chosen.available === false ? (
              <p role="alert" className="text-sm text-[var(--warning)]">
                {chosen.unavailable_reason ||
                  'The restored data is no longer available on this host. Restore the snapshot again.'}
              </p>
            ) : (
              <>
                <FileBrowser
                  key={chosen.id}
                  destinationId={destination.id}
                  restored={chosen}
                  snapshots={[]}
                  disabled={false}
                />
                {!chosen.selected_files?.length ? (
                  <div className="space-y-2">
                    <button className={primaryClass} disabled={helperReady === false || !!applyBlocked} aria-describedby={`apply-readiness-${destination.id}`} onClick={() => setApply(chosen)}>
                      Review applying recovered data
                    </button>
                    {(helperReady === false || applyBlocked) && <p id={`apply-readiness-${destination.id}`} className="text-sm text-[var(--warning)]">
                      {applyBlocked || `In-place recovery is unavailable: ${helperError || 'The backup helper must pass its compatibility and permission checks.'}`}
                    </p>}
                  </div>
                ) : (
                  <p className="text-xs text-[var(--muted)]">
                    Selected files are ready to download. Restore a complete source to apply it to a stack.
                  </p>
                )}
              </>
            )}
            {chosen.job_id && (
              <button className={buttonClass} onClick={() => openJob(chosen.job_id!)}>
                Restore job details
              </button>
            )}
            {chosen.managed !== undefined && (
              <button
                className={buttonClass}
                disabled={removing}
                onClick={() => {
                  setRemoveError(null)
                  setRemove(chosen)
                }}
              >
                {chosen.managed ? 'Delete isolated restore' : 'Forget result'}
              </button>
            )}
          </div>
        )}
      </BackupPanel>
      {apply && (
        <ApplyRecoveryDialog
          destination={destination}
          restored={apply}
          blockedReason={applyBlocked || (helperReady === false ? helperError || 'The backup helper is unavailable.' : undefined)}
          onClose={() => setApply(null)}
          onStarted={(result) => {
            onStarted(result, false)
            setApply(null)
          }}
        />
      )}
      {remove && (
        <ConfirmDialog
          title={remove.managed ? 'Delete isolated restore' : 'Forget recovery result'}
          message={
            remove.managed
              ? 'Delete this local restored data and its registration to reclaim host storage. Repository snapshots remain unchanged. Download any files you want to retain first.'
              : 'Remove this result from the recovery library. Its custom directory and every file remain on this host. Repository snapshots also remain unchanged; manage these files separately.'
          }
          items={[remove.target]}
          confirmLabel={remove.managed ? 'Delete restored data' : 'Forget result'}
          confirming={removing}
          error={removeError}
          onCancel={() => {
            if (!removing) setRemove(null)
          }}
          onConfirm={() => {
            setRemoving(true)
            setRemoveError(null)
            void deleteBackupRestore(remove.id)
              .then(() => {
                setRemove(null)
                setRestoreId('')
                refreshRestores()
              })
              .catch((cause) =>
                setRemoveError(cause instanceof Error ? cause.message : 'Unable to delete restored data'),
              )
              .finally(() => setRemoving(false))
          }}
        />
      )}
    </>
  )
}

function FileBrowser({
  destinationId,
  snapshot,
  restored,
  snapshots,
  disabled,
  selectionReset = 0,
  onRestore,
}: {
  destinationId: string
  snapshot?: BackupSnapshot
  restored?: BackupRestoreResult
  snapshots: BackupSnapshot[]
  disabled: boolean
  selectionReset?: number
  onRestore?: (files?: SelectedFile[], sourceIds?: string[]) => void
}) {
  const [sourceId, setSourceId] = useState(
    () =>
      restored?.selected_files?.[0]?.source_id ||
      (restored?.source_ids?.length === 1
        ? restored.source_ids[0]
        : restored?.manifest?.sources.length === 1
          ? restored.manifest.sources[0].source.id
          : ''),
  )
  const [path, setPath] = useState('')
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [offset, setOffset] = useState(0)
  const [selected, setSelected] = useState<SelectedFile[]>([])
  const [baseline, setBaseline] = useState('')
  const [selectionError, setSelectionError] = useState<string | null>(null)
  useEffect(() => {
    setSelected([])
    setSelectionError(null)
  }, [selectionReset])
  const [comparisonOffset, setComparisonOffset] = useState(0)
  const queryKey = JSON.stringify([destinationId, snapshot?.id, restored?.id, sourceId, path, debouncedSearch, offset])
  useEffect(() => {
    const timer = window.setTimeout(() => {
      setDebouncedSearch(search)
      setOffset(0)
    }, 250)
    return () => window.clearTimeout(timer)
  }, [search])
  const files = useApi(
    async (signal) => {
      const query: BackupCatalogQuery = {
        source_id: sourceId || undefined,
        path,
        search: debouncedSearch,
        offset,
        limit: 50,
      }
      const catalog = await (restored
        ? getBackupRestoreCatalog(restored.id, query, signal)
        : getBackupCatalog(destinationId, snapshot!.id, query, signal))
      return { catalog, queryKey }
    },
    [destinationId, snapshot?.id, restored?.id, sourceId, path, debouncedSearch, offset],
  )
  const comparison = useApi(
    async (signal) =>
      baseline && sourceId && snapshot
        ? compareBackupSnapshots(destinationId, snapshot.id, baseline, sourceId, comparisonOffset, signal)
        : Promise.resolve(null),
    [baseline, sourceId, snapshot?.id, destinationId, comparisonOffset],
  )
  const catalog = files.data?.catalog
  const stale = files.data?.queryKey !== queryKey
  const source = catalog?.sources.find((item) => item.source.id === sourceId)
  const selectedSources = new Set(selected.map((item) => item.source_id))
  const transferBytes = (catalog?.sources ?? [])
    .filter((item) => selectedSources.has(item.source.id))
    .reduce((sum, item) => sum + item.size_bytes, 0)
  function navigate(nextPath: string) {
    setPath(nextPath)
    setSearch('')
    setDebouncedSearch('')
    setOffset(0)
  }
  function selectFile(file: SelectedFile) {
    const alreadySelected = selected.some((item) => item.source_id === file.source_id && item.path === file.path)
    if (!alreadySelected && selected.length >= 100) {
      setSelectionError('Select up to 100 files per restore. Restore the whole source for larger selections.')
      return
    }
    setSelectionError(null)
    setSelected((previous) =>
      previous.some((item) => item.source_id === file.source_id && item.path === file.path)
        ? previous.filter((item) => item.source_id !== file.source_id || item.path !== file.path)
        : [...previous, file],
    )
  }
  return (
    <section className="space-y-3" aria-label={restored ? 'Restored files' : 'Snapshot files'}>
      <BackupError error={files.error?.message ?? selectionError} />
      {files.error && (
        <button className={buttonClass} onClick={files.refetch}>
          Retry file listing
        </button>
      )}
      {files.loading && !catalog && (
        <p role="status" className="text-xs text-[var(--muted)]">
          Loading file index…
        </p>
      )}
      {catalog && (
        <>
          <Field label={restored ? 'Restored source' : 'Snapshot source'}>
            <select
              className={inputClass}
              value={sourceId}
              onChange={(event) => {
                setSourceId(event.target.value)
                navigate('')
                setBaseline('')
                setComparisonOffset(0)
              }}
            >
              <option value="">Select a source to browse</option>
              {(catalog.sources ?? []).map((item) => (
                <option key={item.source.id} value={item.source.id}>
                  {[item.source.stack_id, item.source.service, item.source.mount || item.source.kind]
                    .filter(Boolean)
                    .join(' / ')}
                </option>
              ))}
            </select>
          </Field>
          {source && (
            <div className="text-xs text-[var(--muted)]">
              <SourceLabel source={source.source} />
            </div>
          )}
          {source?.recovery_restrictions?.map((restriction) => (
            <p key={restriction} className="text-xs text-[var(--warning)]">
              Offline recovery required: {restriction}
            </p>
          ))}
          {restored && source && !restored.selected_files?.length && (
            <div className="space-y-2">
              <a className={buttonClass} href={backupArchiveURL(restored.id, sourceId)} download>
                Download original source archive
              </a>
              <p className="text-xs text-[var(--muted)]">
                Preserves the complete source and its metadata. Inspect and extract it offline using the recovery
                runbook; downloading alone does not test application recovery.
              </p>
            </div>
          )}
          {sourceId && !catalog.index_available && (
            <p className="rounded-md border border-[var(--warning)]/30 p-3 text-sm leading-6 text-[var(--warning)]">
              This snapshot has no file index. You can still restore this source or the complete set. File browsing and
              comparison may remain unavailable for large archives; use the recovery runbook to inspect them offline.{' '}
              {catalog.index_unavailable_reason || source?.index_unavailable_reason}
            </p>
          )}
          {sourceId && catalog.index_available && (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <button
                  className={buttonClass}
                  disabled={files.loading || (!path && !search)}
                  onClick={() => navigate('')}
                >
                  Source root
                </button>
                {path && (
                  <button
                    className={buttonClass}
                    disabled={files.loading}
                    onClick={() => navigate(path.split('/').slice(0, -1).join('/'))}
                  >
                    Parent directory
                  </button>
                )}
                <span className="break-all font-mono text-xs text-[var(--muted)]">/{path}</span>
              </div>
              <Field
                label={restored ? 'Search restored files' : 'Search snapshot files'}
                hint="Search includes nested directories in this source."
              >
                <input
                  className={inputClass}
                  type="search"
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                />
              </Field>
              <div
                className="divide-y divide-[var(--panel-border)] rounded-md border border-[var(--panel-border)]"
                aria-label="File entries"
                aria-busy={files.loading}
              >
                {(catalog.items ?? []).map((file) => (
                  <div key={file.path} className="flex min-w-0 flex-wrap items-start justify-between gap-2 p-3">
                    <div className="flex min-w-0 flex-1 items-start gap-2">
                      {onRestore && file.type === 'file' && (
                        <input
                          aria-label={`Select ${file.path}`}
                          type="checkbox"
                          className="mt-1"
                          disabled={disabled || files.loading || stale}
                          checked={selected.some((item) => item.source_id === sourceId && item.path === file.path)}
                          onChange={() => selectFile({ source_id: sourceId, path: file.path })}
                        />
                      )}
                      <div className="min-w-0">
                        {file.type === 'directory' ? (
                          <button
                            className="break-all text-left text-sm text-[var(--accent)] underline underline-offset-4"
                            disabled={files.loading}
                            onClick={() => navigate(file.path)}
                          >
                            {file.path}/
                          </button>
                        ) : (
                          <p className="break-all text-sm text-[var(--text)]">{file.path}</p>
                        )}
                        <p className="mt-1 break-all text-xs text-[var(--muted)]">
                          {file.type} · <BackupSize value={file.size_bytes} /> · <BackupDate value={file.modified_at} />
                          {file.link_target && ` → ${file.link_target}`}
                        </p>
                        <details className="mt-1 text-xs text-[var(--muted)]">
                          <summary className="cursor-pointer">File metadata</summary>
                          <p className="mt-1 break-all">
                            Mode {file.mode.toString(8)} · Owner {file.uid}:{file.gid}
                            {file.sha256 && ` · SHA-256 ${file.sha256}`}
                          </p>
                        </details>
                      </div>
                    </div>
                    {restored && file.type === 'file' && (
                      <a
                        className={buttonClass}
                        aria-label={`Download ${file.path}`}
                        aria-disabled={files.loading || stale}
                        href={files.loading || stale ? undefined : backupDownloadURL(restored.id, sourceId, file.path)}
                        download
                      >
                        Download
                      </a>
                    )}
                  </div>
                ))}
                {!files.loading && !files.error && !stale && !catalog.items?.length && (
                  <p className="p-3 text-sm text-[var(--muted)]">
                    {search ? 'No files match this search.' : 'This directory is empty.'}
                  </p>
                )}
              </div>
              <CatalogPagination page={catalog} onOffset={setOffset} disabled={files.loading} />
              {snapshot && (
                <>
                  <Field label="Compare this source with another snapshot">
                    <select
                      className={inputClass}
                      value={baseline}
                      onChange={(event) => {
                        setBaseline(event.target.value)
                        setComparisonOffset(0)
                      }}
                    >
                      <option value="">No comparison</option>
                      {snapshots
                        .filter((item) => item.id !== snapshot.id)
                        .map((item) => (
                          <option key={item.id} value={item.id}>
                            {formatBackupDate(item.time)} · {item.short_id || item.id.slice(0, 8)}
                          </option>
                        ))}
                    </select>
                  </Field>
                  {baseline && (
                    <div className="space-y-2">
                      <p className="text-xs text-[var(--muted)]">
                        Changes from the comparison snapshot to the selected recovery set. Content hashes identify
                        changed file bytes.
                      </p>
                      <BackupError error={comparison.error?.message ?? null} />
                      {comparison.loading && (
                        <p role="status" className="text-xs text-[var(--muted)]">
                          Comparing file indexes…
                        </p>
                      )}
                      {!comparison.loading && !comparison.error && comparison.data && (
                        <FileComparison data={comparison.data} onOffset={setComparisonOffset} />
                      )}
                    </div>
                  )}
                </>
              )}
            </>
          )}
          {onRestore && sourceId && (
            <div className="space-y-2 rounded-md bg-[var(--bg)] p-3">
              <p className="text-xs leading-5 text-[var(--muted)]">
                Selected files require retrieving their complete source archives.{' '}
                {files.loading ? (
                  'Calculating archive transfer…'
                ) : selected.length ? (
                  <>
                    {selected.length} files selected · estimated archive transfer <BackupSize value={transferBytes} />.
                  </>
                ) : (
                  <>
                    This source requires <BackupSize value={source?.size_bytes} /> of archive transfer.
                  </>
                )}{' '}
                Provider download charges and temporary disk space apply.
              </p>
              <div className="flex flex-wrap gap-2">
                <button
                  className={buttonClass}
                  disabled={disabled || files.loading}
                  onClick={() => onRestore(undefined, [sourceId])}
                >
                  Restore this source
                </button>
                {selected.length > 0 && (
                  <>
                    <button
                      className={primaryClass}
                      disabled={disabled || files.loading}
                      onClick={() => onRestore(selected)}
                    >
                      Restore {selected.length} selected {selected.length === 1 ? 'file' : 'files'} for download
                    </button>
                    <button
                      className={buttonClass}
                      disabled={disabled}
                      onClick={() => {
                        setSelected([])
                        setSelectionError(null)
                      }}
                    >
                      Clear selection
                    </button>
                  </>
                )}
              </div>
            </div>
          )}
        </>
      )}
    </section>
  )
}

function CatalogPagination({
  page,
  onOffset,
  disabled = false,
}: {
  page: Pick<BackupCatalog, 'total' | 'offset' | 'limit'>
  onOffset: (offset: number) => void
  disabled?: boolean
}) {
  return page.total > page.limit ? (
    <nav className="flex flex-wrap items-center justify-between gap-2" aria-label="File pages">
      <button
        className={buttonClass}
        disabled={disabled || page.offset === 0}
        onClick={() => onOffset(Math.max(0, page.offset - page.limit))}
      >
        Previous files
      </button>
      <span className="text-xs text-[var(--muted)]">
        {page.offset + 1}–{Math.min(page.total, page.offset + page.limit)} of {page.total}
      </span>
      <button
        className={buttonClass}
        disabled={disabled || page.offset + page.limit >= page.total}
        onClick={() => onOffset(page.offset + page.limit)}
      >
        Next files
      </button>
    </nav>
  ) : null
}

function FileComparison({ data, onOffset }: { data: BackupComparison; onOffset: (offset: number) => void }) {
  if (!data.index_available)
    return (
      <p className="text-sm text-[var(--warning)]">
        Both snapshots need a file index for comparison. {data.index_unavailable_reason}
      </p>
    )
  return (
    <>
      <p className="text-xs text-[var(--muted)]">
        {data.added} added · {data.removed} removed · {data.modified} modified
      </p>
      <ul className="space-y-2 text-sm text-[var(--text)]">
        {data.items.map((item) => (
          <li key={item.path} className="flex flex-wrap items-center gap-2">
            <BackupStatus value={item.change} />
            <span className="break-all">{item.path}</span>
            {item.change === 'modified' && (
              <span className="text-xs text-[var(--muted)]">
                {[item.content_changed && 'content changed', item.metadata_changed && 'metadata changed']
                  .filter(Boolean)
                  .join(' · ')}
              </span>
            )}
          </li>
        ))}
      </ul>
      {!data.items.length && <p className="text-xs text-[var(--muted)]">No changes found in this source.</p>}
      <CatalogPagination page={data} onOffset={onOffset} />
    </>
  )
}
