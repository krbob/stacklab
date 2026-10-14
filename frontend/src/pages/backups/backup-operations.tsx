import { useEffect, useState, type FormEvent } from 'react'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { useApi } from '@/hooks/use-api'
import { useJobStream } from '@/hooks/use-job-stream'
import {
  getBackupPolicy,
  getBackupRecoveryJournals,
  resolveBackupRecovery,
  saveBackupPolicy,
  type BackupJobResponse,
  type BackupPolicy,
  type BackupRecoveryJournal,
  type BackupRecoveryResolution,
} from '@/lib/backup-api'
import {
  BackupDate,
  BackupError,
  BackupStatus,
  buttonClass,
  Field,
  inputClass,
  primaryClass,
} from './backup-primitives'
import { useEditorCloseGuard } from './editor-close-guard'

export function BackupOperations({
  refreshKey,
  recoveryError,
  onChanged,
  onStarted,
}: {
  refreshKey: number | null
  recoveryError?: string
  onChanged: () => void
  onStarted: (result: BackupJobResponse) => void
}) {
  const policy = useApi((signal) => getBackupPolicy(signal), [refreshKey])
  const [settings, setSettings] = useState(false)
  const [recovery, setRecovery] = useState(false)
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2">
        <button className={buttonClass} disabled={!policy.data} onClick={() => setSettings(true)}>
          Backup settings
        </button>
        <button className={buttonClass} onClick={() => setRecovery(true)}>
          Service recovery
        </button>
      </div>
      <BackupError error={policy.error?.message ?? policy.data?.error ?? null} />
      {policy.error && (
        <button className={buttonClass} onClick={policy.refetch}>
          Retry backup settings
        </button>
      )}
      {policy.data?.host_review_required ? (
        <p role="alert" className="rounded-md border border-[var(--warning)]/30 p-3 text-sm text-[var(--warning)]">
          Backup state needs review on this host after recovery or upgrade. Automatic backups and retention are paused. Review sources,
          destinations and service mappings, then acknowledge this host in Backup settings.
        </p>
      ) : (
        policy.data?.paused && (
          <p role="status" className="rounded-md border border-[var(--warning)]/30 p-3 text-sm text-[var(--warning)]">
            Automatic backups and repository maintenance are paused. Manual operations remain available.
          </p>
        )
      )}
      {recoveryError && (
        <p role="alert" className="rounded-md border border-[var(--warning)]/30 p-3 text-sm text-[var(--warning)]">
          Service recovery needs attention: {recoveryError} Open Service recovery to inspect recorded operations.
        </p>
      )}
      {policy.data?.heartbeat_error && (
        <p role="alert" className="text-sm text-[var(--warning)]">
          {policy.data.heartbeat_error}
        </p>
      )}
      {settings && policy.data && (
        <BackupPolicyDialog
          policy={policy.data}
          onClose={() => setSettings(false)}
          onSaved={() => {
            setSettings(false)
            policy.refetch()
            onChanged()
          }}
        />
      )}
      {recovery && (
        <ServiceRecoveryDialog onClose={() => setRecovery(false)} onChanged={onChanged} onStarted={onStarted} />
      )}
    </div>
  )
}

