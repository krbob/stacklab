import { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { AsyncState } from '@/components/async-state'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { PageHeader } from '@/components/page-header'
import { useApi } from '@/hooks/use-api'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import { useJobStream } from '@/hooks/use-job-stream'
import {
  checkBackupDestination,
  deleteBackupDestination,
  deleteBackupPlan,
  getBackups,
  initializeBackupDestination,
  pruneBackupDestination,
  retryBackupRun,
  runBackupPlan,
  testBackupDestination,
  type BackupDestination,
  type BackupJobResponse,
  type BackupPlan,
} from '@/lib/backup-api'
import { cn } from '@/lib/cn'
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
} from './backups/backup-primitives'
import { DestinationEditor } from './backups/destination-editor'
import { PlanEditor } from './backups/plan-editor'
import { RecoveryWorkspace } from './backups/recovery-workspace'
import { ProtectionDashboard } from './backups/protection-dashboard'
import { RunHistory } from './backups/run-history'
import { RunPlanDialog } from './backups/run-plan-dialog'
import { scheduleDescription } from './backups/schedule-utils'
import { SourceLabel } from './backups/source-label'
import { BackupOperations } from './backups/backup-operations'

type Editor =
  | { kind: 'destination'; value?: BackupDestination }
  | { kind: 'plan'; value?: BackupPlan; destinationId?: string }
  | null
type Review =
  | { kind: 'initialize' | 'prune' | 'deleteDestination'; destination: BackupDestination }
  | { kind: 'deletePlan'; plan: BackupPlan }
  | null
const tabs = ['protection', 'recovery', 'history', 'plans', 'destinations'] as const
type Tab = (typeof tabs)[number]

