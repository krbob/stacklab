import { useRef, useState } from 'react'
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
  const [evidenceChanged, setEvidenceChanged] = useState(false)
  if (reviewToken !== destination.maintenance_review_token) {
    setReviewToken(destination.maintenance_review_token)
    setConfirmation('')
    setEvidenceChanged(confirmation !== '')
  }
  const textRef = useRef<HTMLInputElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const requiresAcknowledgement = !!requiredCheckScope(destination)
  const failures = ([['Repository metadata evidence', destination.metadata_check], ['Stored data evidence', destination.data_check]] as const).filter(([, evidence]) => evidence.status === 'failed')
  return (
    <Dialog title="Run retention cleanup" onClose={onCancel} preventClose={pending} busy={pending} initialFocusRef={requiresAcknowledgement ? textRef : cancelRef}>
      <div className="mt-4 space-y-4">
        <p className="text-sm text-[var(--muted)]">Reclaim storage no longer referenced by retained snapshots. Cleanup locks this repository and may take time or incur provider transfer charges.</p>
        <p className="break-all font-mono text-xs text-[var(--text)]">{destination.repository}</p>
        <BackupError error={error} />
        {refreshing && <p role="status" className="text-sm text-[var(--muted)]">Refreshing repository evidence. Cleanup is disabled until this finishes.</p>}
        {evidenceChanged && !error && <p role="status" className="text-sm text-[var(--warning)]">Repository evidence changed. Typed confirmation was reset; review the current evidence before confirming again.</p>}
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
          <input ref={textRef} className={inputClass} value={confirmation} onChange={(event) => setConfirmation(event.target.value)} readOnly={pending || refreshing || refreshFailed} aria-busy={refreshing} />
        </Field>}
        <div className="flex justify-end gap-2">
          <button ref={cancelRef} className={buttonClass} disabled={pending} onClick={onCancel}>Cancel</button>
          <button className={primaryClass} disabled={pending || refreshing || refreshFailed || (requiresAcknowledgement && (confirmation !== destination.name || !destination.maintenance_review_token))} onClick={() => onConfirm(requiresAcknowledgement ? destination.maintenance_review_token : undefined)}>{pending ? 'Starting…' : 'Start cleanup'}</button>
        </div>
      </div>
    </Dialog>
  )
}
