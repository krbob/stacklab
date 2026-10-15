import type { BackupDestination } from '@/lib/backup-api'
import { BackupDate } from './backup-primitives'
import { checkScopeLabel, requiredCheckScope } from './repository-evidence'

export function CheckHoldWarning({ destination, announce = true }: { destination: BackupDestination; announce?: boolean }) {
  const hold = destination.check_hold
  if (!hold) return null
  return (
    <div role={announce ? 'alert' : undefined} className="space-y-1 rounded-md border border-[var(--warning)]/30 bg-[var(--warning)]/5 p-3 text-sm text-[var(--warning)]">
      <p>Repository check is inconclusive. Saved verification evidence is historical; recovery protection and automatic cleanup remain blocked.</p>
      <p>{hold.error}</p>
      <p className="text-xs">Required check: {checkScopeLabel(requiredCheckScope(destination))}{hold.checked_at && <> · <BackupDate value={hold.checked_at} /></>}. Resolve the cause, then run this check successfully.</p>
    </div>
  )
}