export function BackupPolicyDialog({
  policy,
  onClose,
  onSaved,
}: {
  policy: BackupPolicy
  onClose: () => void
  onSaved: () => void
}) {
  const [paused, setPaused] = useState(policy.paused)
  const [acknowledgeHost, setAcknowledgeHost] = useState(false)
  const [grace, setGrace] = useState(String(policy.overdue_grace_minutes))
  const [heartbeat, setHeartbeat] = useState('')
  const [clearHeartbeat, setClearHeartbeat] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const dirty =
    paused !== policy.paused ||
    acknowledgeHost ||
    grace !== String(policy.overdue_grace_minutes) ||
    !!heartbeat ||
    clearHeartbeat
  const { requestClose, confirmation } = useEditorCloseGuard(dirty, onClose)

  async function save(event: FormEvent) {
    event.preventDefault()
    if (pending) return
    setPending(true)
    setError(null)
    try {
      await saveBackupPolicy({
        paused,
        overdue_grace_minutes: Number(grace),
        ...(acknowledgeHost ? { acknowledge_host: true } : {}),
        ...(clearHeartbeat ? { clear_heartbeat: true } : heartbeat.trim() ? { heartbeat_url: heartbeat.trim() } : {}),
      })
      setHeartbeat('')
      onSaved()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to save backup settings')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog
      title="Backup settings"
      onClose={requestClose}
      preventClose={pending}
      busy={pending}
      panelClassName="max-w-2xl"
    >
      <form
        className="mt-4 space-y-4"
        onSubmit={(event) => {
          void save(event)
        }}
      >
        <BackupError error={error ?? policy.error ?? null} />
        <fieldset disabled={pending || !!policy.error} className="space-y-4">
          <label className="flex items-start gap-2 text-sm">
            <input type="checkbox" checked={paused} onChange={(event) => setPaused(event.target.checked)} />
            Pause automatic backups and repository maintenance
          </label>
          <p className="text-xs leading-5 text-[var(--muted)]">
            Pausing stops scheduled copies, integrity checks and retention cleanup. Manual recovery and backup actions
            remain available. Success heartbeats stop while paused.
          </p>
          {policy.host_review_required && (
            <label className="flex items-start gap-2 text-sm text-[var(--warning)]">
              <input
                type="checkbox"
                checked={acknowledgeHost}
                onChange={(event) => setAcknowledgeHost(event.target.checked)}
              />
              I reviewed the recovered sources, service mappings and repositories on this host, including the
              possibility of another host writing to the same repositories.
            </label>
          )}
          <Field
            label="Overdue alert grace (minutes)"
            hint="Allow this long after the scheduled time before reporting a missing copy."
          >
            <input
              className={inputClass}
              type="number"
              required
              min={1}
              max={10080}
              value={grace}
              onChange={(event) => setGrace(event.target.value)}
            />
          </Field>
          <Field
            label="External success heartbeat URL"
            hint={
              policy.heartbeat_configured
                ? 'An address is configured. Leave empty to keep it; the saved URL is never displayed.'
                : 'Create a heartbeat monitor outside this host and enter its HTTP(S) success URL.'
            }
          >
            <input
              className={inputClass}
              type="password"
              autoComplete="new-password"
              disabled={clearHeartbeat}
              value={heartbeat}
              onChange={(event) => setHeartbeat(event.target.value)}
            />
          </Field>
          {policy.heartbeat_configured && (
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={clearHeartbeat}
                onChange={(event) => setClearHeartbeat(event.target.checked)}
              />
              Remove the saved heartbeat URL
            </label>
          )}
          <p className="text-xs leading-5 text-[var(--muted)]">
            Stacklab sends a success heartbeat every minute while enabled scheduled copies meet their protection
            requirements. Configure the independent monitor to alert when heartbeats stop, including when this host is
            offline. Manual-only plans do not produce success heartbeats.
          </p>
          <p className="text-xs text-[var(--muted)]">
            Last successful heartbeat: <BackupDate value={policy.last_heartbeat_at} />
          </p>
        </fieldset>
        <div className="flex justify-end gap-2">
          <button type="button" className={buttonClass} disabled={pending} onClick={requestClose}>
            Cancel
          </button>
          <button
            type="submit"
            className={primaryClass}
            disabled={pending || !!policy.error || (!paused && policy.host_review_required && !acknowledgeHost)}
          >
            {pending ? 'Saving…' : 'Save backup settings'}
          </button>
        </div>
      </form>
      {confirmation}
    </Dialog>
  )
}

export function ServiceRecoveryDialog({
  onClose,
  onChanged,
  onStarted,
}: {
  onClose: () => void
  onChanged: () => void
  onStarted: (result: BackupJobResponse) => void
}) {
  const state = useApi((signal) => getBackupRecoveryJournals(signal), [])
  const [review, setReview] = useState<{
    journal: BackupRecoveryJournal
    action: BackupRecoveryResolution['action']
  } | null>(null)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [jobId, setJobId] = useState<string | null>(null)
  const { state: jobState } = useJobStream({ jobId })
  const { refetch } = state
  const running = !!jobId && !['succeeded', 'failed', 'cancelled', 'timed_out'].includes(jobState ?? '')
  useEffect(() => {
    if (jobId && ['succeeded', 'failed', 'cancelled', 'timed_out'].includes(jobState ?? '')) {
      refetch()
      onChanged()
    }
  }, [jobId, jobState, refetch, onChanged])

  async function resolve() {
    if (!review || pending || running) return
    setPending(true)
    setError(null)
    try {
      const result = await resolveBackupRecovery(review.journal.run_id, { action: review.action })
      setJobId(result.job.id)
      setReview(null)
      onStarted(result)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to resolve service recovery')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog title="Service recovery" onClose={onClose} preventClose={pending} busy={pending} panelClassName="max-w-3xl">
      <div className="mt-4 space-y-4">
        <p className="text-sm leading-6 text-[var(--muted)]">
          Interrupted backup operations retain the identities and phases of affected services. Review these records
          before retrying recovery. A held restore requires inspecting live data and any rollback copies first.
        </p>
        <BackupError error={state.error?.message ?? null} />
        <button className={buttonClass} disabled={state.loading || pending} onClick={state.refetch}>
          Refresh recovery records
        </button>
        {state.loading && !state.data && <p role="status">Loading service recovery records…</p>}
        {jobId && (
          <p role="status" className="text-sm">
            Recovery operation: <BackupStatus value={jobState ?? 'queued'} />
          </p>
        )}
        {state.data?.items.length === 0 && (
          <p className="text-sm text-[var(--muted)]">No unresolved service recovery records.</p>
        )}
        {state.data?.items.map((journal) => (
          <section key={journal.run_id} className="space-y-3 rounded-md border border-[var(--panel-border)] p-3">
            <h3 className="break-all font-mono text-sm">{journal.run_id}</h3>
            <BackupError error={journal.error ?? null} />
            <p className="text-sm">
              {journal.hold_stopped
                ? 'Services held stopped after recovery.'
                : 'Previous service state still needs recovery.'}
            </p>
            <ul className="space-y-1 text-xs text-[var(--muted)]">
              {(journal.writers ?? []).map((writer) => (
                <li key={writer.container_id} className="break-all">
                  {writer.stack_id || 'external'} / {writer.service || writer.container_id} · {writer.container_id} ·{' '}
                  {journal.phases?.[writer.container_id] ?? 'phase not recorded'}
                </li>
              ))}
            </ul>
            {Object.keys(journal.prepared_tokens ?? {}).length > 0 && (
              <p className="text-xs text-[var(--warning)]">
                Prepared restore and rollback data exists. Follow the recovery runbook to inspect it; acknowledging the
                record does not remove those files.
              </p>
            )}
            {!journal.error && (
              <button
                className={buttonClass}
                disabled={pending || running}
                onClick={() => {
                  setError(null)
                  setReview({ journal, action: journal.hold_stopped ? 'acknowledge_hold' : 'retry' })
                }}
              >
                {journal.hold_stopped ? 'Review held services' : 'Retry recorded service recovery'}
              </button>
            )}
          </section>
        ))}
        <div className="flex justify-end">
          <button className={buttonClass} disabled={pending} onClick={onClose}>
            Close
          </button>
        </div>
      </div>
      {review && (
        <ConfirmDialog
          title={
            review.action === 'acknowledge_hold' ? 'Acknowledge inspected recovery' : 'Retry recorded service recovery'
          }
          message={
            review.action === 'acknowledge_hold'
              ? 'Confirm that you inspected the affected data, services and rollback copies. This removes the recovery hold record and allows new backups. Services remain stopped and rollback data is preserved; start services separately only when ready.'
              : 'Retry recovery using the recorded container identities. Stacklab may stop a pending shutdown and then restart services that were previously running. Missing or changed identities require manual inspection.'
          }
          confirmLabel={review.action === 'acknowledge_hold' ? 'Acknowledge inspection' : 'Retry recovery'}
          requireText={review.action === 'acknowledge_hold' ? 'INSPECTED' : undefined}
          items={(review.journal.writers ?? []).map((writer) => writer.container_id)}
          error={error}
          confirming={pending}
          onCancel={() => {
            setReview(null)
            setError(null)
          }}
          onConfirm={() => {
            void resolve()
          }}
        />
      )}
    </Dialog>
  )
}
