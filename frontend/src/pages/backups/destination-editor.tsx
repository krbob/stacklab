import { useState, type FormEvent } from 'react'
import { Dialog } from '@/components/dialog'
import { useJobStream } from '@/hooks/use-job-stream'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import {
  getBackupFilesystem,
  saveBackupDestination,
  testBackupDestination,
  initializeBackupDestination,
  type BackupDestination,
  type BackupSecrets,
  type BackupSchedule,
} from '@/lib/backup-api'
import { BackupError, buttonClass, Field, inputClass, primaryClass, BackupStatus } from './backup-primitives'
import { ScheduleFields } from './schedule-fields'
import { manualSchedule } from './schedule-utils'

const repositoryHints: Record<string, string> = {
  local: 'Absolute directory on a disk or mounted NAS, for example /mnt/backup/stacklab.',
  s3: 'S3-compatible HTTPS endpoint, dedicated bucket and optional prefix.',
  sftp: 'An SSH server with a pinned host key and dedicated backup account.',
  rclone: 'An authorized rclone remote, for example Google Drive.',
}

export function DestinationEditor({
  destination,
  onSaved,
  onClose,
  onCreatePlan,
}: {
  destination?: BackupDestination
  onSaved: () => void
  onClose: () => void
  onCreatePlan?: (destination: BackupDestination) => void
}) {
  const [step, setStep] = useState(0)
  const [saved, setSaved] = useState<BackupDestination | null>(null)
  const [name, setName] = useState(destination?.name ?? '')
  const [kind, setKind] = useState<BackupDestination['kind']>(destination?.kind ?? 'local')
  const [repository, setRepository] = useState(destination?.repository ?? '')
  const [filesystem, setFilesystem] = useState(destination?.expected_filesystem ?? '')
  const [region, setRegion] = useState(destination?.region ?? '')
  const [secrets, setSecrets] = useState<BackupSecrets>({})
  const [advanced, setAdvanced] = useState(!!destination)
  const [location, setLocation] = useState({
    endpoint: '',
    bucket: '',
    prefix: 'stacklab',
    user: 'backup',
    host: '',
    port: '22',
    directory: '/backups/stacklab',
    remote: 'gdrive',
    remotePath: 'stacklab',
  })
  const [showPassword, setShowPassword] = useState(false)
  const [secretMessage, setSecretMessage] = useState('')
  const [checkSchedule, setCheckSchedule] = useState<BackupSchedule>(destination?.check_schedule ?? manualSchedule)
  const [pruneSchedule, setPruneSchedule] = useState<BackupSchedule>(destination?.prune_schedule ?? manualSchedule)
  const [readData, setReadData] = useState<'metadata' | 'all' | 'subset'>(
    destination?.check_read_data === 'all' || destination?.check_read_data === 'subset'
      ? destination.check_read_data
      : 'metadata',
  )
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [detected, setDetected] = useState(false)
  const [newRepository, setNewRepository] = useState(false)
  const [jobId, setJobId] = useState<string | null>(null)
  const [jobAction, setJobAction] = useState('')
  const { state: jobState } = useJobStream({ jobId })
  const { openJob } = useJobDrawer()
  const running = !!jobId && !['succeeded', 'failed', 'cancelled', 'timed_out'].includes(jobState ?? '')
  const changeSecret = (key: keyof BackupSecrets, value: string) =>
    setSecrets((previous) => ({ ...previous, [key]: value }))
  const changeLocation = (key: keyof typeof location, value: string) =>
    setLocation((previous) => ({ ...previous, [key]: value }))
  const repositoryValue =
    advanced || kind === 'local'
      ? repository.trim()
      : kind === 's3'
        ? `s3:${location.endpoint.replace(/\/+$/, '')}/${location.bucket}/${location.prefix.replace(/^\/+|\/+$/g, '')}`.replace(
            /\/$/,
            '',
          )
        : kind === 'sftp'
          ? `sftp://${encodeURIComponent(location.user)}@${location.host}${location.port && location.port !== '22' ? `:${location.port}` : ''}/${location.directory.replace(/^\/+/, '')}`
          : `rclone:${location.remote}:${location.remotePath}`

  async function detectFilesystem() {
    setPending(true)
    setError(null)
    try {
      const result = await getBackupFilesystem(repositoryValue)
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
    if (!destination && step === 0) {
      setStep(1)
      return
    }
    setPending(true)
    setError(null)
    const supplied = Object.fromEntries(Object.entries(secrets).filter(([, value]) => value !== ''))
    try {
      const result = await saveBackupDestination(
        {
          name: name.trim(),
          kind,
          repository: repositoryValue,
          expected_filesystem: kind === 'local' ? filesystem.trim() : undefined,
          region: kind === 's3' ? region.trim() : undefined,
          secrets: Object.keys(supplied).length > 0 ? supplied : undefined,
          check_schedule: checkSchedule,
          check_read_data: readData,
          prune_schedule: pruneSchedule,
        },
        destination?.id,
      )
      setSecrets({})
      if (destination) onSaved()
      else {
        setSaved(result.destination)
        setStep(2)
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to save destination')
    } finally {
      setPending(false)
    }
  }

  async function verify(initialize: boolean) {
    if (!saved || pending || running) return
    setPending(true)
    setError(null)
    try {
      const result = await (initialize ? initializeBackupDestination : testBackupDestination)(saved.id)
      setJobId(result.job.id)
      setJobAction(initialize ? 'Initialize repository' : 'Test access')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to start verification')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog
      title={destination ? 'Edit destination' : 'Add destination'}
      onClose={saved ? onSaved : onClose}
      preventClose={pending}
      busy={pending}
      panelClassName="max-w-2xl"
    >
      {!destination && (
        <ol aria-label="Destination setup steps" className="mt-4 flex gap-2 text-xs text-[var(--muted)]">
          {['Location', 'Credentials & policy', 'Verify'].map((label, index) => (
            <li
              key={label}
              aria-current={step === index ? 'step' : undefined}
              className={step === index ? 'font-semibold text-[var(--text)]' : ''}
            >
              {index + 1}. {label}
            </li>
          ))}
        </ol>
      )}
      {step === 2 && saved ? (
        <div className="mt-4 space-y-4">
          <BackupError error={error} />
          <p className="text-sm text-[var(--text)]">
            {saved.name} is saved. Verify an existing repository, or initialize this location if it is new.
          </p>
          <p className="break-all text-xs text-[var(--muted)]">{saved.repository}</p>
          <p className="text-xs leading-5 text-[var(--warning)]">
            Keep the repository password and access credentials independently of this host. Stacklab cannot recover an
            encrypted repository without them.
          </p>
          <button
            className={primaryClass}
            disabled={pending || running}
            onClick={() => {
              void verify(false)
            }}
          >
            Test existing repository
          </button>
          <div className="space-y-2 rounded-md border border-[var(--panel-border)] p-3">
            <label className="flex items-start gap-2 text-sm text-[var(--text)]">
              <input
                type="checkbox"
                checked={newRepository}
                onChange={(event) => setNewRepository(event.target.checked)}
              />
              This location is new and needs an encrypted repository
            </label>
            <button
              className={buttonClass}
              disabled={!newRepository || pending || running}
              onClick={() => {
                void verify(true)
              }}
            >
              Initialize new repository
            </button>
          </div>
          {jobId && (
            <div role="status" className="space-y-2 text-sm text-[var(--text)]">
              <span>{jobAction}: </span>
              <BackupStatus value={jobState ?? 'queued'} />
              <button className={buttonClass} onClick={() => openJob(jobId)}>
                Operation details
              </button>
              {jobState === 'succeeded' && (
                <p className="text-xs text-[var(--muted)]">
                  Repository ready. Create a plan, select its sources, then use Back up now for the first copy.
                </p>
              )}
              {['failed', 'cancelled', 'timed_out'].includes(jobState ?? '') && (
                <p className="text-xs text-[var(--warning)]">
                  Verification did not finish successfully. Open the operation details, then review credentials or
                  retry.
                </p>
              )}
            </div>
          )}
          <div className="flex justify-end">
            <button
              className={primaryClass}
              onClick={() => {
                if (jobState === 'succeeded' && onCreatePlan) onCreatePlan(saved)
                else onSaved()
              }}
            >
              {jobState === 'succeeded' ? 'Done — create a backup plan' : 'Finish setup'}
            </button>
          </div>
        </div>
      ) : (
        <form
          onSubmit={(event) => {
            void save(event)
          }}
          className="mt-4 space-y-4"
        >
          <BackupError error={error} />
          <fieldset disabled={pending} className="grid min-w-0 gap-4 sm:grid-cols-2">
            {(!!destination || step === 0) && (
              <>
                <Field label="Destination name">
                  <input
                    className={inputClass}
                    required
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                  />
                </Field>
                <Field label="Storage type">
                  <select
                    className={inputClass}
                    disabled={!!destination}
                    value={kind}
                    onChange={(event) => {
                      setKind(event.target.value as BackupDestination['kind'])
                      setRepository('')
                      setSecrets({})
                      setFilesystem('')
                      setDetected(false)
                    }}
                  >
                    <option value="local">Local disk / mounted NAS</option>
                    <option value="s3">S3 / Backblaze B2</option>
                    <option value="sftp">NAS over SFTP</option>
                    <option value="rclone">Google Drive / rclone</option>
                  </select>
                </Field>
                <p className="text-xs leading-5 text-[var(--muted)] sm:col-span-2">
                  {repositoryHints[kind]}
                  {destination &&
                    ' Repository type and address are fixed. Add a new destination to use another location.'}
                </p>
                {!destination && kind !== 'local' && (
                  <label className="flex items-center gap-2 text-sm text-[var(--text)] sm:col-span-2">
                    <input
                      type="checkbox"
                      checked={advanced}
                      onChange={(event) => {
                        setRepository(repositoryValue)
                        setAdvanced(event.target.checked)
                      }}
                    />
                    Enter repository address directly
                  </label>
                )}
                {advanced || kind === 'local' ? (
                  <div className="sm:col-span-2">
                    <Field label="Repository">
                      <input
                        className={inputClass}
                        disabled={!!destination}
                        required
                        value={repository}
                        onChange={(event) => {
                          setRepository(event.target.value)
                          if (kind === 'local') {
                            setFilesystem('')
                            setDetected(false)
                          }
                        }}
                        autoComplete="off"
                      />
                    </Field>
                  </div>
                ) : kind === 's3' ? (
                  <>
                    <div className="sm:col-span-2">
                      <Field label="S3 HTTPS endpoint">
                        <input
                          className={inputClass}
                          type="url"
                          pattern="https://.+"
                          required
                          placeholder="https://s3.us-west-004.backblazeb2.com"
                          value={location.endpoint}
                          onChange={(event) => changeLocation('endpoint', event.target.value)}
                        />
                      </Field>
                    </div>
                    <Field label="Bucket">
                      <input
                        className={inputClass}
                        required
                        value={location.bucket}
                        onChange={(event) => changeLocation('bucket', event.target.value)}
                      />
                    </Field>
                    <Field label="Repository prefix">
                      <input
                        className={inputClass}
                        value={location.prefix}
                        onChange={(event) => changeLocation('prefix', event.target.value)}
                      />
                    </Field>
                  </>
                ) : kind === 'sftp' ? (
                  <>
                    <Field label="SSH host">
                      <input
                        className={inputClass}
                        required
                        value={location.host}
                        onChange={(event) => changeLocation('host', event.target.value)}
                      />
                    </Field>
                    <Field label="SSH port">
                      <input
                        className={inputClass}
                        type="number"
                        min={1}
                        max={65535}
                        required
                        value={location.port}
                        onChange={(event) => changeLocation('port', event.target.value)}
                      />
                    </Field>
                    <Field label="SSH user">
                      <input
                        className={inputClass}
                        required
                        value={location.user}
                        onChange={(event) => changeLocation('user', event.target.value)}
                      />
                    </Field>
                    <Field label="Remote directory">
                      <input
                        className={inputClass}
                        required
                        value={location.directory}
                        onChange={(event) => changeLocation('directory', event.target.value)}
                      />
                    </Field>
                  </>
                ) : (
                  <>
                    <Field label="Rclone remote name">
                      <input
                        className={inputClass}
                        required
                        value={location.remote}
                        onChange={(event) => changeLocation('remote', event.target.value)}
                      />
                    </Field>
                    <Field label="Remote directory">
                      <input
                        className={inputClass}
                        required
                        value={location.remotePath}
                        onChange={(event) => changeLocation('remotePath', event.target.value)}
                      />
                    </Field>
                  </>
                )}
                {!advanced && kind !== 'local' && (
                  <p className="break-all text-xs text-[var(--muted)] sm:col-span-2">Repository: {repositoryValue}</p>
                )}
                {kind === 'local' && (
                  <div className="space-y-2 sm:col-span-2">
                    <Field
                      label="Expected filesystem device ID"
                      hint="Detect while the intended disk is mounted. A changed device number blocks writes until reviewed."
                    >
                      <input
                        className={inputClass}
                        required
                        pattern="[0-9]+"
                        value={filesystem}
                        onChange={(event) => {
                          setFilesystem(event.target.value)
                          setDetected(false)
                        }}
                        placeholder="2049"
                      />
                    </Field>
                    <button
                      type="button"
                      className={buttonClass}
                      disabled={!repositoryValue || pending}
                      onClick={() => {
                        void detectFilesystem()
                      }}
                    >
                      Detect current filesystem
                    </button>
                    {detected && (
                      <p role="status" className="text-xs text-[var(--warning)]">
                        Detected device {filesystem}. Verify the intended disk is mounted before saving.
                      </p>
                    )}
                  </div>
                )}
              </>
            )}
            {(!!destination || step === 1) && (
              <>
                <div className="space-y-2 sm:col-span-2">
                  <Field
                    label="Repository password"
                    hint={
                      destination
                        ? 'Leave secrets empty to keep saved credentials. Changing this field does not change repository encryption.'
                        : 'Save this password independently of this host. It is required for recovery.'
                    }
                  >
                    <input
                      type={showPassword ? 'text' : 'password'}
                      autoComplete="new-password"
                      required={!destination}
                      value={secrets.password ?? ''}
                      onChange={(event) => changeSecret('password', event.target.value)}
                      className={inputClass}
                    />
                  </Field>
                  <div className="flex flex-wrap gap-2">
                    <button
                      type="button"
                      className={buttonClass}
                      onClick={() => {
                        const bytes = crypto.getRandomValues(new Uint8Array(24))
                        changeSecret(
                          'password',
                          Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join(''),
                        )
                        setSecretMessage('Generated a new password. Store it independently before saving.')
                      }}
                    >
                      Generate password
                    </button>
                    <button type="button" className={buttonClass} onClick={() => setShowPassword(!showPassword)}>
                      {showPassword ? 'Hide password' : 'Show password'}
                    </button>
                    <button
                      type="button"
                      className={buttonClass}
                      disabled={!secrets.password}
                      onClick={() => {
                        void (
                          navigator.clipboard
                            ? navigator.clipboard.writeText(secrets.password ?? '')
                            : Promise.reject(new Error('Clipboard unavailable'))
                        )
                          .then(() => setSecretMessage('Password copied. Store it in your password manager.'))
                          .catch(() =>
                            setSecretMessage('Clipboard is unavailable. Show the password and copy it manually.'),
                          )
                      }}
                    >
                      Copy password
                    </button>
                  </div>
                  {secretMessage && (
                    <p role="status" className="text-xs text-[var(--muted)]">
                      {secretMessage}
                    </p>
                  )}
                </div>
                {kind === 's3' && (
                  <>
                    <Field label="Access key ID">
                      <input
                        className={inputClass}
                        autoComplete="off"
                        value={secrets.access_key_id ?? ''}
                        required={!destination}
                        onChange={(event) => changeSecret('access_key_id', event.target.value)}
                      />
                    </Field>
                    <Field label="Secret access key">
                      <input
                        type="password"
                        autoComplete="new-password"
                        className={inputClass}
                        value={secrets.secret_access_key ?? ''}
                        required={!destination}
                        onChange={(event) => changeSecret('secret_access_key', event.target.value)}
                      />
                    </Field>
                    <Field label="Region (optional)">
                      <input
                        className={inputClass}
                        value={region}
                        onChange={(event) => setRegion(event.target.value)}
                      />
                    </Field>
                    <Field label="Session token (optional)">
                      <input
                        type="password"
                        autoComplete="new-password"
                        className={inputClass}
                        value={secrets.session_token ?? ''}
                        onChange={(event) => changeSecret('session_token', event.target.value)}
                      />
                    </Field>
                  </>
                )}
                {kind === 'sftp' && (
                  <>
                    <div className="sm:col-span-2">
                      <Field label="SSH private key">
                        <textarea
                          className={inputClass}
                          rows={5}
                          autoComplete="off"
                          spellCheck={false}
                          required={!destination}
                          value={secrets.ssh_private_key ?? ''}
                          onChange={(event) => changeSecret('ssh_private_key', event.target.value)}
                        />
                      </Field>
                    </div>
                    <div className="sm:col-span-2">
                      <Field
                        label="Trusted SSH known_hosts"
                        hint="Verify the server host key independently before pasting its known_hosts entry."
                      >
                        <textarea
                          className={inputClass}
                          rows={3}
                          spellCheck={false}
                          required={!destination}
                          value={secrets.ssh_known_hosts ?? ''}
                          onChange={(event) => changeSecret('ssh_known_hosts', event.target.value)}
                        />
                      </Field>
                    </div>
                  </>
                )}
                {kind === 'rclone' && (
                  <div className="sm:col-span-2">
                    <Field
                      label="Authorized rclone configuration"
                      hint="Configure the remote with rclone first; its access and refresh tokens are stored privately."
                    >
                      <textarea
                        className={inputClass}
                        rows={6}
                        autoComplete="off"
                        spellCheck={false}
                        required={!destination}
                        value={secrets.rclone_config ?? ''}
                        onChange={(event) => changeSecret('rclone_config', event.target.value)}
                      />
                    </Field>
                  </div>
                )}
                <details className="space-y-3 rounded-md border border-[var(--panel-border)] p-3 sm:col-span-2">
                  <summary className="cursor-pointer text-sm">Repository maintenance schedules</summary>
                  <ScheduleFields label="Repository check schedule" value={checkSchedule} onChange={setCheckSchedule} />
                  <Field label="Scheduled check policy">
                    <select
                      className={inputClass}
                      value={readData}
                      onChange={(event) => setReadData(event.target.value as typeof readData)}
                    >
                      <option value="metadata">Repository metadata</option>
                      <option value="subset">Read a data subset</option>
                      <option value="all">Read all stored data</option>
                    </select>
                  </Field>
                  <ScheduleFields
                    label="Retention cleanup schedule"
                    value={pruneSchedule}
                    onChange={setPruneSchedule}
                  />
                  <p className="text-xs leading-5 text-[var(--muted)]">
                    A check verifies stored data; it does not test application recovery. A scheduled cleanup separates
                    pruning from uploads. Manual cleanup keeps the existing after-copy policy.
                  </p>
                </details>
              </>
            )}
          </fieldset>
          <p className="text-xs leading-5 text-[var(--muted)]">
            Credentials are write-only. Saving does not initialize or modify the repository.
          </p>
          <div className="flex flex-wrap justify-end gap-2">
            <button type="button" className={buttonClass} disabled={pending} onClick={onClose}>
              Cancel
            </button>
            {!destination && step === 1 && (
              <button type="button" className={buttonClass} disabled={pending} onClick={() => setStep(0)}>
                Back
              </button>
            )}
            <button type="submit" className={primaryClass} disabled={pending}>
              {pending ? 'Saving…' : !destination && step === 0 ? 'Continue to credentials' : 'Save destination'}
            </button>
          </div>
        </form>
      )}
    </Dialog>
  )
}
