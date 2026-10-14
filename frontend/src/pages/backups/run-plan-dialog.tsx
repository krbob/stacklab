import { useState } from 'react'
import { Dialog } from '@/components/dialog'
import { useApi } from '@/hooks/use-api'
import { getBackupPreflight, type BackupDestination, type BackupPlan } from '@/lib/backup-api'
import { BackupError, buttonClass, primaryClass } from './backup-primitives'

export function RunPlanDialog({
  plan,
  destinations,
  pending,
  error,
  blockedReason,
  onClose,
  onRun,
}: {
  plan: BackupPlan
  destinations: BackupDestination[]
  pending: boolean
  error: string | null
  blockedReason?: string
  onClose: () => void
  onRun: (ids: string[]) => Promise<void>
}) {
  const [selected, setSelected] = useState(
    plan.destinations.filter((assignment) => assignment.enabled).map((assignment) => assignment.destination_id),
  )
  const [reviewed, setReviewed] = useState(false)
  const preflight = useApi((signal) => getBackupPreflight(plan.id, signal), [plan.id])
  const writers = (preflight.data?.writers ?? []).filter((writer) => writer.running && !writer.read_only)
  return (
    <Dialog title={`Back up ${plan.name}`} onClose={onClose} preventClose={pending} busy={pending}>
      <div className="mt-4 space-y-4">
        <BackupError error={error ?? preflight.error?.message ?? null} />
        {blockedReason && <p role="status" className="text-sm text-[var(--warning)]">New capture is blocked: {blockedReason}</p>}
        <p className="text-sm text-[var(--muted)]">
          Capture {plan.sources.length} selected sources once and deliver to the selected repositories.
        </p>
        {plan.sources.some((source) => source.excludes?.some((pattern) => pattern.trim())) && <p className="text-sm text-[var(--warning)]">
          This plan excludes files. Exact replacement is unavailable for those sources. Database recovery needs every
          required file; merging preserves newer files, and an empty target cannot recover excluded database files.
        </p>}
        {preflight.loading && (
          <p role="status" className="text-sm text-[var(--muted)]">
            Checking source paths and affected services…
          </p>
        )}
        {preflight.error && (
          <button
            className={buttonClass}
            onClick={() => {
              setReviewed(false)
              preflight.refetch()
            }}
          >
            Retry preflight
          </button>
        )}
        {preflight.data && (
          <section className="space-y-2 rounded-md border border-[var(--panel-border)] p-3">
            <h3 className="text-sm font-medium text-[var(--text)]">
              {plan.consistency === 'stop' ? 'Services to stop during capture' : 'Active writers in the selection'}
            </h3>
            {writers.length ? (
              <ul className="space-y-1 text-sm text-[var(--text)]">
                {writers.map((writer) => (
                  <li key={writer.container_id}>
                    {writer.stack_id || 'External container'} / {writer.service || writer.container_id}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-[var(--muted)]">No running writers found.</p>
            )}
            <p className="text-xs text-[var(--muted)]">
              Includes writers of shared data. Service state is checked again when capture starts.
            </p>
            {preflight.data.warnings?.map((warning) => (
              <p className="text-xs text-[var(--warning)]" key={warning}>
                {warning}
              </p>
            ))}
          </section>
        )}
        <fieldset disabled={pending || preflight.loading || !!preflight.error} className="space-y-2">
          {plan.destinations
            .filter((assignment) => assignment.enabled)
            .map((assignment) => (
              <label key={assignment.destination_id} className="flex items-center gap-2 text-sm text-[var(--text)]">
                <input
                  type="checkbox"
                  checked={selected.includes(assignment.destination_id)}
                  onChange={(event) =>
                    setSelected((previous) =>
                      event.target.checked
                        ? [...previous, assignment.destination_id]
                        : previous.filter((id) => id !== assignment.destination_id),
                    )
                  }
                />
                {destinations.find((destination) => destination.id === assignment.destination_id)?.name ??
                  assignment.destination_id}
              </label>
            ))}
          <label className="mt-4 flex items-start gap-2 text-sm text-[var(--warning)]">
            <input
              type="checkbox"
              className="mt-1"
              checked={reviewed}
              onChange={(event) => setReviewed(event.target.checked)}
            />
            {plan.consistency === 'stop'
              ? 'I understand that the listed running services stop during local capture and restart before upload.'
              : 'I reviewed live-copy consistency: active database directories require prepared dumps or stopped writers.'}
          </label>
        </fieldset>
        <div className="flex justify-end gap-2">
          <button className={buttonClass} disabled={pending} onClick={onClose}>
            Cancel
          </button>
          <button
            className={primaryClass}
            disabled={
              pending || !!blockedReason || !preflight.data || preflight.loading || !!preflight.error || !reviewed || selected.length === 0
            }
            onClick={() => {
              if (!blockedReason) void onRun(selected)
            }}
          >
            {pending ? 'Starting…' : 'Start backup'}
          </button>
        </div>
      </div>
    </Dialog>
  )
}
