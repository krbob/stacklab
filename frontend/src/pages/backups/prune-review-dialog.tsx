import { useId, useLayoutEffect, useRef, useState } from 'react'
import { Dialog } from '@/components/dialog'
import type { BackupDestination } from '@/lib/backup-api'
import { BackupDate, BackupError, BackupStatus, buttonClass, Field, inputClass, primaryClass } from './backup-primitives'
import { checkScopeLabel, requiredCheckScope } from './repository-evidence'
import { CheckHoldWarning } from './check-hold-warning'

const refreshNotice = 'Refreshing repository evidence. Cleanup is disabled until this finishes.'
const resetNotice = 'Repository evidence changed. Typed confirmation was reset; review the current evidence before confirming again.'
const clearNotice = 'Repository evidence is now clear; acknowledgement is no longer required.'

// Bind the typed confirmation to the evidence fingerprint without remounting
// the dialog: changed evidence clears the field and explains the reset.
export function PruneReviewDialog({ destination, revision = 0, pending, refreshing, refreshFailed, error, onCancel, onConfirm, onRefresh }: {
  destination: BackupDestination
  revision?: number
  pending: boolean
  refreshing: boolean
  refreshFailed: boolean
  error: string | null
  onCancel: () => void
  onConfirm: (reviewToken?: string) => void
  onRefresh: () => void
}) {
  const [confirmation, setConfirmation] = useState('')
  const [previousRevision, setPreviousRevision] = useState(revision)
  const [previousName, setPreviousName] = useState(destination.name)
  const [reviewToken, setReviewToken] = useState(destination.maintenance_review_token)
  const requiresAcknowledgement = !!requiredCheckScope(destination)
  const [previousRequirement, setPreviousRequirement] = useState(requiresAcknowledgement)
  const [evidenceNotice, setEvidenceNotice] = useState<string | null>(null)
  const statusId = useId()
  if (reviewToken !== destination.maintenance_review_token || previousRequirement !== requiresAcknowledgement || previousRevision !== revision || previousName !== destination.name) {
    setPreviousRevision(revision)
    setPreviousName(destination.name)
    setReviewToken(destination.maintenance_review_token)
    setPreviousRequirement(requiresAcknowledgement)
    setConfirmation('')
    if (previousRequirement !== requiresAcknowledgement) {
      setEvidenceNotice(requiresAcknowledgement
        ? 'Cleanup now requires acknowledgement of new repository evidence.'
        : clearNotice)
    } else if (confirmation !== '') {
      setEvidenceNotice(previousName !== destination.name
        ? 'Destination name changed. Typed confirmation was reset; enter the current name before confirming again.'
        : resetNotice)
    }
  }
  const textRef = useRef<HTMLInputElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const [initialFocusRef] = useState(() => requiresAcknowledgement ? textRef : cancelRef)
  const focusRequirement = useRef(requiresAcknowledgement)
  useLayoutEffect(() => {
    if (focusRequirement.current !== requiresAcknowledgement) {
      focusRequirement.current = requiresAcknowledgement
      const nextFocus = requiresAcknowledgement ? textRef : cancelRef
      nextFocus.current?.focus()
    }
  }, [requiresAcknowledgement])
  const status = pending ? 'Starting cleanup. Please wait.' : refreshing
    ? refreshNotice
    : !error ? evidenceNotice : null
  const failures = ([['Repository metadata evidence', destination.metadata_check], ['Stored data evidence', destination.data_check]] as const).filter(([, evidence]) => evidence.status === 'failed')
  return (
    <Dialog title="Run retention cleanup" onClose={onCancel} preventClose={pending} initialFocusRef={initialFocusRef}>
      <div className="mt-4 space-y-4">
        <p className="text-sm text-[var(--muted)]">Reclaim storage no longer referenced by retained snapshots. Cleanup locks this repository and may take time or incur provider transfer charges.</p>
        <p className="break-all font-mono text-xs text-[var(--text)]">{destination.repository}</p>
        <BackupError error={error} />
        <div className="grid text-sm leading-5">
          {[refreshNotice, requiresAcknowledgement ? resetNotice : clearNotice].map(message => <span key={message} aria-hidden="true" data-placeholder={message} className="invisible col-start-1 row-start-1 after:content-[attr(data-placeholder)]" />)}
          <p id={statusId} role="status" aria-atomic="true" className={`col-start-1 row-start-1 ${requiresAcknowledgement && !refreshing && !pending ? 'text-[var(--warning)]' : 'text-[var(--muted)]'}`}>{status}</p>
        </div>
        {requiresAcknowledgement && !destination.maintenance_review_token && <p className="text-sm text-[var(--warning)]">The current evidence review token is unavailable. Refresh repository evidence before confirming cleanup.</p>}
        {(error || (requiresAcknowledgement && !destination.maintenance_review_token)) && <button className={buttonClass} disabled={pending || refreshing} onClick={onRefresh}>Refresh repository evidence</button>}
        {requiresAcknowledgement && (
          <div role="alert" className="space-y-3 rounded-md border border-[var(--warning)]/30 bg-[var(--warning)]/5 p-3 text-sm text-[var(--warning)]">
            <p>This repository has an unresolved integrity failure or inconclusive check. Cleanup can remove recovery material needed for repair. Inspect the evidence below and keep an independent repository copy before continuing.</p>
            {failures.map(([label, evidence]) => <div key={label}>
              <p>{label}: <BackupStatus value={evidence.status} /> · {checkScopeLabel(evidence.scope)} · <BackupDate value={evidence.checked_at} /></p>
              {evidence.error && <p className="mt-1 break-words">{evidence.error}</p>}
            </div>)}
            <CheckHoldWarning destination={destination} announce={false} />
            <p>Type the destination name to acknowledge this specific evidence and the cleanup risk. Cleanup does not repair the repository or clear these warnings.</p>
          </div>
        )}
        {requiresAcknowledgement && <Field label={`Type ${destination.name} to confirm`}>
          <input ref={textRef} className={`${inputClass} read-only:opacity-70`} value={confirmation} onChange={(event) => { setConfirmation(event.target.value); if (event.target.value === destination.name) setEvidenceNotice(null) }} readOnly={pending || refreshing || refreshFailed} aria-describedby={status ? statusId : undefined} />
        </Field>}
        <div className="flex justify-end gap-2">
          <button ref={cancelRef} className={buttonClass} disabled={pending} onClick={onCancel}>Cancel</button>
          <button className={primaryClass} disabled={pending || refreshing || refreshFailed || (requiresAcknowledgement && (confirmation !== destination.name || !destination.maintenance_review_token))} onClick={() => onConfirm(requiresAcknowledgement ? destination.maintenance_review_token : undefined)}>{pending ? 'Starting…' : 'Start cleanup'}</button>
        </div>
      </div>
    </Dialog>
  )
}
