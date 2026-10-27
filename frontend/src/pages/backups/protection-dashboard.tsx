import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useApi } from '@/hooks/use-api'
import { getBackupCoverage, type BackupDestination, type BackupOverview, type BackupPlan } from '@/lib/backup-api'
import { BackupDate, BackupError, BackupPanel, BackupStatus, buttonClass, primaryClass } from './backup-primitives'
import { SourceLabel } from './source-label'
import { sourceCoverage } from './source-coverage'
import { repositoryToolsAvailable } from './repository-evidence'
import { CheckHoldWarning } from './check-hold-warning'

export function ProtectionDashboard({
  data,
  refreshKey,
  active = true,
  pending = false,
  captureBlocked,
  onCreatePlan,
  onAddDestination,
  onRun,
  onRecovery,
  onCheck,
}: {
  data: BackupOverview
  refreshKey?: number | null
  active?: boolean
  pending?: boolean
  captureBlocked?: string
  onCreatePlan: () => void
  onAddDestination: () => void
  onRun: (plan: BackupPlan) => void
  onRecovery: (destinationId: string) => void
  onCheck?: (destination: BackupDestination) => void
}) {
  const [coverageRevision, setCoverageRevision] = useState(refreshKey)
  useEffect(() => {
    if (active) setCoverageRevision(refreshKey)
  }, [active, refreshKey])
  const coverage = useApi((signal) => getBackupCoverage(signal), [coverageRevision], { resourceKey: 'backup-coverage' })
  const protection = data.protection ?? []
  const copies = protection.flatMap((plan) => plan.copies)
  const current = copies.filter((copy) => copy.state === 'current').length
  const manual = copies.filter((copy) => copy.state === 'manual').length
  const attention = copies.filter(
    (copy) => copy.state !== 'disabled' && (
      [
        'overdue',
        'outdated',
        'unprotected',
        'missing',
        'integrity_failed',
        'unavailable',
        'clock_skew',
        'unverified',
      ].includes(copy.state) || !!copy.last_error),
  ).length
  const incomplete = !!coverage.error || (coverage.data?.stacks ?? []).some((stack) => !!stack.error)
  const gaps = (coverage.data?.stacks ?? [])
    .flatMap((stack) => stack.sources.map((source) => ({ stack, source, ...sourceCoverage(source, stack.plans) })))
    .filter((item) => item.state !== 'configured')
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[
          { label: 'Copies meeting schedule', value: current },
          { label: 'Manual copies available', value: manual },
          { label: 'Copies needing attention', value: attention },
          {
            label: 'Source selection gaps',
            value: coverage.loading && !coverage.data ? '…' : incomplete ? `${gaps.length}+` : gaps.length,
          },
        ].map((item) => (
          <div key={item.label} className="flex flex-col justify-between gap-2 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-4">
            <p className="text-xs text-[var(--muted)]">{item.label}</p>
            <p className="text-2xl font-semibold text-[var(--text)]">{item.value}</p>
          </div>
        ))}
      </div>
      <BackupPanel
        title="Protection status"
        actions={
          <>
            <button className={buttonClass} onClick={onAddDestination}>
              Add destination
            </button>
            <button className={primaryClass} onClick={onCreatePlan}>
              Create plan
            </button>
          </>
        }
      >
        <p className="mb-4 text-sm leading-6 text-[var(--muted)]">
          A selected source is configuration. Protection requires a verified copy of that selection. Manual copies have
          no automatic freshness expectation; repository checks and recovery tests are tracked separately.
        </p>
        {data.plans.length === 0 && (
          <p className="text-sm text-[var(--muted)]">
            Start with a destination, select the data that matters, then run your first backup.
          </p>
        )}
        <div className="space-y-3">
          {data.plans.map((plan) => {
            const health = protection.find((item) => item.plan_id === plan.id)
            return (
              <article key={plan.id} className="space-y-3 rounded-md border border-[var(--panel-border)] p-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <h3 className="text-sm font-semibold text-[var(--text)]">{plan.name}</h3>
                  <div className="flex flex-wrap gap-2"><BackupStatus value={health?.state ?? (plan.enabled ? 'unprotected' : 'disabled')} />
                    {health?.activity && health.activity !== 'idle' && <BackupStatus value={health.activity} />}
                  </div>
                </div>
                <p className="text-sm text-[var(--muted)]">
                  {health
                    ? `${health.current_copies} of ${health.expected_copies} configured copies available`
                    : 'No verified copy recorded for this plan.'}{' '}
                  · {plan.sources.length} selected sources
                </p>
                <div className="space-y-2">
                  {(health?.copies ?? []).map((copy) => {
                    const destination = data.destinations.find((item) => item.id === copy.destination_id)
                    return (
                      <div key={copy.destination_id} className="space-y-3 rounded-md bg-[var(--bg)] p-3">
                        <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
                          <span className="text-[var(--text)]">{destination?.name || copy.destination_id}</span>
                          <div className="flex flex-wrap gap-2"><BackupStatus value={copy.state} />
                            {copy.activity && copy.activity !== 'idle' && <BackupStatus value={copy.activity} />}
                          </div>
                        </div>
                        {copy.evidence_usable !== undefined && copy.state !== 'disabled' && !destination?.check_hold && (
                          <p className="text-xs text-[var(--muted)]">
                            {copy.evidence_usable ? 'Saved copy has usable recovery evidence.' : 'Saved copy does not have usable recovery evidence.'}
                          </p>
                        )}
                        {destination && <CheckHoldWarning destination={destination} compact />}
                        <dl className="grid gap-3 text-xs sm:grid-cols-3">
                          <div>
                            <dt className="text-[var(--muted)]">Latest data captured</dt>
                            <dd className="mt-1">
                              <BackupDate value={copy.last_capture_at} />
                            </dd>
                          </div>
                          <div>
                            <dt className="text-[var(--muted)]">Next scheduled copy</dt>
                            <dd className="mt-1">
                              {copy.next_run_at ? <BackupDate value={copy.next_run_at} /> : 'Manual / disabled'}
                            </dd>
                          </div>
                          <div>
                            <dt className="text-[var(--muted)]">Recovery test of this snapshot</dt>
                            <dd className="mt-1">
                              <BackupDate value={copy.last_restore_test_at} />
                            </dd>
                          </div>
                        </dl>
                        <dl className="grid gap-3 text-xs sm:grid-cols-3">
                          <div>
                            <dt className="text-[var(--muted)]">Snapshot presence</dt>
                            <dd className="mt-1">
                              {copy.snapshot_status ?? 'Unknown'}
                              {copy.snapshot_checked_at && (
                                <>
                                  {' '}
                                  · observed <BackupDate value={copy.snapshot_checked_at} />
                                </>
                              )}
                            </dd>
                          </div>
                          <div>
                            <dt className="text-[var(--muted)]">Repository availability</dt>
                            <dd className="mt-1">{copy.availability_status ?? 'Unknown'}</dd>
                          </div>
                          <div>
                            <dt className="text-[var(--muted)]">Repository integrity</dt>
                            <dd className="mt-1">{copy.integrity_status ?? 'Not checked'}</dd>
                          </div>
                        </dl>
                        {copy.state === 'missing' && (
                          <p className="text-xs text-[var(--warning)]">
                            The latest recorded snapshot is missing from the repository. Create a new copy and
                            investigate retention or external changes.
                          </p>
                        )}
                        {copy.state === 'integrity_failed' && (
                          <p className="text-xs text-[var(--warning)]">
                            A repository integrity check failed. Inspect the check result before relying on this copy.
                          </p>
                        )}
                        {copy.state === 'unavailable' && (
                          <p className="text-xs text-[var(--warning)]">
                            Repository access failed. Check its availability and credentials; this does not establish
                            whether its data is intact.
                          </p>
                        )}
                        {copy.state === 'clock_skew' && (
                          <p className="text-xs text-[var(--warning)]">
                            Recorded backup times are ahead of the host clock. Review time synchronization before
                            assessing protection.
                          </p>
                        )}
                        {copy.state === 'unverified' && !destination?.check_hold && (
                          <p className="text-xs text-[var(--warning)]">
                            The complete recovery objects have not been verified. Create a new verified copy.
                          </p>
                        )}
                        {copy.state === 'manual' && (
                          <p className="text-xs text-[var(--muted)]">
                            Copy available. Manual schedule has no freshness guarantee.
                          </p>
                        )}
                        {(copy.overdue_since || copy.state === 'overdue') && (
                          <p className="text-xs text-[var(--warning)]">
                            Scheduled copy missing since <BackupDate value={copy.first_outstanding_due ?? copy.due_at} />.{copy.overdue_since && <> Overdue since <BackupDate value={copy.overdue_since} />.</>}
                          </p>
                        )}
                        {copy.state === 'outdated' && (
                          <p className="text-xs text-[var(--warning)]">
                            Plan selection changed after the latest copy. Run a new capture.
                          </p>
                        )}
                        {copy.last_error && copy.last_error !== destination?.check_hold?.error && <p className="text-xs text-[var(--warning)]">{copy.last_error}</p>}
                        {(destination?.check_hold && onCheck || copy.snapshot_id) && (
                          <div className="flex flex-wrap gap-2">
                            {destination?.check_hold && onCheck && <button className={buttonClass} disabled={pending || !repositoryToolsAvailable(destination, data)} onClick={() => onCheck(destination)}>Check {destination.name} repository</button>}
                            {copy.snapshot_id && <button className={buttonClass} onClick={() => onRecovery(copy.destination_id)}>Recover data</button>}
                          </div>
                        )}
                      </div>
                    )
                  })}
                </div>
                <button className={buttonClass} disabled={pending || !data.restic_available || !!captureBlocked} aria-describedby={captureBlocked ? 'backup-capture-block' : undefined} onClick={() => onRun(plan)}>
                  Back up now
                </button>
              </article>
            )
          })}
        </div>
      </BackupPanel>
      <BackupPanel title="Source selection gaps">
        <BackupError error={coverage.error?.message ?? null} />
        {coverage.error && (
          <button className={buttonClass} onClick={coverage.refetch}>
            Retry source inventory
          </button>
        )}
        {coverage.loading && !coverage.data && (
          <p role="status" className="text-sm text-[var(--muted)]">
            Discovering stack and host sources…
          </p>
        )}
        {coverage.data?.stacks
          .filter((stack) => stack.error)
          .map((stack) => (
            <p key={stack.stack_id} role="alert" className="text-sm text-[var(--warning)]">
              {stack.stack_name}: {stack.error}
            </p>
          ))}
        {incomplete && (
          <p className="mb-3 text-sm text-[var(--warning)]">
            Source inventory is incomplete. Resolve discovery errors before assessing selection gaps.
          </p>
        )}
        {coverage.data && !incomplete && gaps.length === 0 && (
          <p className="text-sm text-[var(--muted)]">
            All discovered sources are selected by enabled plans. Check copy status above for successful protection.
          </p>
        )}
        <div className="space-y-2">
          {gaps.map(({ stack, source, state }) => (
            <div
              key={`${stack.stack_id}:${source.id}`}
              className="flex flex-wrap items-start justify-between gap-3 rounded-md border border-[var(--panel-border)] p-3 text-sm"
            >
              <div className="min-w-0">
                <p className="mb-1 text-xs text-[var(--muted)]">{stack.stack_name || 'Stacklab host'}</p>
                <SourceLabel source={source} />
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <BackupStatus value={state} />
                <Link className={buttonClass} to={`/backups?view=plans&stack=${encodeURIComponent(stack.stack_id)}`}>
                  Review plans
                </Link>
              </div>
            </div>
          ))}
        </div>
      </BackupPanel>
    </div>
  )
}
