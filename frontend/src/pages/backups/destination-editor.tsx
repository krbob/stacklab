import { useState, type FormEvent } from 'react'
import { Dialog } from '@/components/dialog'
import { getBackupFilesystem, saveBackupDestination, type BackupDestination, type BackupSecrets } from '@/lib/backup-api'
import { BackupError, buttonClass, Field, inputClass, primaryClass } from './backup-primitives'

const repositoryHints: Record<string, string> = {
  local: 'Absolute repository directory, for example /mnt/backup/stacklab. The expected filesystem prevents writing to the host disk when a mounted disk is absent.',
  s3: 'S3 repository, for example s3:https://s3.us-west-004.backblazeb2.com/my-bucket/stacklab. Use a dedicated bucket or prefix.',
  sftp: 'SFTP repository, for example sftp://backup@nas/backups/stacklab. Supply the private key and a trusted known_hosts entry below.',
  rclone: 'Rclone repository, for example rclone:gdrive:stacklab. Paste an authorized rclone configuration containing that remote below.',
}

export function DestinationEditor({ destination, onSaved, onClose }: {
  destination?: BackupDestination
  onSaved: () => void
  onClose: () => void
}) {
  const [name, setName] = useState(destination?.name ?? '')
  const [kind, setKind] = useState<BackupDestination['kind']>(destination?.kind ?? 'local')
  const [repository, setRepository] = useState(destination?.repository ?? '')
  const [filesystem, setFilesystem] = useState(destination?.expected_filesystem ?? '')
  const [region, setRegion] = useState(destination?.region ?? '')
  const [secrets, setSecrets] = useState<BackupSecrets>({})
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [detected, setDetected] = useState(false)
  const changeSecret = (key: keyof BackupSecrets, value: string) => setSecrets((previous) => ({ ...previous, [key]: value }))

  async function detectFilesystem() {
    setPending(true)
    setError(null)
    try {
      const result = await getBackupFilesystem(repository.trim())
      setFilesystem(result.device)
      setDetected(true)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to detect filesystem')
    } finally {
      setPending(false)
    }
  }

  async function save(event: FormEvent) {
    event.preventDefault()
    if (pending) return
    setPending(true)
    setError(null)
    const supplied = Object.fromEntries(Object.entries(secrets).filter(([, value]) => value !== ''))
    try {
      await saveBackupDestination({
        name: name.trim(), kind, repository: repository.trim(),
        expected_filesystem: kind === 'local' ? filesystem.trim() : undefined,
        region: kind === 's3' ? region.trim() : undefined,
        secrets: Object.keys(supplied).length > 0 ? supplied : undefined,
      }, destination?.id)
      setSecrets({})
      onSaved()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to save destination')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog title={destination ? 'Edit destination' : 'Add destination'} onClose={onClose} preventClose={pending} busy={pending} panelClassName="max-w-2xl">
      <form onSubmit={(event) => { void save(event) }} className="mt-4 space-y-4">
        <BackupError error={error} />
        <fieldset disabled={pending} className="grid min-w-0 gap-4 sm:grid-cols-2">
          <Field label="Destination name"><input className={inputClass} required value={name} onChange={(event) => setName(event.target.value)} /></Field>
          <Field label="Storage type"><select className={inputClass} value={kind} onChange={(event) => { setKind(event.target.value as BackupDestination['kind']); setRepository(''); setSecrets({}) }}>
            <option value="local">Local disk / mounted NAS</option><option value="s3">S3 / Backblaze B2</option><option value="sftp">NAS over SFTP</option><option value="rclone">Google Drive / rclone</option>
          </select></Field>
          <div className="sm:col-span-2"><Field label="Repository" hint={repositoryHints[kind]}><input className={inputClass} required value={repository} onChange={(event) => setRepository(event.target.value)} autoComplete="off" /></Field></div>
          {kind === 'local' && <div className="space-y-2 sm:col-span-2"><Field label="Expected filesystem device ID" hint="Detect only while the intended disk or NAS is mounted. A changed device number blocks writes and needs review."><input className={inputClass} required pattern="[0-9]+" value={filesystem} onChange={(event) => { setFilesystem(event.target.value); setDetected(false) }} placeholder="2049" /></Field><button type="button" className={buttonClass} disabled={!repository.trim() || pending} onClick={() => { void detectFilesystem() }}>Detect current filesystem</button>{detected && <p role="status" className="text-xs text-[var(--warning)]">Detected device {filesystem}. Verify the intended disk is mounted at this path before saving.</p>}</div>}
          <div className="sm:col-span-2"><Field label="Repository password" hint={destination ? 'Leave secret fields empty to keep saved credentials. Entering a password changes the saved access credential; it does not change the repository encryption password.' : 'Keep this password and the destination credentials independently of this host. They are required for recovery.'}>
            <input type="password" autoComplete="new-password" required={!destination} value={secrets.password ?? ''} onChange={(event) => changeSecret('password', event.target.value)} className={inputClass} />
          </Field></div>
          {kind === 's3' && <>
            <Field label="Access key ID"><input className={inputClass} autoComplete="off" value={secrets.access_key_id ?? ''} required={!destination} onChange={(event) => changeSecret('access_key_id', event.target.value)} /></Field>
            <Field label="Secret access key"><input type="password" autoComplete="new-password" className={inputClass} value={secrets.secret_access_key ?? ''} required={!destination} onChange={(event) => changeSecret('secret_access_key', event.target.value)} /></Field>
            <Field label="Region (optional)"><input className={inputClass} value={region} onChange={(event) => setRegion(event.target.value)} /></Field>
            <Field label="Session token (optional)"><input type="password" autoComplete="new-password" className={inputClass} value={secrets.session_token ?? ''} onChange={(event) => changeSecret('session_token', event.target.value)} /></Field>
          </>}
          {kind === 'sftp' && <>
            <div className="sm:col-span-2"><Field label="SSH private key"><textarea className={inputClass} rows={5} autoComplete="off" spellCheck={false} required={!destination} value={secrets.ssh_private_key ?? ''} onChange={(event) => changeSecret('ssh_private_key', event.target.value)} /></Field></div>
            <div className="sm:col-span-2"><Field label="Trusted SSH known_hosts" hint="Verify the NAS host key independently before pasting its known_hosts entry."><textarea className={inputClass} rows={3} spellCheck={false} required={!destination} value={secrets.ssh_known_hosts ?? ''} onChange={(event) => changeSecret('ssh_known_hosts', event.target.value)} /></Field></div>
          </>}
          {kind === 'rclone' && <div className="sm:col-span-2"><Field label="Authorized rclone configuration" hint="Configure the remote with rclone first. This configuration can include access and refresh tokens and is stored privately."><textarea className={inputClass} rows={6} autoComplete="off" spellCheck={false} required={!destination} value={secrets.rclone_config ?? ''} onChange={(event) => changeSecret('rclone_config', event.target.value)} /></Field></div>}
        </fieldset>
        <p className="text-xs leading-5 text-[var(--muted)]">Credentials are write-only. Saving a destination does not initialize or modify its repository. Test access after saving; initialize only a new repository.</p>
        <div className="flex justify-end gap-2"><button type="button" className={buttonClass} disabled={pending} onClick={onClose}>Cancel</button><button type="submit" className={primaryClass} disabled={pending}>{pending ? 'Saving…' : 'Save destination'}</button></div>
      </form>
    </Dialog>
  )
}
