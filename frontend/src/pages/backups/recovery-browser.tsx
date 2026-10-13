import { useState } from 'react'
import { Dialog } from '@/components/dialog'
import { OperationReview } from '@/components/operation-review'
import { useApi } from '@/hooks/use-api'
import { getBackupSnapshots, restoreBackupSnapshot, type BackupDestination, type BackupJobResponse, type BackupSnapshot } from '@/lib/backup-api'
import { BackupDate, BackupError, buttonClass, Field, inputClass, primaryClass } from './backup-primitives'

export function RecoveryBrowser({ destination, onClose, onStarted }: {
  destination: BackupDestination
  onClose: () => void
  onStarted: (result: BackupJobResponse) => void
}) {
  const snapshots = useApi(() => getBackupSnapshots(destination.id), [destination.id])
  const [selected, setSelected] = useState<BackupSnapshot | null>(null)
  const [target, setTarget] = useState('')
  const [includes, setIncludes] = useState('')
  const [test, setTest] = useState(true)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function restore() {
    if (!selected || pending) return
    setPending(true)
    setError(null)
    try {
      const result = await restoreBackupSnapshot(destination.id, {
        snapshot_id: selected.id,
        target: target.trim() || undefined,
        includes: includes.split('\n').map((path) => path.trim()).filter(Boolean),
        test,
      })
      onStarted(result)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to start restore')
    } finally {
      setPending(false)
    }
  }

  return <Dialog title={`Recovery sets · ${destination.name}`} onClose={onClose} preventClose={pending} busy={pending} panelClassName="max-w-3xl">
    <div className="mt-4 space-y-4">
      <BackupError error={error} />
      {snapshots.loading && <p role="status" className="text-sm text-[var(--muted)]">Loading recovery sets…</p>}
      {snapshots.error && <div className="space-y-2"><BackupError error={snapshots.error.message} /><button className={buttonClass} onClick={snapshots.refetch}>Retry loading snapshots</button></div>}
      {snapshots.data && <Field label="Recovery set"><select className={inputClass} value={selected?.id ?? ''} disabled={pending} onChange={(event) => { setSelected(snapshots.data?.items.find((snapshot) => snapshot.id === event.target.value) ?? null); setError(null) }}>
        <option value="">Select a snapshot</option>
        {(snapshots.data.items ?? []).map((snapshot) => <option value={snapshot.id} key={snapshot.id}>{new Date(snapshot.time).toLocaleString()} · {snapshot.short_id || snapshot.id.slice(0, 8)} · {(snapshot.tags ?? []).join(', ')}</option>)}
      </select></Field>}
      {snapshots.data && (snapshots.data.items ?? []).length === 0 && <p className="text-sm text-[var(--muted)]">No snapshots found in this repository.</p>}
      {selected && <>
        <dl className="grid gap-2 text-sm text-[var(--muted)] sm:grid-cols-[6rem_1fr]"><dt>Captured</dt><dd><BackupDate value={selected.time} /></dd><dt>Host</dt><dd className="break-all">{selected.hostname || 'Unknown'}</dd><dt>Saved paths</dt><dd className="break-all">{(selected.paths ?? []).join(', ')}</dd></dl>
        <fieldset disabled={pending} className="space-y-3">
          <Field label="Separate restore directory (optional)" hint="Leave empty to create a private restore directory on the Stacklab host. An explicit path must not already exist; live data is never overwritten here."><input className={inputClass} placeholder="Automatic isolated directory" value={target} onChange={(event) => setTarget(event.target.value)} /></Field>
          <Field label="Snapshot paths to include (optional)" hint="One saved path or pattern per line; empty restores the whole recovery set. Service content is stored in source archives; extract individual files after inspecting the restored manifest."><textarea rows={2} className={inputClass} value={includes} onChange={(event) => { setIncludes(event.target.value); if (event.target.value.trim()) setTest(false) }} /></Field>
          <label className="flex items-start gap-2 text-sm text-[var(--text)]"><input type="checkbox" className="mt-1" checked={test} disabled={!!includes.trim()} onChange={(event) => setTest(event.target.checked)} />Record a complete isolated restore test</label>
          <p className="text-xs leading-5 text-[var(--muted)]">A restore test verifies retrieval of the complete recovery set. Application startup and database recovery need a separate recovery exercise.</p>
        </fieldset>
        <OperationReview review={{ target: target.trim() || 'New private directory on the Stacklab host', scope: [includes.trim() ? 'Restore only the selected snapshot paths.' : 'Restore the full recovery set.', `Repository: ${destination.name}`], impact: ['Consumes disk space on this host.', 'Live services and their data are unchanged.'], snapshot: selected.id, recovery: 'Inspect the manifest, source archives and prerequisites before applying recovered data to a stack.' }} />
      </>}
      <div className="flex justify-end gap-2"><button className={buttonClass} disabled={pending} onClick={onClose}>Close</button><button className={primaryClass} disabled={!selected || pending} onClick={() => { void restore() }}>{pending ? 'Starting…' : test ? 'Start isolated restore test' : 'Restore to separate directory'}</button></div>
    </div>
  </Dialog>
}
