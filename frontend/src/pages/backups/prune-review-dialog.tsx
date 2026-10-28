import { useId, useLayoutEffect, useRef, useState } from 'react'
import { Dialog } from '@/components/dialog'
import type { BackupDestination } from '@/lib/backup-api'
import { BackupDate, BackupError, BackupStatus, buttonClass, Field, inputClass, primaryClass } from './backup-primitives'
import { checkScopeLabel, requiredCheckScope } from './repository-evidence'
import { CheckHoldWarning } from './check-hold-warning'

// Bind the typed confirmation to the evidence fingerprint without remounting
// the dialog: changed evidence clears the field and explains the reset.
export function PruneReviewDialog({ destination, pending, refreshing, refreshFailed, error, onCancel, onConfirm, onRefresh }: {
  destination: BackupDestination
  pending: boolean
  refreshing: boolean
  refreshFailed: boolean
  error: string | null
  onCancel: () => void
  onConfirm: (reviewToken?: string) => void
  onRefresh: () => void
}) {
  const [confirmation, setConfirmation] = useState('')
  const [reviewToken, setReviewToken] = useState(destination.maintenance_review_token)
  const requiresAcknowledgement = !!requiredCheckScope(destination)
  const [previousRequirement, setPreviousRequirement] = useState(requiresAcknowledgement)
  const [evidenceNotice, setEvidenceNotice] = useState<string | null>(null)
  const statusId = useId()
  if (reviewToken !== destination.maintenance_review_token || previousRequirement !== requiresAcknowledgement) {
    setReviewToken(destination.maintenance_review_token)
    setPreviousRequirement(requiresAcknowledgement)
    setConfirmation('')
    if (previousRequirement !== requiresAcknowledgement) {
      setEvidenceNotice(requiresAcknowledgement
        ? 'Cleanup now requires acknowledgement of new repository evidence.'
        : 'Repository evidence is now clear; acknowledgement is no longer required.')
    } else if (confirmation !== '') {
      setEvidenceNotice('Repository evidence changed. Typed confirmation was reset; review the current evidence before confirming again.')
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
  const status = refreshing
    ? 'Refreshing repository evidence. Cleanup is disabled until this finishes.'
    : !error ? evidenceNotice : null
  const failures = ([['Repository metadata evidence', destination.metadata_check], ['Stored data evidence', destination.data_check]] as const).filter(([, evidence]) => evidence.status === 'failed')
  return (
    <Dialog title="Run retention cleanup" onClose={onCancel} preventClose={pending} busy={pending || refreshing} initialFocusRef={initialFocusRef}>
      <div className="mt-4 space-y-4">
        <p className="text-sm text-[var(--muted)]">Reclaim storage no longer referenced by retained snapshots. Cleanup locks this repository and may take time or incur provider transfer charges.</p>
        <p className="break-all font-mono text-xs text-[var(--text)]">{destination.repository}</p>
        <BackupError error={error} />
        <div className="min-h-[4.5em] text-sm">
          <p id={statusId} role={status ? 'status' : undefined} className={refreshing ? 'text-[var(--muted)]' : 'text-[var(--warning)]'}>{status}</p>
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
          <input ref={textRef} className={`${inputClass} read-only:opacity-70`} value={confirmation} onChange={(event) => { setConfirmation(event.target.value); setEvidenceNotice(null) }} readOnly={pending || refreshing || refreshFailed} aria-describedby={status ? statusId : undefined} />
        </Field>}
        <div className="flex justify-end gap-2">
          <button ref={cancelRef} className={buttonClass} disabled={pending} onClick={onCancel}>Cancel</button>
          <button className={primaryClass} disabled={pending || refreshing || refreshFailed || (requiresAcknowledgement && (confirmation !== destination.name || !destination.maintenance_review_token))} onClick={() => onConfirm(requiresAcknowledgement ? destination.maintenance_review_token : undefined)}>{pending ? 'Starting…' : 'Start cleanup'}</button>
        </div>
      </div>
    </Dialog>
  )
}
