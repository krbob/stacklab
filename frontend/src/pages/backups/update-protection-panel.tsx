import { useState } from 'react'
import { Link } from 'react-router-dom'
import { AsyncState } from '@/components/async-state'
import { ApiClientError } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { getUpdateProtection, saveUpdateProtection, type BackupDestination, type BackupPlan, type UpdateProtectionPolicy } from '@/lib/backup-api'
import { BackupPanel, Field, inputClass, primaryClass } from './backup-primitives'
import { SourceLabel } from './source-label'

interface PanelProps {
  stackId: string
  plans: BackupPlan[]
  destinations: BackupDestination[]
  refreshPlans: () => void
}

export function UpdateProtectionPanel(props: PanelProps) {
  const state = useApi(signal => getUpdateProtection(props.stackId, signal), [props.stackId])
  return (
    <BackupPanel title="Maintenance update protection">
      <AsyncState loading={state.loading} error={state.error} hasData={!!state.data}
        isEmpty={false} emptyMessage="" loadingFallback={<p className="text-sm text-[var(--muted)]">Loading settings…</p>}
        loadingLabel="Loading update protection…" onRetry={state.refetch}>
        {state.data && <ProtectionForm key={state.data.revision} {...props} initial={state.data} reloadPolicy={state.refetch} />}
      </AsyncState>
    </BackupPanel>
  )
}

function ProtectionForm({ stackId, plans, destinations, refreshPlans, initial, reloadPolicy }: PanelProps & { initial: UpdateProtectionPolicy; reloadPolicy: () => void }) {
  const [saved, setSaved] = useState(initial)
  const [autoRollback, setAutoRollback] = useState(initial.auto_rollback)
  const [planId, setPlanId] = useState(initial.backup_plan_id ?? '')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [conflict, setConflict] = useState(false)
  const [success, setSuccess] = useState(false)
  const eligible = plans.filter(plan => plan.update_backup_fingerprint && plan.sources.every(source => source.stack_id === stackId))
  const selected = eligible.find(plan => plan.id === planId)
  const changedPlan = !!selected && saved.backup_plan_id === planId && selected.update_backup_fingerprint !== saved.backup_plan_fingerprint
  const changed = autoRollback !== saved.auto_rollback || planId !== (saved.backup_plan_id ?? '') || changedPlan

  async function save() {
    if (!changed || pending || conflict || (planId && !selected)) return
    setPending(true)
    setError('')
    setSuccess(false)
    try {
      const policy = await saveUpdateProtection(stackId, {
        auto_rollback: autoRollback,
        revision: saved.revision,
        ...(selected && {
          backup_plan_id: selected.id,
          backup_source_ids: selected.sources.map(source => source.id),
          backup_plan_fingerprint: selected.update_backup_fingerprint,
        }),
      })
      setSaved(policy)
      setSuccess(true)
    } catch (cause) {
      setConflict(cause instanceof ApiClientError && cause.status === 409)
      setError(cause instanceof Error ? cause.message : 'Could not save update protection.')
      refreshPlans()
    } finally {
      setPending(false)
    }
  }

  return (
    <form className="space-y-4 text-sm text-[var(--text)]" onSubmit={event => { event.preventDefault(); void save() }}>
      <fieldset disabled={pending} className="space-y-4">
        <div>
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={autoRollback} onChange={event => { setAutoRollback(event.target.checked); setSuccess(false) }} />
            Automatically roll back images after a failed update
          </label>
          <p className="mt-1 text-xs text-[var(--muted)]">
            Stacklab can restore images for services that passed a Docker or HTTP readiness check before the update.
            Enable this when the previous version can use the updated data. Restoring data requires a separate review.
          </p>
        </div>
        <Field label="Required backup before deployment" hint="Choose an enabled backup plan dedicated to this stack. Deployment waits for a verified copy.">
          <select className={inputClass} value={planId} onChange={event => { setPlanId(event.target.value); setSuccess(false) }}>
            <option value="">No required backup</option>
            {planId && !selected && <option value={planId} disabled>Configured plan unavailable</option>}
            {eligible.map(plan => <option key={plan.id} value={plan.id}>{plan.name}</option>)}
          </select>
        </Field>
        {selected && <div className="space-y-2 rounded border border-[var(--panel-border)] p-3">
          <p>{selected.consistency === 'stop' ? 'Stop writers during capture' : 'Live file capture'}</p>
          <ul className="space-y-2">
            {selected.sources.map(source => <li key={source.id}>
              <SourceLabel source={source} />
              {!!source.excludes?.length && <span className="text-xs text-[var(--muted)]">Excluded paths: {source.excludes.join(', ')}</span>}
            </li>)}
          </ul>
          <p>Backup destinations</p>
          <ul className="space-y-1 text-xs text-[var(--muted)]">
            {selected.destinations.filter(assignment => assignment.enabled).map(({ destination_id: id }) => {
              const destination = destinations.find(item => item.id === id)
              return <li key={id} className="break-all">
                {destination ? [destination.name, destination.repository, destination.region,
                  destination.expected_filesystem && `Filesystem ${destination.expected_filesystem}`].filter(Boolean).join(' · ') : id}
              </li>
            })}
          </ul>
        </div>}
        {changedPlan && <p className="text-[var(--warning)]" role="status">The plan has changed. Review its sources and destinations before saving.</p>}
        <Link className="text-[var(--accent)] underline" to={`/backups?view=plans&stack=${encodeURIComponent(stackId)}`}>Configure backup sources and destinations</Link>
      </fieldset>
      {error && <p role="alert" className="text-[var(--danger)]">{error}</p>}
      {conflict && <button type="button" className="text-[var(--accent)] underline" onClick={reloadPolicy}>
        Reload current settings
      </button>}
      <div className="flex items-center gap-3">
        <button className={primaryClass} type="submit" disabled={!changed || pending || conflict || (!!planId && !selected)}>
          {pending ? 'Saving…' : 'Save update protection'}
        </button>
        {success && !changed && <span role="status">Saved</span>}
      </div>
    </form>
  )
}