export function BackupsPage() {
  const [search, setSearch] = useSearchParams()
  const rawTab = search.get('view')
  const tab: Tab = rawTab === 'runs' ? 'history' : tabs.includes(rawTab as Tab) ? (rawTab as Tab) : 'protection'
  const [mountedTabs, setMountedTabs] = useState<Set<Tab>>(() => new Set([tab]))
  useEffect(() => {
    setMountedTabs((previous) => (previous.has(tab) ? previous : new Set([...previous, tab])))
  }, [tab])
  const state = useApi((signal) => getBackups(signal), [])
  const { refetch } = state
  const { openJob } = useJobDrawer()
  const [editor, setEditor] = useState<Editor>(null)
  const [review, setReview] = useState<Review>(null)
  const [runPlan, setRunPlan] = useState<BackupPlan | null>(null)
  const [check, setCheck] = useState<BackupDestination | null>(null)
  const [readData, setReadData] = useState<'metadata' | 'subset' | 'all'>('metadata')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [errorScope, setErrorScope] = useState<string | null>(null)
  const [jobIds, setJobIds] = useState<string[]>([])
  const [configurationRevision, setConfigurationRevision] = useState(0)
  const [recoveryRevision, setRecoveryRevision] = useState(0)
  const [policyRevision, setPolicyRevision] = useState(0)
  const openedPlanRef = useRef<string | null>(null)
  const [clock, setClock] = useState(() => Date.now())
  const data = state.data
  const destinations = data?.destinations ?? []
  const plans = data?.plans ?? []
  const runs = data?.runs ?? []
  const hasRunning = jobIds.length > 0 || runs.some((run) => ['queued', 'running'].includes(run.state))

  const finished = useCallback(
    (id: string) => {
      setJobIds((previous) => previous.filter((jobId) => jobId !== id))
      refetch()
      setRecoveryRevision((value) => value + 1)
      setPolicyRevision((value) => value + 1)
    },
    [refetch],
  )
  useEffect(() => {
    const timer = window.setInterval(
      () => {
        if (document.visibilityState === 'hidden') return
        setClock(Date.now())
        if (hasRunning) refetch()
      },
      hasRunning ? 5000 : 60000,
    )
    return () => window.clearInterval(timer)
  }, [hasRunning, refetch])

  function started(result: BackupJobResponse, reveal = true) {
    setJobIds((previous) => (previous.includes(result.job.id) ? previous : [...previous, result.job.id]))
    if (reveal) openJob(result.job.id)
    state.refetch()
  }

  async function command(action: () => Promise<unknown>, scope: string | null = null): Promise<boolean> {
    if (pending) return false
    setPending(true)
    setError(null)
    setErrorScope(scope)
    try {
      const result = await action()
      if (result && typeof result === 'object' && 'job' in result) started(result as BackupJobResponse)
      else state.refetch()
      return true
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Backup operation failed')
      return false
    } finally {
      setPending(false)
    }
  }

  const destinationName = (id: string) => destinations.find((item) => item.id === id)?.name ?? id
  const closeEditor = () => {
    setEditor(null)
    setSearch(
      (previous) => {
        previous.delete('stack')
        previous.delete('plan')
        return previous
      },
      { replace: true },
    )
  }
  const configurationChanged = useCallback(() => {
    refetch()
    setConfigurationRevision((value) => value + 1)
  }, [refetch])
  const refreshAll = useCallback(() => {
    configurationChanged()
    setRecoveryRevision((value) => value + 1)
    setPolicyRevision((value) => value + 1)
  }, [configurationChanged])
  const finishEditor = () => {
    closeEditor()
    configurationChanged()
  }
  const openRun = (plan: BackupPlan) => {
    setError(null)
    setRunPlan(plan)
  }
  const openReview = (value: Exclude<Review, null>) => {
    setError(null)
    setReview(value)
  }
  const hasDialog = !!runPlan || !!check || !!review || !!editor
  useEffect(() => {
    const id = search.get('plan')
    const plan = data?.plans.find((item) => item.id === id)
    if (!id) { openedPlanRef.current = null; return }
    if (openedPlanRef.current === id || !data) return
    openedPlanRef.current = id
    if (plan) setEditor({ kind: 'plan', value: plan })
    else setError('The linked backup plan no longer exists. Select an existing plan or create one.')
  }, [search, data])
  const recover = (id: string) => {
    setRecoveryRevision((value) => value + 1)
    setSearch((previous) => {
      previous.set('view', 'recovery')
      previous.set('destination', id)
      return previous
    })
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        kicker="Recovery"
        title="Backups"
        meta="Encrypted recovery sets with independent copies per destination"
        actions={
          <button className={buttonClass} disabled={state.loading} onClick={refreshAll}>
            Refresh
          </button>
        }
      />
      <BackupError error={hasDialog || errorScope ? null : error} />
      <div className="flex gap-2 overflow-x-auto" role="tablist" aria-label="Backup views">
        {tabs.map((item) => (
          <button
            key={item}
            id={`backup-tab-${item}`}
            aria-controls={`backup-panel-${item}`}
            role="tab"
            aria-selected={tab === item}
            tabIndex={tab === item ? 0 : -1}
            onKeyDown={(event) => {
              if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
              event.preventDefault()
              const index = tabs.indexOf(item)
              const next =
                event.key === 'Home'
                  ? tabs[0]
                  : event.key === 'End'
                    ? tabs[tabs.length - 1]
                    : tabs[(index + (event.key === 'ArrowRight' ? 1 : tabs.length - 1)) % tabs.length]
              setSearch((previous) => {
                previous.set('view', next)
                previous.delete('stack')
                previous.delete('plan')
                return previous
              })
              document.getElementById(`backup-tab-${next}`)?.focus()
            }}
            onClick={() =>
              setSearch((previous) => {
                previous.set('view', item)
                previous.delete('stack')
                previous.delete('plan')
                return previous
              })
            }
            className={cn(buttonClass, tab === item && 'border-[var(--accent)]/40 bg-[var(--accent)]/10')}
          >
            {item[0].toUpperCase() + item.slice(1)}
          </button>
        ))}
      </div>
      <AsyncState
        loading={state.loading && !data}
        error={state.error}
        hasData={data !== null}
        isEmpty={false}
        loadingLabel="Loading backups…"
        emptyMessage="No backups configured."
        onRetry={state.refetch}
        loadingFallback={<p className="text-sm text-[var(--muted)]">Loading backups…</p>}
      >
        {data && (
          <>
            {!data.restic_available && (
              <p
                role="alert"
                className="rounded-md border border-[var(--warning)]/30 p-3 text-sm text-[var(--warning)]"
              >
                Restic is unavailable on this host. Install the supported backup tools before running backups.
              </p>
            )}
            {data.helper_ready === false && (
              <p role="alert" className="rounded-md border border-[var(--warning)]/30 p-3 text-sm text-[var(--warning)]">
                In-place recovery is unavailable: {data.helper_error || 'The backup helper is not ready.'}
                {' '}Update the backup helper and its narrowly scoped sudoers permissions, then verify recovery readiness.
              </p>
            )}
            <BackupOperations
              refreshKey={policyRevision}
              recoveryError={data.recovery_error}
              onChanged={refreshAll}
              onStarted={started}
            />
            <div className="space-y-4">
              <div
                role="tabpanel"
                id="backup-panel-protection"
                aria-labelledby="backup-tab-protection"
                hidden={tab !== 'protection'}
              >
                {(mountedTabs.has('protection') || tab === 'protection') && (
                  <ProtectionDashboard
                    data={data}
                    refreshKey={configurationRevision}
                    active={tab === 'protection'}
                    pending={pending}
                    onCreatePlan={() => setEditor({ kind: 'plan' })}
                    onAddDestination={() => setEditor({ kind: 'destination' })}
                    onRun={openRun}
                    onRecovery={recover}
                  />
                )}
              </div>
              <div
                role="tabpanel"
                id="backup-panel-recovery"
                aria-labelledby="backup-tab-recovery"
                hidden={tab !== 'recovery'}
              >
                {(mountedTabs.has('recovery') || tab === 'recovery') && (
                  <RecoveryWorkspace
                    destinations={destinations}
                    refreshKey={recoveryRevision}
                    active={tab === 'recovery'}
                    helperReady={data.helper_ready}
                    helperError={data.helper_error}
                    initialDestinationId={search.get('destination') ?? undefined}
                    plans={plans}
                    toolsAvailable={data.restic_available}
                    rcloneAvailable={data.rclone_available}
                    onDestinationChange={(id) =>
                      setSearch(
                        (previous) => {
                          previous.set('destination', id)
                          return previous
                        },
                        { replace: true },
                      )
                    }
                    onStarted={started}
                  />
                )}
              </div>
              <div
                role="tabpanel"
                id="backup-panel-destinations"
                aria-labelledby="backup-tab-destinations"
                hidden={tab !== 'destinations'}
              >
                {(mountedTabs.has('destinations') || tab === 'destinations') && (
                  <BackupPanel
                    title="Destinations"
                    actions={
                      <button className={primaryClass} onClick={() => setEditor({ kind: 'destination' })}>
                        Add destination
                      </button>
                    }
                  >
                    {destinations.length === 0 && (
                      <p className="text-sm leading-6 text-[var(--muted)]">
                        Start by adding a local disk, NAS, S3 bucket or Google Drive repository. Keep repository
                        passwords and access credentials outside this host so recovery remains possible after host loss.
                      </p>
                    )}
                    <div className="space-y-3">
                      {destinations.map((destination) => (
                        <article
                          key={destination.id}
                          className="space-y-3 rounded-md border border-[var(--panel-border)] p-4"
                        >
                          <div className="flex flex-wrap items-center justify-between gap-2">
                            <h3 className="font-medium text-[var(--text)]">{destination.name}</h3>
                            <BackupStatus value={destination.status} />
                          </div>
                          <p className="break-all text-xs text-[var(--muted)]">
                            {destination.kind.toUpperCase()} · {destination.repository}
                          </p>
                          <p className="text-xs text-[var(--muted)]">
                            Repository data: <BackupSize value={destination.repository_size_bytes} />
                          </p>
                          {destination.kind === 'rclone' && !data.rclone_available && (
                            <p className="text-sm text-[var(--warning)]">Rclone is unavailable on this host.</p>
                          )}
                          <dl className="grid gap-2 text-xs sm:grid-cols-3">
                            <div>
                              <dt className="text-[var(--muted)]">Last successful copy</dt>
                              <dd className="mt-1 text-[var(--text)]">
                                <BackupDate value={destination.last_success_at} />
                              </dd>
                            </div>
                            <div>
                              <dt className="text-[var(--muted)]">
                                Last successful repository check{' '}
                                {destination.last_check_policy ? `(${destination.last_check_policy})` : ''}
                              </dt>
                              <dd className="mt-1 text-[var(--text)]">
                                <BackupDate value={destination.last_check_at} />
                              </dd>
                            </div>
                            <div>
                              <dt className="text-[var(--muted)]">Isolated restore test</dt>
                              <dd className="mt-1 text-[var(--text)]">
                                <BackupDate value={destination.last_restore_test_at} />
                              </dd>
                            </div>
                          </dl>
                          <dl className="grid gap-2 text-xs sm:grid-cols-3">
                            <div>
                              <dt className="text-[var(--muted)]">Snapshot inventory confirmed</dt>
                              <dd className="mt-1"><BackupDate value={destination.last_inventory_at} /></dd>
                            </div>
                            <div>
                              <dt className="text-[var(--muted)]">Latest inventory attempt</dt>
                              <dd className="mt-1"><BackupDate value={destination.last_inventory_attempt_at} /></dd>
                            </div>
                            <div>
                              <dt className="text-[var(--muted)]">Latest check attempt</dt>
                              <dd className="mt-1"><BackupDate value={destination.last_check_attempt_at} /> · {destination.check_status || 'not checked'}</dd>
                            </div>
                          </dl>
                          {destination.inventory_error && (
                            <p className="text-sm text-[var(--warning)]">Snapshot inventory: {destination.inventory_error}</p>
                          )}
                          {destination.check_error && (
                            <p className="text-sm text-[var(--warning)]">Integrity check: {destination.check_error}</p>
                          )}
                          {destination.last_operation_error && (
                            <p className="text-xs text-[var(--warning)]">
                              Last {destination.last_operation?.replaceAll('_', ' ')} operation:{' '}
                              {destination.last_operation_error}
                            </p>
                          )}
                          <p className="text-xs text-[var(--muted)]">
                            Checks: {scheduleDescription(destination.check_schedule ?? { frequency: 'manual' })} ·
                            Cleanup: {scheduleDescription(destination.prune_schedule ?? { frequency: 'manual' })}
                          </p>
                          <BackupError error={errorScope === destination.id ? error : null} />
                          {destination.error && <p className="text-sm text-[var(--danger)]">{destination.error}</p>}
                          <div className="flex flex-wrap gap-2">
                            <button
                              className={buttonClass}
                              disabled={
                                pending ||
                                !data.restic_available ||
                                (destination.kind === 'rclone' && !data.rclone_available)
                              }
                              onClick={() => {
                                void command(() => testBackupDestination(destination.id), destination.id)
                              }}
                            >
                              Test access
                            </button>
                            <button
                              className={buttonClass}
                              title={destination.status === 'available' ? 'This repository is already available; use Test access to verify it.' : undefined}
                              disabled={
                                pending ||
                                destination.status === 'available' ||
                                !data.restic_available ||
                                (destination.kind === 'rclone' && !data.rclone_available)
                              }
                              onClick={() => openReview({ kind: 'initialize', destination })}
                            >
                              Initialize
                            </button>
                            <button
                              className={buttonClass}
                              disabled={
                                pending ||
                                !data.restic_available ||
                                (destination.kind === 'rclone' && !data.rclone_available)
                              }
                              onClick={() => {
                                setError(null)
                                setReadData(destination.check_read_data === 'all' || destination.check_read_data === 'subset' ? destination.check_read_data : 'metadata')
                                setCheck(destination)
                              }}
                            >
                              Check repository
                            </button>
                            <button className={buttonClass} disabled={pending} onClick={() => recover(destination.id)}>
                              Browse & restore
                            </button>
                            <button
                              className={buttonClass}
                              disabled={
                                pending ||
                                !data.restic_available ||
                                (destination.kind === 'rclone' && !data.rclone_available)
                              }
                              onClick={() => {
                                openReview({ kind: 'prune', destination })
                              }}
                            >
                              Run retention cleanup
                            </button>
                            <button
                              className={buttonClass}
                              disabled={pending}
                              onClick={() => setEditor({ kind: 'destination', value: destination })}
                            >
                              Edit
                            </button>
                            <button
                              className={buttonClass}
                              disabled={pending}
                              onClick={() => openReview({ kind: 'deleteDestination', destination })}
                            >
                              Remove
                            </button>
                          </div>
                        </article>
                      ))}
                    </div>
                  </BackupPanel>
                )}
              </div>
              <div role="tabpanel" id="backup-panel-plans" aria-labelledby="backup-tab-plans" hidden={tab !== 'plans'}>
                {(mountedTabs.has('plans') || tab === 'plans') && (
                  <BackupPanel
                    title="Backup plans"
                    actions={
                      <button className={primaryClass} onClick={() => setEditor({ kind: 'plan' })}>
                        Create plan
                      </button>
                    }
                  >
                    {plans.length === 0 && (
                      <p className="text-sm text-[var(--muted)]">
                        Select stack definitions, service data or Stacklab recovery, then choose destinations and
                        schedules.
                      </p>
                    )}
                    <div className="space-y-3">
                      {plans.map((plan) => (
                        <article key={plan.id} className="space-y-3 rounded-md border border-[var(--panel-border)] p-4">
                          <div className="flex flex-wrap items-center justify-between gap-2">
                            <h3 className="font-medium text-[var(--text)]">{plan.name}</h3>
                            <BackupStatus value={plan.enabled ? 'enabled' : 'disabled'} />
                          </div>
                          <p className="text-sm text-[var(--muted)]">
                            {plan.sources.length} sources ·{' '}
                            {plan.consistency === 'stop'
                              ? 'Stop writers during capture'
                              : 'Live files / prepared dumps'}{' '}
                            · {(plan.staging_max_bytes / 1024 ** 3).toLocaleString()} GiB staging budget
                          </p>
                          <ul className="space-y-1 text-xs text-[var(--muted)]">
                            {plan.destinations.map((assignment) => (
                              <li key={assignment.destination_id}>
                                {destinationName(assignment.destination_id)} ·{' '}
                                {assignment.enabled ? scheduleDescription(assignment.schedule) : 'disabled'} ·{' '}
                                {Object.values(assignment.retention).every((count) => count === 0)
                                  ? 'Keep all snapshots (no retention)'
                                  : `Keep ${assignment.retention.daily} daily / ${assignment.retention.weekly} weekly / ${assignment.retention.monthly} monthly`}
                              </li>
                            ))}
                          </ul>
                          <details>
                            <summary className="cursor-pointer text-sm text-[var(--muted)]">Selected data</summary>
                            <ul className="mt-2 space-y-2 text-sm text-[var(--text)]">
                              {plan.sources.map((source) => (
                                <li key={source.id}>
                                  <SourceLabel source={source} />
                                  {(source.excludes?.length ?? 0) > 0 && (
                                    <p className="text-xs text-[var(--warning)]">
                                      Excludes: {source.excludes?.join(', ')}
                                    </p>
                                  )}
                                </li>
                              ))}
                            </ul>
                          </details>
                          <div className="flex flex-wrap gap-2">
                            <button
                              className={primaryClass}
                              disabled={pending || !data.restic_available}
                              onClick={() => openRun(plan)}
                            >
                              Back up now
                            </button>
                            <button
                              className={buttonClass}
                              disabled={pending}
                              onClick={() => setEditor({ kind: 'plan', value: plan })}
                            >
                              Edit plan
                            </button>
                            <button
                              className={buttonClass}
                              disabled={pending}
                              onClick={() => openReview({ kind: 'deletePlan', plan })}
                            >
                              Remove plan
                            </button>
                          </div>
                        </article>
                      ))}
                    </div>
                  </BackupPanel>
                )}
              </div>
              <div
                role="tabpanel"
                id="backup-panel-history"
                aria-labelledby="backup-tab-history"
                hidden={tab !== 'history'}
              >
                {(mountedTabs.has('history') || tab === 'history') && (
                  <RunHistory
                    runs={runs}
                    error={errorScope === 'history' ? error : null}
                    plans={plans}
                    destinations={destinations}
                    clock={clock}
                    pending={pending}
                    onJob={openJob}
                    onRetry={(run, ids) => {
                      void command(() => retryBackupRun(run.id, ids), 'history')
                    }}
                  />
                )}
              </div>
            </div>
          </>
        )}
      </AsyncState>
      {jobIds.map((id) => (
        <BackupJobObserver key={id} id={id} onFinished={finished} />
      ))}
      {editor?.kind === 'destination' && (
        <DestinationEditor
          destination={editor.value}
          toolsAvailable={data?.restic_available ?? false}
          rcloneAvailable={data?.rclone_available ?? false}
          onClose={closeEditor}
          onSaved={finishEditor}
          onStarted={(result) => started(result, false)}
          onCreatePlan={(destination) => {
            configurationChanged()
            setEditor({ kind: 'plan', destinationId: destination.id })
          }}
        />
      )}
      {editor?.kind === 'plan' && (
        <PlanEditor
          plan={editor.value}
          destinations={destinations}
          initialStackId={search.get('stack') ?? undefined}
          initialDestinationId={editor.destinationId}
          onClose={closeEditor}
          onSaved={(plan) => {
            finishEditor()
            if (plan) openRun(plan)
          }}
        />
      )}
      {runPlan && (
        <RunPlanDialog
          plan={runPlan}
          destinations={destinations}
          pending={pending}
          error={error}
          onClose={() => {
            setRunPlan(null)
            setError(null)
          }}
          onRun={async (ids) => {
            if (await command(() => runBackupPlan(runPlan.id, ids))) setRunPlan(null)
          }}
        />
      )}
      {check && (
        <Dialog
          title={`Check ${check.name}`}
          onClose={() => {
            if (!pending) {
              setCheck(null)
              setError(null)
            }
          }}
          preventClose={pending}
          busy={pending}
        >
          <div className="mt-4 space-y-4">
            <BackupError error={error} />
            <Field label="Data-read policy">
              <select
                className={inputClass}
                value={readData}
                disabled={pending}
                onChange={(event) => setReadData(event.target.value as typeof readData)}
              >
                <option value="metadata">Repository metadata only</option>
                <option value="subset">Read a subset of stored data</option>
                <option value="all">Read all stored data</option>
              </select>
            </Field>
            <p className="text-sm text-[var(--muted)]">
              Reading stored data can incur provider download charges. A repository check is separate from an actual
              restore test.
            </p>
            <div className="flex justify-end gap-2">
              <button
                className={buttonClass}
                disabled={pending}
                onClick={() => {
                  setCheck(null)
                  setError(null)
                }}
              >
                Cancel
              </button>
              <button
                className={primaryClass}
                disabled={pending}
                onClick={() => {
                  void command(() => checkBackupDestination(check.id, readData)).then((ok) => {
                    if (ok) setCheck(null)
                  })
                }}
              >
                {pending ? 'Starting…' : 'Start check'}
              </button>
            </div>
          </div>
        </Dialog>
      )}
      {review && (
        <ConfirmDialog
          title={
            review.kind === 'initialize'
              ? 'Initialize new repository'
              : review.kind === 'prune'
                ? 'Run retention cleanup'
                : review.kind === 'deletePlan'
                  ? 'Remove backup plan'
                  : 'Remove destination'
          }
          message={
            review.kind === 'initialize'
              ? 'Create an encrypted restic repository at the configured location. Only initialize a new repository; test access to an existing one.'
              : review.kind === 'prune'
                ? 'Reclaim storage no longer referenced by retained snapshots. Cleanup locks this repository and may take time or incur provider transfer charges.'
                : 'Remove this configuration from Stacklab. Existing repository snapshots remain available with independently saved credentials.'
          }
          error={error}
          confirmLabel={
            review.kind === 'initialize'
              ? 'Initialize repository'
              : review.kind === 'prune'
                ? 'Start cleanup'
                : 'Remove configuration'
          }
          confirming={pending}
          requireText={
            review.kind === 'initialize' || review.kind === 'prune'
              ? undefined
              : review.kind === 'deletePlan'
                ? review.plan.name
                : review.destination.name
          }
          items={[review.kind === 'deletePlan' ? review.plan.name : review.destination.repository]}
          onCancel={() => {
            if (!pending) {
              setReview(null)
              setError(null)
            }
          }}
          onConfirm={() => {
            const action =
              review.kind === 'initialize'
                ? () => initializeBackupDestination(review.destination.id)
                : review.kind === 'prune'
                  ? () => pruneBackupDestination(review.destination.id)
                  : review.kind === 'deletePlan'
                    ? () => deleteBackupPlan(review.plan.id)
                    : () => deleteBackupDestination(review.destination.id)
            void command(action).then((ok) => {
              if (ok) { setReview(null); configurationChanged() }
            })
          }}
        />
      )}
    </div>
  )
}

function BackupJobObserver({ id, onFinished }: { id: string; onFinished: (id: string) => void }) {
  const { state } = useJobStream({ jobId: id })
  useEffect(() => {
    if (['succeeded', 'failed', 'cancelled', 'timed_out'].includes(state ?? '')) onFinished(id)
  }, [id, state, onFinished])
  return null
}
