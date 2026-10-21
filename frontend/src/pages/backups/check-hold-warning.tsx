import type { BackupDestination } from '@/lib/backup-api'
import { BackupDate } from './backup-primitives'
import { checkScopeLabel, requiredCheckScope } from './repository-evidence'

export function CheckHoldWarning({ destination, announce = false, compact = false }: { destination: BackupDestination; announce?: boolean; compact?: boolean }) {
  const hold = destination.check_hold
  if (!hold) return null
  return (
    <div role={announce ? 'alert' : undefined} className="space-y-1 rounded-md border border-[var(--warning)]/30 bg-[var(--warning)]/5 p-3 text-sm text-[var(--warning)]">
      <p>Repository check is inconclusive. Saved verification evidence is historical; protection is unverified and automatic cleanup is blocked. Recovery remains available.</p>
      {!compact && <p>{hold.error}</p>}
      {!compact && hold.checked_at && <p className="text-xs">Hold recorded: <BackupDate value={hold.checked_at} />.</p>}
      <p className="text-xs">Required check: {checkScopeLabel(requiredCheckScope(destination))}. Resolve the cause, then run this check successfully.</p>
    </div>
  )
}
