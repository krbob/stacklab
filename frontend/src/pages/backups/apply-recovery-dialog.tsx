import { useState } from 'react'
import { Dialog } from '@/components/dialog'
import { OperationReview } from '@/components/operation-review'
import { getBackupRestoreManifest, previewBackupRestore, restoreBackupSnapshot, type BackupDestination, type BackupJobResponse, type BackupManifest, type BackupRestoreMapping, type BackupRestoreRequest, type BackupRestoreReview } from '@/lib/backup-api'
import { BackupDate, BackupError, buttonClass, Field, inputClass, primaryClass } from './backup-primitives'
import { SourceLabel } from './source-label'

export function ApplyRecoveryDialog({ destination, onStarted, onClose }: { destination: BackupDestination; onStarted: (result: BackupJobResponse) => void; onClose: () => void }) {
  const [directory, setDirectory] = useState('')
  const [manifest, setManifest] = useState<BackupManifest | null>(null)
  const [mappings, setMappings] = useState<BackupRestoreMapping[]>([])
  const [resume, setResume] = useState(false)
  const [review, setReview] = useState<BackupRestoreReview | null>(null)
  const [confirmation, setConfirmation] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function payload(): BackupRestoreRequest {
    return { restored_directory: directory.trim(), mappings, resume_services: resume }
  }

  async function loadManifest() {
    setPending(true)
    setError(null)
    setManifest(null)
    setMappings([])
    setReview(null)
    try {
      const result = await getBackupRestoreManifest(directory.trim())
      setManifest(result.manifest)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to load recovery manifest')
    } finally {
      setPending(false)
    }
  }

  async function preview() {
    setPending(true)
    setError(null)
    setConfirmation('')
    try {
      setReview(await previewBackupRestore(destination.id, payload()))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to preview recovery')
    } finally {
      setPending(false)
    }
  }

  async function apply() {
    if (!review || confirmation !== 'RESTORE' || pending) return
    setPending(true)
    setError(null)
    try {
      onStarted(await restoreBackupSnapshot(destination.id, { ...payload(), review_token: review.review_token }))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to start recovery')
      setReview(null)
    } finally {
      setPending(false)
    }
  }

  function updateMapping(sourceId: string, patch: Partial<BackupRestoreMapping>) {
    setMappings((previous) => previous.map((mapping) => mapping.source_id === sourceId ? { ...mapping, ...patch } : mapping))
  }

  return <Dialog title={`Apply recovered data · ${destination.name}`} onClose={onClose} preventClose={pending} busy={pending} panelClassName="max-w-3xl">
    <div className="mt-4 space-y-4">
      <BackupError error={error} />
      <p className="text-sm leading-6 text-[var(--muted)]">First restore a recovery set to a separate directory using Browse & restore. Then load its manifest, select the sources to apply and review destination paths and affected services. The isolated restore job records its output directory.</p>
      {!review && <>
        <Field label="Completed isolated restore directory"><input className={inputClass} value={directory} disabled={pending} onChange={(event) => { setDirectory(event.target.value); setManifest(null); setMappings([]) }} placeholder="/var/lib/stacklab/backups/restores/restore_…" /></Field>
        <button className={buttonClass} disabled={pending || !directory.trim()} onClick={() => { void loadManifest() }}>{pending ? 'Working…' : 'Load recovery manifest'}</button>
        {manifest && <>
          <p className="text-xs text-[var(--muted)]">Captured <BackupDate value={manifest.captured_at} /> · {manifest.consistency} · Manifest v{manifest.version}</p>
          <fieldset disabled={pending} className="space-y-3">
            {manifest.sources.map((item) => {
              const mapping = mappings.find((value) => value.source_id === item.source.id)
              const offline = item.source.kind === 'stacklab'
              return <div key={item.source.id} className="space-y-3 rounded-md border border-[var(--panel-border)] p-3">
                <label className="flex items-start gap-2 text-sm text-[var(--text)]"><input type="checkbox" className="mt-1" disabled={offline} checked={!!mapping} onChange={(event) => setMappings((previous) => event.target.checked ? [...previous, { source_id: item.source.id, target: '' }] : previous.filter((value) => value.source_id !== item.source.id))} /><SourceLabel source={item.source} /></label>
                {offline && <p className="text-xs text-[var(--warning)]">Stacklab private state and SQLite must be recovered offline using the recovery runbook.</p>}
                {mapping && <div className="grid gap-3 sm:grid-cols-2">
                  <Field label={`Target directory for ${item.source.service || item.source.kind}`} hint={`Original path: ${item.original_path}${item.source.kind === 'managed_config' ? '. For a backed-up file, choose its parent directory; extraction retains the filename.' : ''}`}><input className={inputClass} value={mapping.target} disabled={!!mapping.volume_name} onChange={(event) => updateMapping(item.source.id, { target: event.target.value })} placeholder="Explicit absolute target directory" /></Field>
                  {item.volume_name && <Field label={`Existing volume for ${item.source.service || item.source.kind}`} hint="Create the destination volume first. A volume name replaces the directory mapping."><input className={inputClass} value={mapping.volume_name ?? ''} onChange={(event) => updateMapping(item.source.id, { volume_name: event.target.value || undefined, target: '' })} placeholder={item.volume_name} /></Field>}
                  <button type="button" className={buttonClass} onClick={() => updateMapping(item.source.id, item.volume_name ? { volume_name: item.volume_name, target: '' } : { target: item.file_name ? item.original_path.slice(0, item.original_path.lastIndexOf('/')) || '/' : item.original_path })}>Use original location</button>
                </div>}
              </div>
            })}
            <label className="flex items-start gap-2 text-sm text-[var(--text)]"><input type="checkbox" className="mt-1" checked={resume} onChange={(event) => setResume(event.target.checked)} />Restart services that were running before recovery</label>
          </fieldset>
          <p className="text-xs leading-5 text-[var(--warning)]">Recovery merges selected content, overwriting matching files and preserving unrelated files. Affected running writers remain stopped by default. A failure after writing begins also leaves them stopped so you can inspect the result.</p>
          {(manifest.images?.length ?? 0) > 0 && <p className="break-all text-xs text-[var(--muted)]">Required images: {manifest.images?.join(', ')}. Registry access, external networks, secrets and build inputs must be available on this host.</p>}
          {manifest.warnings?.map((warning) => <p key={warning} className="text-xs text-[var(--warning)]">{warning}</p>)}
        </>}
      </>}
      {review && <>
        <OperationReview review={{
          target: review.preview.mappings.map((mapping) => mapping.volume_name ? `${mapping.volume_name} (${mapping.target})` : mapping.target).join(', '),
          scope: review.preview.mappings.map((mapping) => {
            const source = manifest?.sources.find((item) => item.source.id === mapping.source_id)?.source
            const label = source ? [source.stack_id, source.service, source.mount || source.kind].filter(Boolean).join(' / ') : mapping.source_id
            return `${label} → ${mapping.target}`
          }),
          impact: [review.preview.overwrites ? 'Existing files at these destinations may be overwritten. Unrelated files remain.' : 'Recovered data will be written to the selected destinations.', ...(review.preview.writers ?? []).filter((writer) => writer.running).map((writer) => `Stop writer: ${writer.stack_id || 'external'}/${writer.service || writer.container_id}`), resume ? 'Restart previously running writers after recovery succeeds.' : 'Leave affected writers stopped after recovery.'],
          snapshot: `Recovery set captured ${manifest?.captured_at ? new Date(manifest.captured_at).toLocaleString() : 'at unknown time'}; review expires ${new Date(review.expires_at).toLocaleString()}`,
          recovery: 'Keep a separate current backup before replacing live data. Review Compose and application prerequisites before starting services.',
        }} />
        {(review.preview.warnings ?? []).map((warning) => <p key={warning} className="text-xs text-[var(--warning)]">{warning}</p>)}
        <Field label="Type RESTORE to apply recovered data"><input className={inputClass} value={confirmation} disabled={pending} autoComplete="off" onChange={(event) => setConfirmation(event.target.value)} /></Field>
      </>}
      <div className="flex flex-wrap justify-end gap-2"><button className={buttonClass} disabled={pending} onClick={onClose}>Cancel</button>{review ? <><button className={buttonClass} disabled={pending} onClick={() => { setReview(null); setConfirmation('') }}>Change mappings</button><button className="rounded-md border border-[var(--danger)]/40 bg-[var(--danger)]/10 px-3 py-2 text-sm text-[var(--danger)] disabled:opacity-50" disabled={pending || confirmation !== 'RESTORE'} onClick={() => { void apply() }}>{pending ? 'Starting…' : 'Apply recovered data'}</button></> : <button className={primaryClass} disabled={pending || !manifest || mappings.length === 0 || mappings.some((mapping) => !mapping.target.trim() && !mapping.volume_name?.trim())} onClick={() => { void preview() }}>Preview recovery</button>}</div>
    </div>
  </Dialog>
}
