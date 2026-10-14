import { useMemo, useState, type FormEvent } from 'react'
import { Dialog } from '@/components/dialog'
import { getStacks } from '@/lib/api-client'
import {
  getBackupSources,
  saveBackupPlan,
  type BackupAssignment,
  type BackupDestination,
  type BackupPlan,
  type BackupSource,
} from '@/lib/backup-api'
import { useApi } from '@/hooks/use-api'
import { BackupError, buttonClass, Field, inputClass, primaryClass } from './backup-primitives'
import { SourceLabel } from './source-label'
import { ScheduleFields } from './schedule-fields'
import { useEditorCloseGuard } from './editor-close-guard'

const gib = 1024 ** 3

export function PlanEditor({
  plan,
  destinations,
  initialStackId,
  initialDestinationId,
  onSaved,
  onClose,
}: {
  plan?: BackupPlan
  destinations: BackupDestination[]
  initialStackId?: string
  initialDestinationId?: string
  onSaved: (plan?: BackupPlan) => void
  onClose: () => void
}) {
  const [name, setName] = useState(plan?.name ?? '')
  const [enabled, setEnabled] = useState(plan?.enabled ?? true)
  const [consistency, setConsistency] = useState<BackupPlan['consistency']>(plan?.consistency ?? 'stop')
  const [sourceSearch, setSourceSearch] = useState('')
  const [stackId, setStackId] = useState(initialStackId ?? '')
  const [sources, setSources] = useState<BackupSource[]>(plan?.sources ?? [])
  const [assignments, setAssignments] = useState<BackupAssignment[]>(
    plan?.destinations ??
      (initialDestinationId
        ? [
            {
              destination_id: initialDestinationId,
              enabled: true,
              schedule: { frequency: 'manual' },
              retention: { daily: 7, weekly: 4, monthly: 12 },
            },
          ]
        : []),
  )
  const [maxGiB, setMaxGiB] = useState((plan?.staging_max_bytes ?? 10 * gib) / gib)
  const [ttl, setTtl] = useState(plan?.staging_ttl_hours ?? 24)
  const [stopTimeout, setStopTimeout] = useState(plan?.stop_timeout_seconds ?? 300)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const formValue = JSON.stringify({ name, enabled, consistency, sources, assignments, maxGiB, ttl, stopTimeout })
  const [initialValue] = useState(formValue)
  const { requestClose, confirmation } = useEditorCloseGuard(formValue !== initialValue, onClose)
  const stacks = useApi((signal) => getStacks(undefined, signal), [])
  const inventory = useApi((signal) => getBackupSources(stackId || undefined, signal), [stackId])
  const available = useMemo(() => inventory.data?.items ?? [], [inventory.data])

  const visibleSources = available.filter((source) =>
    [source.kind, source.service, source.mount, source.path]
      .join(' ')
      .toLowerCase()
      .includes(sourceSearch.toLowerCase()),
  )

  function toggleSource(source: BackupSource) {
    setSources((previous) =>
      previous.some((item) => item.id === source.id)
        ? previous.filter((item) => item.id !== source.id)
        : [...previous, { ...source, excludes: [] }],
    )
  }

  function toggleDestination(id: string) {
    setAssignments((previous) =>
      previous.some((item) => item.destination_id === id)
        ? previous.filter((item) => item.destination_id !== id)
        : [
            ...previous,
            {
              destination_id: id,
              enabled: true,
              schedule: { frequency: 'manual' },
              retention: { daily: 7, weekly: 4, monthly: 12 },
            },
          ],
    )
  }

  function updateAssignment(id: string, patch: Partial<BackupAssignment>) {
    setAssignments((previous) => previous.map((item) => (item.destination_id === id ? { ...item, ...patch } : item)))
  }

  async function save(event: FormEvent) {
    event.preventDefault()
    if (pending) return
    if (!sources.length || !assignments.some((assignment) => assignment.enabled)) {
      setError('Select at least one source and one enabled destination.')
      return
    }
    setPending(true)
    setError(null)
    try {
      const result = await saveBackupPlan(
        {
          name: name.trim(),
          enabled,
          consistency,
          sources: sources.map((source) => ({
            ...source,
            excludes: source.excludes?.map((pattern) => pattern.trim()).filter(Boolean),
          })),
          destinations: assignments,
          staging_max_bytes: Math.round(maxGiB * gib),
          staging_ttl_hours: ttl,
          stop_timeout_seconds: stopTimeout,
        },
        plan?.id,
      )
      onSaved(plan ? undefined : result.plan)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to save plan')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog
      title={plan ? 'Edit backup plan' : 'Create backup plan'}
      onClose={requestClose}
      preventClose={pending}
      busy={pending}
      panelClassName="max-w-3xl"
    >
      <form
        onSubmit={(event) => {
          void save(event)
        }}
        className="mt-4 space-y-5"
      >
        <BackupError error={error} />
        <fieldset disabled={pending} className="space-y-5">
          <div className="grid items-end gap-4 sm:grid-cols-2">
            <Field label="Plan name">
              <input required className={inputClass} value={name} onChange={(event) => setName(event.target.value)} />
            </Field>
            <label className="flex items-center gap-2 py-2 text-sm text-[var(--text)]">
              <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} /> Enable
              plan schedules
            </label>
          </div>
          <section className="space-y-3" aria-label="Backup sources">
            <h3 className="text-sm font-semibold text-[var(--text)]">Select data</h3>
            <Field label="Source scope">
              <select className={inputClass} value={stackId} onChange={(event) => setStackId(event.target.value)}>
                <option value="">Stacklab recovery / managed configuration</option>
                {(stacks.data?.items ?? []).map((stack) => (
                  <option key={stack.id} value={stack.id}>
                    {stack.name}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Search sources">
              <input
                type="search"
                className={inputClass}
                value={sourceSearch}
                onChange={(event) => setSourceSearch(event.target.value)}
                placeholder="Service, mount or path"
              />
            </Field>
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs text-[var(--muted)]">{sources.length} selected across scopes</span>
              <button
                type="button"
                className={buttonClass}
                disabled={!visibleSources.length}
                onClick={() =>
                  setSources((previous) => [
                    ...previous,
                    ...visibleSources
                      .filter((source) => !previous.some((item) => item.id === source.id))
                      .map((source) => ({ ...source, excludes: [] })),
                  ])
                }
              >
                Select visible sources
              </button>
            </div>
            {stacks.error && (
              <p role="alert" className="text-sm text-[var(--danger)]">
                Unable to list stacks: {stacks.error.message}{' '}
                <button type="button" className={buttonClass} onClick={stacks.refetch}>
                  Retry stacks
                </button>
              </p>
            )}
            {inventory.loading ? (
              <p role="status" className="text-sm text-[var(--muted)]">
                Discovering sources…
              </p>
            ) : inventory.error ? (
              <p role="alert" className="text-sm text-[var(--danger)]">
                {inventory.error.message}{' '}
                <button type="button" className={buttonClass} onClick={inventory.refetch}>
                  Retry discovery
                </button>
              </p>
            ) : (
              <div className="space-y-2">
                {visibleSources.map((source) => (
                  <label
                    key={source.id}
                    className="flex items-start gap-3 rounded-md border border-[var(--panel-border)] p-3 text-sm text-[var(--text)]"
                  >
                    <input
                      type="checkbox"
                      className="mt-1"
                      checked={sources.some((item) => item.id === source.id)}
                      onChange={() => toggleSource(source)}
                    />
                    <SourceLabel source={source} />
                  </label>
                ))}
                {visibleSources.length === 0 && (
                  <p className="text-sm text-[var(--muted)]">No selectable sources in this scope.</p>
                )}
              </div>
            )}
            <p className="text-xs leading-5 text-[var(--muted)]">
              New mounts are never selected automatically. Definitions include the entire stack directory: .env,
              supporting files and any stack-local data directories. Every run creates a complete local, unencrypted
              staging copy of the selected data before uploading it to encrypted repositories. Reserve enough local
              space even when the repository upload is incremental. Failed copies retain staging for the retry window.
            </p>
            {sources.some((source) => source.kind === 'stacklab') && (
              <p role="note" className="rounded-md border border-[var(--warning)]/30 p-3 text-xs leading-5 text-[var(--warning)]">
                Stacklab recovery includes its SQLite state, stacklab.env and the passwords and access credentials
                for every backup destination configured on this host. This credential bundle is sent to every enabled
                destination of this plan. Select only repositories you trust with access to all of them, and keep an
                independent recovery kit outside this host.
              </p>
            )}
            {sources.length > 0 && (
              <details className="rounded-md border border-[var(--panel-border)] p-3">
                <summary className="cursor-pointer text-sm text-[var(--text)]">
                  Selected sources and exclusions ({sources.length})
                </summary>
                <div className="mt-3 space-y-4">
                  {sources.map((source) => (
                    <div key={source.id} className="space-y-2">
                      <div className="flex items-start justify-between gap-2 text-sm text-[var(--text)]">
                        <SourceLabel source={source} />
                        <button
                          type="button"
                          className={buttonClass}
                          onClick={() => toggleSource(source)}
                          aria-label={`Remove ${source.service || source.kind} ${source.mount || source.stack_id || ''}`}
                        >
                          Remove
                        </button>
                      </div>
                      {source.kind === 'managed_config' && (
                        <Field
                          label={`Configuration path for ${source.stack_id || 'managed configuration'}`}
                          hint="Use the configured directory or narrow it to a file or subdirectory within managed configuration."
                        >
                          <input
                            className={inputClass}
                            value={source.path ?? ''}
                            onChange={(event) =>
                              setSources((previous) =>
                                previous.map((item) =>
                                  item.id === source.id ? { ...item, path: event.target.value } : item,
                                ),
                              )
                            }
                          />
                        </Field>
                      )}
                      <Field
                        label={`Exclusions for ${source.service || source.kind}${source.mount ? ` ${source.mount}` : ''}`}
                        hint="One relative file or directory pattern per line. Use identical exclusions for shared data."
                      >
                        <textarea
                          rows={2}
                          className={inputClass}
                          value={(source.excludes ?? []).join('\n')}
                          onChange={(event) =>
                            setSources((previous) =>
                              previous.map((item) =>
                                item.id === source.id ? { ...item, excludes: event.target.value.split('\n') } : item,
                              ),
                            )
                          }
                        />
                      </Field>
                    </div>
                  ))}
                </div>
              </details>
            )}
          </section>
          <Field label="Capture consistency">
            <select
              className={inputClass}
              value={consistency}
              onChange={(event) => setConsistency(event.target.value as BackupPlan['consistency'])}
            >
              <option value="stop">Stop writers, capture, then restore prior service state</option>
              <option value="files">Live file capture / prepared application dumps</option>
            </select>
          </Field>
          <p className="text-xs leading-5 text-[var(--warning)]">
            {consistency === 'stop'
              ? 'Running services that write selected or shared data will be stopped during local capture. They are restarted before upload; services already stopped remain stopped.'
              : 'Live copies of an active database directory are not application-consistent. Use prepared database dumps or stop the writers.'}
          </p>
          <section className="space-y-3" aria-label="Plan destinations">
            <h3 className="text-sm font-semibold text-[var(--text)]">Destinations, schedules and retention</h3>
            {assignments
              .filter((assignment) => !destinations.some((destination) => destination.id === assignment.destination_id))
              .map((assignment) => (
                <div
                  key={assignment.destination_id}
                  className="rounded-md border border-[var(--warning)]/30 p-3 text-sm"
                >
                  <p role="status">
                    Destination details unavailable ({assignment.destination_id}). This assignment is still selected.
                  </p>
                  <button
                    type="button"
                    className={buttonClass}
                    onClick={() => toggleDestination(assignment.destination_id)}
                  >
                    Remove unavailable destination assignment
                  </button>
                </div>
              ))}
            {destinations.length === 0 && (
              <p className="text-sm text-[var(--muted)]">Add a destination before saving a plan.</p>
            )}
            {destinations.map((destination) => {
              const assignment = assignments.find((item) => item.destination_id === destination.id)
              return (
                <div key={destination.id} className="rounded-md border border-[var(--panel-border)] p-3">
                  <label className="flex items-center gap-2 text-sm text-[var(--text)]">
                    <input type="checkbox" checked={!!assignment} onChange={() => toggleDestination(destination.id)} />
                    {destination.name}
                  </label>
                  {assignment && (
                    <div className="mt-3 grid gap-3 sm:grid-cols-3">
                      <div className="sm:col-span-3">
                        <ScheduleFields
                          label={`Schedule for ${destination.name}`}
                          value={assignment.schedule}
                          onChange={(schedule) => updateAssignment(destination.id, { schedule })}
                        />
                      </div>
                      {(['daily', 'weekly', 'monthly'] as const).map((period) => (
                        <Field key={period} label={`Keep ${period} (${destination.name})`}>
                          <input
                            required
                            type="number"
                            min={0}
                            max={3650}
                            className={inputClass}
                            value={Number.isNaN(assignment.retention[period]) ? '' : assignment.retention[period]}
                            onChange={(event) =>
                              updateAssignment(destination.id, {
                                retention: { ...assignment.retention, [period]: event.target.valueAsNumber },
                              })
                            }
                          />
                        </Field>
                      ))}
                      <label className="flex items-center gap-2 text-sm text-[var(--text)] sm:col-span-3">
                        <input
                          type="checkbox"
                          checked={assignment.enabled}
                          onChange={(event) => updateAssignment(destination.id, { enabled: event.target.checked })}
                        />
                        Enable copies to {destination.name}
                      </label>
                    </div>
                  )}
                </div>
              )
            })}
            <p className="text-xs leading-5 text-[var(--muted)]">
              Each destination has an independent copy and retention policy. Scheduled times use the displayed timezone.
              Retention removes older snapshots owned by this plan. Set all retention counts to 0 to keep every snapshot
              (no automatic expiration).
            </p>
          </section>
          <details className="rounded-md border border-[var(--panel-border)] p-3">
            <summary className="cursor-pointer text-sm text-[var(--text)]">Capture limits</summary>
            <div className="mt-3 grid gap-3 sm:grid-cols-3">
              <Field label="Staging budget (GiB)">
                <input
                  required
                  type="number"
                  min={0.001}
                  step="any"
                  className={inputClass}
                  value={Number.isNaN(maxGiB) ? '' : maxGiB}
                  onChange={(event) => setMaxGiB(event.target.valueAsNumber)}
                />
              </Field>
              <Field label="Retry window (hours)">
                <input
                  required
                  type="number"
                  min={1}
                  max={168}
                  className={inputClass}
                  value={Number.isNaN(ttl) ? '' : ttl}
                  onChange={(event) => setTtl(event.target.valueAsNumber)}
                />
              </Field>
              <Field
                label="Capture downtime limit (seconds)"
                hint="Bounds the stopped capture phase, including stopping writers and reading their data."
              >
                <input
                  required
                  type="number"
                  min={10}
                  max={3600}
                  className={inputClass}
                  value={Number.isNaN(stopTimeout) ? '' : stopTimeout}
                  onChange={(event) => setStopTimeout(event.target.valueAsNumber)}
                />
              </Field>
            </div>
          </details>
        </fieldset>
        <div className="flex justify-end gap-2">
          <button type="button" className={buttonClass} disabled={pending} onClick={requestClose}>
            Cancel
          </button>
          <button type="submit" className={primaryClass} disabled={pending}>
            {pending ? 'Saving…' : 'Save plan'}
          </button>
        </div>
      </form>
      {confirmation}
    </Dialog>
  )
}
