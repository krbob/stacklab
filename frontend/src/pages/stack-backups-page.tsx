import { Link, useParams } from 'react-router-dom'
import { AsyncState } from '@/components/async-state'
import { useApi } from '@/hooks/use-api'
import { getStackBackups } from '@/lib/backup-api'
import { BackupPanel, BackupStatus, buttonClass, primaryClass } from './backups/backup-primitives'
import { SourceLabel } from './backups/source-label'
import { sourceCoverage } from './backups/source-coverage'

export function StackBackupsPage() {
  const { stackId = '' } = useParams()
  const state = useApi(() => getStackBackups(stackId), [stackId])
  const data = state.data
  return (
    <div className="space-y-4">
      <BackupPanel
        title="Backup coverage"
        actions={
          <>
            <Link className={primaryClass} to={`/backups?view=plans&stack=${encodeURIComponent(stackId)}`}>
              Manage backup plans
            </Link>
            <Link className={buttonClass} to="/backups?view=recovery">
              Recover data
            </Link>
          </>
        }
      >
        <p className="mb-4 text-sm leading-6 text-[var(--muted)]">
          Review each persistent mount and definition explicitly. New or changed data needs a plan review. Coverage
          shows configured selection; successful copies and restore tests are shown in Backups.
        </p>
        <AsyncState
          loading={state.loading}
          error={state.error}
          hasData={data !== null}
          isEmpty={data?.sources.length === 0}
          loadingLabel="Discovering stack backup sources…"
          emptyMessage="No selectable backup sources found."
          onRetry={state.refetch}
          loadingFallback={<p className="text-sm text-[var(--muted)]">Discovering sources…</p>}
        >
          <div className="space-y-3">
            {data?.sources.map((source) => {
              const { selected, state: status } = sourceCoverage(source, data.plans)
              return (
                <div key={source.id} className="rounded-md border border-[var(--panel-border)] p-3">
                  <div className="flex flex-wrap items-start justify-between gap-2 text-sm text-[var(--text)]">
                    <SourceLabel source={source} />
                    <BackupStatus value={status} />
                  </div>
                  {status === 'source_changed' && (
                    <p className="mt-2 text-xs text-[var(--warning)]">
                      The mount identity changed. Re-select this source in the plan before the next capture.
                    </p>
                  )}
                  {status === 'partial' && (
                    <p className="mt-2 text-xs text-[var(--warning)]">
                      The plan selects a narrower configuration path; review the selected content and exclusions.
                    </p>
                  )}
                  {selected.map((selection) => (
                    <p key={selection.plan.id} className="mt-2 text-xs text-[var(--muted)]">
                      <Link
                        className="text-[var(--accent)] underline underline-offset-4"
                        to={`/backups?view=plans&plan=${encodeURIComponent(selection.plan.id)}`}
                      >
                        {selection.plan.name}
                      </Link>{' '}
                      · {selection.plan.consistency === 'stop' ? 'Stop writers' : 'Live file capture'}
                      {selection.source.excludes?.length ? ` · Excluded: ${selection.source.excludes.join(', ')}` : ''}
                    </p>
                  ))}
                </div>
              )
            })}
          </div>
        </AsyncState>
      </BackupPanel>
      <p className="text-xs leading-5 text-[var(--muted)]">
        Container writable layers and image layers are outside this selection. Repository integrity checks and isolated
        restore tests are tracked separately for each destination.
      </p>
    </div>
  )
}
