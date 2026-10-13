import { useMemo, useState, type FormEvent } from 'react'
import { Dialog } from '@/components/dialog'
import { getStacks } from '@/lib/api-client'
import { getBackupSources, saveBackupPlan, type BackupAssignment, type BackupDestination, type BackupPlan, type BackupSource } from '@/lib/backup-api'
import { useApi } from '@/hooks/use-api'
import { BackupError, buttonClass, Field, inputClass, primaryClass } from './backup-primitives'
import { SourceLabel } from './source-label'

const gib = 1024 ** 3

export function PlanEditor({ plan, destinations, initialStackId, onSaved, onClose }: {
  plan?: BackupPlan
  destinations: BackupDestination[]
  initialStackId?: string
  onSaved: () => void
  onClose: () => void
}) {
  const [name, setName] = useState(plan?.name ?? '')
  const [enabled, setEnabled] = useState(plan?.enabled ?? true)
  const [consistency, setConsistency] = useState<BackupPlan['consistency']>(plan?.consistency ?? 'stop')
  const [stackId, setStackId] = useState(initialStackId ?? '')
  const [sources, setSources] = useState<BackupSource[]>(plan?.sources ?? [])
  const [assignments, setAssignments] = useState<BackupAssignment[]>(plan?.destinations ?? [])
  const [maxGiB, setMaxGiB] = useState((plan?.staging_max_bytes ?? 10 * gib) / gib)
  const [ttl, setTtl] = useState(plan?.staging_ttl_hours ?? 24)
  const [stopTimeout, setStopTimeout] = useState(plan?.stop_timeout_seconds ?? 300)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const stacks = useApi(() => getStacks(), [])
  const inventory = useApi(() => getBackupSources(stackId || undefined), [stackId])
  const available = useMemo(() => inventory.data?.items ?? [], [inventory.data])

  function toggleSource(source: BackupSource) {
    setSources((previous) => previous.some((item) => item.id === source.id)
      ? previous.filter((item) => item.id !== source.id)
      : [...previous, { ...source, excludes: [] }])
  }

  function toggleDestination(id: string) {
    setAssignments((previous) => previous.some((item) => item.destination_id === id)
      ? previous.filter((item) => item.destination_id !== id)
      : [...previous, { destination_id: id, enabled: true, schedule: { frequency: 'manual', time: '02:00', weekday: 0, timezone: '' }, retention: { daily: 7, weekly: 4, monthly: 12 } }])
  }

  function updateAssignment(id: string, patch: Partial<BackupAssignment>) {
    setAssignments((previous) => previous.map((item) => item.destination_id === id ? { ...item, ...patch } : item))
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
      await saveBackupPlan({ name: name.trim(), enabled, consistency, sources: sources.map((source) => ({ ...source, excludes: source.excludes?.map((pattern) => pattern.trim()).filter(Boolean) })), destinations: assignments, staging_max_bytes: Math.round(maxGiB * gib), staging_ttl_hours: ttl, stop_timeout_seconds: stopTimeout }, plan?.id)
      onSaved()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to save plan')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog title={plan ? 'Edit backup plan' : 'Create backup plan'} onClose={onClose} preventClose={pending} busy={pending} panelClassName="max-w-3xl">
      <form onSubmit={(event) => { void save(event) }} className="mt-4 space-y-5">
        <BackupError error={error} />
        <fieldset disabled={pending} className="space-y-5">
          <div className="grid items-end gap-4 sm:grid-cols-2">
            <Field label="Plan name"><input required className={inputClass} value={name} onChange={(event) => setName(event.target.value)} /></Field>
            <label className="flex items-center gap-2 py-2 text-sm text-[var(--text)]"><input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} /> Enable plan schedules</label>
          </div>
          <section className="space-y-3" aria-label="Backup sources">
            <h3 className="text-sm font-semibold text-[var(--text)]">Select data</h3>
            <Field label="Source scope"><select className={inputClass} value={stackId} onChange={(event) => setStackId(event.target.value)}>
              <option value="">Stacklab recovery / managed configuration</option>
              {(stacks.data?.items ?? []).map((stack) => <option key={stack.id} value={stack.id}>{stack.name}</option>)}
            </select></Field>
            {stacks.error && <p role="alert" className="text-sm text-[var(--danger)]">Unable to list stacks: {stacks.error.message} <button type="button" className={buttonClass} onClick={stacks.refetch}>Retry stacks</button></p>}
            {inventory.loading ? <p role="status" className="text-sm text-[var(--muted)]">Discovering sources…</p> : inventory.error ? <p role="alert" className="text-sm text-[var(--danger)]">{inventory.error.message} <button type="button" className={buttonClass} onClick={inventory.refetch}>Retry discovery</button></p> : <div className="space-y-2">
              {available.map((source) => <label key={source.id} className="flex items-start gap-3 rounded-md border border-[var(--panel-border)] p-3 text-sm text-[var(--text)]">
                <input type="checkbox" className="mt-1" checked={sources.some((item) => item.id === source.id)} onChange={() => toggleSource(source)} /><SourceLabel source={source} />
              </label>)}
              {available.length === 0 && <p className="text-sm text-[var(--muted)]">No selectable sources in this scope.</p>}
            </div>}
            <p className="text-xs leading-5 text-[var(--muted)]">New mounts are never selected automatically. Definitions include .env and stack-local supporting files. Stacklab recovery includes private credentials and a consistent SQLite copy. All selected content is encrypted in each repository.</p>
            {sources.length > 0 && <details className="rounded-md border border-[var(--panel-border)] p-3">
              <summary className="cursor-pointer text-sm text-[var(--text)]">Selected sources and exclusions ({sources.length})</summary>
              <div className="mt-3 space-y-4">{sources.map((source) => <div key={source.id} className="space-y-2">
                <div className="flex items-start justify-between gap-2 text-sm text-[var(--text)]"><SourceLabel source={source} /><button type="button" className={buttonClass} onClick={() => toggleSource(source)} aria-label={`Remove ${source.service || source.kind} ${source.mount || source.stack_id || ''}`}>Remove</button></div>
                {source.kind === 'managed_config' && <Field label={`Configuration path for ${source.stack_id || 'managed configuration'}`} hint="Use the configured directory or narrow it to a file or subdirectory within managed configuration."><input className={inputClass} value={source.path ?? ''} onChange={(event) => setSources((previous) => previous.map((item) => item.id === source.id ? { ...item, path: event.target.value } : item))} /></Field>}
                <Field label={`Exclusions for ${source.service || source.kind}${source.mount ? ` ${source.mount}` : ''}`} hint="One relative file or directory pattern per line. Use identical exclusions for shared data."><textarea rows={2} className={inputClass} value={(source.excludes ?? []).join('\n')} onChange={(event) => setSources((previous) => previous.map((item) => item.id === source.id ? { ...item, excludes: event.target.value.split('\n') } : item))} /></Field>
              </div>)}</div>
            </details>}
          </section>
          <Field label="Capture consistency"><select className={inputClass} value={consistency} onChange={(event) => setConsistency(event.target.value as BackupPlan['consistency'])}>
            <option value="stop">Stop writers, capture, then restore prior service state</option><option value="files">Live file capture / prepared application dumps</option>
          </select></Field>
          <p className="text-xs leading-5 text-[var(--warning)]">{consistency === 'stop' ? 'Running services that write selected or shared data will be stopped during local capture. They are restarted before upload; services already stopped remain stopped.' : 'Live copies of an active database directory are not application-consistent. Use prepared database dumps or stop the writers.'}</p>
          <section className="space-y-3" aria-label="Plan destinations">
            <h3 className="text-sm font-semibold text-[var(--text)]">Destinations, schedules and retention</h3>
            {destinations.length === 0 && <p className="text-sm text-[var(--muted)]">Add a destination before saving a plan.</p>}
            {destinations.map((destination) => {
              const assignment = assignments.find((item) => item.destination_id === destination.id)
              return <div key={destination.id} className="rounded-md border border-[var(--panel-border)] p-3">
                <label className="flex items-center gap-2 text-sm text-[var(--text)]"><input type="checkbox" checked={!!assignment} onChange={() => toggleDestination(destination.id)} />{destination.name}</label>
                {assignment && <div className="mt-3 grid gap-3 sm:grid-cols-3">
                  <Field label={`Schedule for ${destination.name}`}><select className={inputClass} value={assignment.schedule.frequency} onChange={(event) => updateAssignment(destination.id, { schedule: { ...assignment.schedule, frequency: event.target.value as BackupAssignment['schedule']['frequency'] } })}>
                    <option value="manual">Manual only</option><option value="daily">Daily</option><option value="weekly">Weekly</option>
                  </select></Field>
                  {assignment.schedule.frequency !== 'manual' && <>
                    <Field label={`Time for ${destination.name}`}><input required type="time" className={inputClass} value={assignment.schedule.time ?? '02:00'} onChange={(event) => updateAssignment(destination.id, { schedule: { ...assignment.schedule, time: event.target.value } })} /></Field>
                    <Field label={`Timezone for ${destination.name}`} hint="Empty uses the host's local timezone; enter an IANA zone to override it."><input className={inputClass} placeholder="Host local timezone" value={assignment.schedule.timezone ?? ''} onChange={(event) => updateAssignment(destination.id, { schedule: { ...assignment.schedule, timezone: event.target.value } })} /></Field>
                  </>}
                  {assignment.schedule.frequency === 'weekly' && <Field label={`Weekday for ${destination.name}`}><select className={inputClass} value={assignment.schedule.weekday ?? 0} onChange={(event) => updateAssignment(destination.id, { schedule: { ...assignment.schedule, weekday: Number(event.target.value) } })}>{['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'].map((day, index) => <option value={index} key={day}>{day}</option>)}</select></Field>}
                  {(['daily', 'weekly', 'monthly'] as const).map((period) => <Field key={period} label={`Keep ${period} (${destination.name})`}><input required type="number" min={0} max={3650} className={inputClass} value={assignment.retention[period]} onChange={(event) => updateAssignment(destination.id, { retention: { ...assignment.retention, [period]: Number(event.target.value) } })} /></Field>)}
                  <label className="flex items-center gap-2 text-sm text-[var(--text)] sm:col-span-3"><input type="checkbox" checked={assignment.enabled} onChange={(event) => updateAssignment(destination.id, { enabled: event.target.checked })} />Enable copies to {destination.name}</label>
                </div>}
              </div>
            })}
            <p className="text-xs leading-5 text-[var(--muted)]">Each destination has an independent copy and retention policy. Scheduled times use the displayed timezone. Retention removes older snapshots owned by this plan.</p>
          </section>
          <details className="rounded-md border border-[var(--panel-border)] p-3">
            <summary className="cursor-pointer text-sm text-[var(--text)]">Capture limits</summary>
            <div className="mt-3 grid gap-3 sm:grid-cols-3">
              <Field label="Staging budget (GiB)"><input required type="number" min={0.001} step="any" className={inputClass} value={maxGiB} onChange={(event) => setMaxGiB(Number(event.target.value))} /></Field>
              <Field label="Retry window (hours)"><input required type="number" min={1} max={168} className={inputClass} value={ttl} onChange={(event) => setTtl(Number(event.target.value))} /></Field>
              <Field label="Stop timeout (seconds)"><input required type="number" min={10} max={3600} className={inputClass} value={stopTimeout} onChange={(event) => setStopTimeout(Number(event.target.value))} /></Field>
            </div>
          </details>
        </fieldset>
        <div className="flex justify-end gap-2"><button type="button" className={buttonClass} disabled={pending} onClick={onClose}>Cancel</button><button type="submit" className={primaryClass} disabled={pending}>{pending ? 'Saving…' : 'Save plan'}</button></div>
      </form>
    </Dialog>
  )
}
