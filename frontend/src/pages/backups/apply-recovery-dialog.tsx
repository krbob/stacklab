import { useEffect, useRef, useState } from 'react'
import { Dialog } from '@/components/dialog'
import { useApi } from '@/hooks/use-api'
import { OperationReview } from '@/components/operation-review'
import {
  getBackupRestoreManifest,
  previewBackupRestore,
  restoreBackupSnapshot,
  type BackupDestination,
  type BackupJobResponse,
  type BackupRestoreMapping,
  type BackupRestoreRequest,
  type BackupRestoreReview,
  type BackupRestoreResult,
} from '@/lib/backup-api'
import { BackupDate, BackupError, buttonClass, Field, inputClass, primaryClass } from './backup-primitives'
import { SourceLabel } from './source-label'
import { formatBackupDate } from './backup-formatters'

export function ApplyRecoveryDialog({
  destination,
  restored,
  onStarted,
  onClose,
}: {
  destination: BackupDestination
  restored: BackupRestoreResult
  onStarted: (result: BackupJobResponse) => void
  onClose: () => void
}) {
  const manifestState = useApi(
    () =>
      restored.manifest ? Promise.resolve({ manifest: restored.manifest }) : getBackupRestoreManifest(restored.target),
    [restored.id, restored.target, restored.manifest],
  )
  const manifest = restored.manifest ?? manifestState.data?.manifest
  const [mappings, setMappings] = useState<BackupRestoreMapping[]>([])
  const [resume, setResume] = useState(false)
  const [review, setReview] = useState<BackupRestoreReview | null>(null)
  const [confirmation, setConfirmation] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const confirmationRef = useRef<HTMLInputElement>(null)
  const unsafeReplacement = mappings.some((mapping) =>
    (mapping.mode ?? 'replace') === 'replace' && !!manifest?.sources.find((item) => item.source.id === mapping.source_id)?.source.excludes?.length,
  )
  useEffect(() => {
    if (review) confirmationRef.current?.focus()
  }, [review])

  function payload(): BackupRestoreRequest {
    return { restore_id: restored.id, mappings, resume_services: resume }
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
    setReview(null)
    setConfirmation('')
    setMappings((previous) =>
      previous.map((mapping) => (mapping.source_id === sourceId ? { ...mapping, ...patch } : mapping)),
    )
  }

  return (
    <Dialog
      title={`Apply recovered data · ${destination.name}`}
      onClose={onClose}
      preventClose={pending}
      busy={pending}
      panelClassName="max-w-3xl"
    >
      <div className="mt-4 space-y-4">
        <BackupError error={error} />
        {!manifest && manifestState.loading && (
          <p role="status" className="text-sm text-[var(--muted)]">
            Loading recovery manifest…
          </p>
        )}
        <BackupError error={manifestState.error?.message ?? null} />
        {manifestState.error && (
          <button className={buttonClass} onClick={manifestState.refetch}>
            Retry recovery manifest
          </button>
        )}
        <p className="text-sm leading-6 text-[var(--muted)]">
          Select the recovered sources to apply, then review destination paths and affected services. Targets and
          service restart are explicit choices.
        </p>
        {!review && (
          <>
            {manifest && (
              <>
                <p className="text-xs text-[var(--muted)]">
                  Captured <BackupDate value={manifest.captured_at} /> · {manifest.consistency} · Manifest v
                  {manifest.version}
                </p>
                <fieldset disabled={pending} className="space-y-3">
                  {manifest.sources
                    .filter((item) => !restored?.source_ids?.length || restored.source_ids.includes(item.source.id))
                    .map((item) => {
                      const mapping = mappings.find((value) => value.source_id === item.source.id)
                      const offline = item.source.kind === 'stacklab' || !!item.recovery_restrictions?.length
                      return (
                        <div
                          key={item.source.id}
                          className="space-y-3 rounded-md border border-[var(--panel-border)] p-3"
                        >
                          <label className="flex items-start gap-2 text-sm text-[var(--text)]">
                            <input
                              type="checkbox"
                              className="mt-1"
                              disabled={offline}
                              checked={!!mapping}
                              onChange={(event) =>
                                setMappings((previous) =>
                                  event.target.checked
                                    ? [
                                        ...previous,
                                        {
                                          source_id: item.source.id,
                                          target: '',
                                          mode: item.file_name ? 'merge' : 'replace',
                                        },
                                      ]
                                    : previous.filter((value) => value.source_id !== item.source.id),
                                )
                              }
                            />
                            <SourceLabel source={item.source} />
                          </label>
                          {offline && (
                            <p className="text-xs text-[var(--warning)]">
                              {item.source.kind === 'stacklab'
                                ? 'Stacklab private state and SQLite must be recovered offline using the recovery runbook.'
                                : `This source preserves metadata requiring offline recovery: ${item.recovery_restrictions?.join('; ')}. Download its original archive and follow the recovery runbook.`}
                            </p>
                          )}
                          {!!item.source.excludes?.length && <p className="text-xs text-[var(--warning)]">
                            Excluded from this source: {item.source.excludes.join(', ')}. Replacement is blocked because
                            those live paths are absent from the backup. Inspect the original archive for offline recovery
                            or explicitly choose merge when that meets your application’s recovery requirements.
                          </p>}
                          {mapping && (
                            <div className="grid gap-3 sm:grid-cols-2">
                              <div className="sm:col-span-2">
                                <Field
                                  label={`Recovery mode for ${item.source.service || item.source.kind}`}
                                  hint={
                                    item.file_name
                                      ? 'A single-file source overwrites only its named file and preserves neighboring files.'
                                      : item.source.excludes?.length ? 'Replace is unavailable because this source excluded data. Choose merge explicitly only when preserving extra files is appropriate; merge is unsuitable for exact database recovery.' : 'Replace removes files absent from this recovery source. Merge preserves extra files and is unsuitable for exact database recovery.'
                                  }
                                >
                                  <select
                                    className={inputClass}
                                    value={mapping.mode ?? 'replace'}
                                    disabled={!!item.file_name}
                                    onChange={(event) =>
                                      updateMapping(item.source.id, { mode: event.target.value as 'replace' | 'merge' })
                                    }
                                  >
                                    <option value="replace" disabled={!!item.source.excludes?.length}>Replace complete source contents</option>
                                    <option value="merge">Merge and preserve extra files</option>
                                  </select>
                                </Field>
                              </div>
                              <Field
                                label={`Target directory for ${item.source.service || item.source.kind}`}
                                hint={`Original path: ${item.original_path}${item.source.kind === 'managed_config' ? '. For a backed-up file, choose its parent directory; extraction retains the filename.' : ''}`}
                              >
                                <input
                                  className={inputClass}
                                  value={mapping.target}
                                  disabled={!!mapping.volume_name}
                                  onChange={(event) => updateMapping(item.source.id, { target: event.target.value })}
                                  placeholder="Explicit absolute target directory"
                                />
                              </Field>
                              {item.volume_name && (
                                <Field
                                  label={`Existing volume for ${item.source.service || item.source.kind}`}
                                  hint="Create the destination volume first. A volume name replaces the directory mapping."
                                >
                                  <input
                                    className={inputClass}
                                    value={mapping.volume_name ?? ''}
                                    onChange={(event) =>
                                      updateMapping(item.source.id, {
                                        volume_name: event.target.value || undefined,
                                        target: '',
                                      })
                                    }
                                    placeholder={item.volume_name}
                                  />
                                </Field>
                              )}
                              <button
                                type="button"
                                className={buttonClass}
                                onClick={() =>
                                  updateMapping(
                                    item.source.id,
                                    item.volume_name
                                      ? { volume_name: item.volume_name, target: '' }
                                      : {
                                          target: item.file_name
                                            ? item.original_path.slice(0, item.original_path.lastIndexOf('/')) || '/'
                                            : item.original_path,
                                        },
                                  )
                                }
                              >
                                Use original location
                              </button>
                            </div>
                          )}
                        </div>
                      )
                    })}
                  <label className="flex items-start gap-2 text-sm text-[var(--text)]">
                    <input
                      type="checkbox"
                      className="mt-1"
                      checked={resume}
                      onChange={(event) => setResume(event.target.checked)}
                    />
                    Restart services that were running before recovery
                  </label>
                </fieldset>
                <p className="text-xs leading-5 text-[var(--warning)]">
                  Replacement restores the selected source contents and removes every target entry absent from that source. Merge keeps
                  extra files. Affected running writers remain stopped by default. A failure after writing begins also
                  leaves them stopped for inspection; keep a separate current backup before applying recovery.
                </p>
                {(manifest.images?.length ?? 0) > 0 && (
                  <p className="break-all text-xs text-[var(--muted)]">
                    Required images: {manifest.images?.join(', ')}. Registry access, external networks, secrets and
                    build inputs must be available on this host.
                  </p>
                )}
                {manifest.warnings?.map((warning) => (
                  <p key={warning} className="text-xs text-[var(--warning)]">
                    {warning}
                  </p>
                ))}
              </>
            )}
          </>
        )}
        {review && (
          <>
            <OperationReview
              review={{
                target: review.preview.mappings
                  .map((mapping) =>
                    mapping.volume_name ? `${mapping.volume_name} (${mapping.target})` : mapping.target,
                  )
                  .join(', '),
                scope: review.preview.mappings.map((mapping) => {
                  const source = manifest?.sources.find((item) => item.source.id === mapping.source_id)?.source
                  const label = source
                    ? [source.stack_id, source.service, source.mount || source.kind].filter(Boolean).join(' / ')
                    : mapping.source_id
                  return `${label} → ${mapping.target} (${mapping.mode ?? 'replace'})`
                }),
                impact: [
                  review.preview.overwrites ? 'Server inspection confirms existing target contents may be overwritten.' : 'Server inspection found no existing target contents to overwrite.',
                  ...review.preview.mappings.map((mapping) =>
                    (mapping.mode ?? 'replace') === 'replace'
                      ? `Replace ${mapping.target}: entries absent from the recovered source will be removed.`
                      : `Merge ${mapping.target}: matching files will be overwritten; extra files remain.`,
                  ),
                  ...(review.preview.writers ?? [])
                    .filter((writer) => writer.running)
                    .map(
                      (writer) =>
                        `Stop writer: ${writer.stack_id || 'external'}/${writer.service || writer.container_id}`,
                    ),
                  resume
                    ? 'Restart previously running writers after recovery succeeds.'
                    : 'Leave affected writers stopped after recovery.',
                ],
                snapshot: `Recovery set captured ${manifest?.captured_at ? formatBackupDate(manifest.captured_at) : 'at unknown time'}; review expires ${formatBackupDate(review.expires_at)}`,
                recovery:
                  'Keep a separate current backup before replacing live data. Review Compose and application prerequisites before starting services.',
              }}
            />
            {(review.preview.warnings ?? []).map((warning) => (
              <p key={warning} className="text-xs text-[var(--warning)]">
                {warning}
              </p>
            ))}
            <Field label="Type RESTORE to apply recovered data">
              <input
                ref={confirmationRef}
                className={inputClass}
                value={confirmation}
                disabled={pending}
                autoComplete="off"
                onChange={(event) => setConfirmation(event.target.value)}
              />
            </Field>
          </>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          <button className={buttonClass} disabled={pending} onClick={onClose}>
            Cancel
          </button>
          {review ? (
            <>
              <button
                className={buttonClass}
                disabled={pending}
                onClick={() => {
                  setReview(null)
                  setConfirmation('')
                }}
              >
                Change mappings
              </button>
              <button
                className="rounded-md border border-[var(--danger)]/40 bg-[var(--danger)]/10 px-3 py-2 text-sm text-[var(--danger)] disabled:opacity-50"
                disabled={pending || confirmation !== 'RESTORE'}
                onClick={() => {
                  void apply()
                }}
              >
                {pending ? 'Starting…' : 'Apply recovered data'}
              </button>
            </>
          ) : (
            <button
              className={primaryClass}
              disabled={
                pending ||
                unsafeReplacement ||
                !manifest ||
                mappings.length === 0 ||
                mappings.some((mapping) => !mapping.target.trim() && !mapping.volume_name?.trim())
              }
              onClick={() => {
                void preview()
              }}
            >
              Preview recovery
            </button>
          )}
        </div>
      </div>
    </Dialog>
  )
}
