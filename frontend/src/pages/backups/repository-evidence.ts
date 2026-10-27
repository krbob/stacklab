import type { BackupDestination } from '@/lib/backup-api'

export function repositoryToolsAvailable(destination: BackupDestination, tools?: { restic_available?: boolean; rclone_available?: boolean } | null): boolean {
  return !!tools?.restic_available && (destination.kind !== 'rclone' || !!tools.rclone_available)
}

export const checkScopeLabel = (scope?: string) => ({ metadata: 'Metadata', subset: 'Sampled data read', all: 'Full data read' } as Record<string, string>)[scope ?? ''] ?? 'Scope not recorded'

export function requiredCheckScope(destination: BackupDestination): 'metadata' | 'all' | undefined {
  if (destination.data_check.status === 'failed' || destination.check_hold?.scope === 'all') return 'all'
  if (destination.metadata_check.status === 'failed' || destination.check_hold) return 'metadata'
}

export function preferredCheckScope(destination: BackupDestination): 'metadata' | 'subset' | 'all' {
  const required = requiredCheckScope(destination)
  if (required === 'all' || destination.check_read_data === 'all') return 'all'
  if (destination.check_read_data === 'subset') return 'subset'
  return 'metadata'
}

export function needsLockReview(destination: BackupDestination): boolean {
  const unlockedAt = destination.last_operation === 'backup_unlock' && (destination.last_operation_status === 'succeeded' || /\(unlock_refused\)/.test(destination.last_operation_error ?? ''))
    ? Date.parse(destination.last_operation_at ?? '') : NaN
  return [
    [destination.last_check_attempt_error, destination.last_check_attempt_at],
    [destination.inventory_error, destination.last_inventory_attempt_at],
    [destination.last_operation_error, destination.last_operation_at],
    [destination.error, undefined],
  // The latest completed unlock also supersedes undated imported attempts.
  // Their errors stay visible as history; only a newer dated error offers unlock.
  ].some(([error, at]) => repositoryLocked(error) && (!Number.isFinite(unlockedAt) || (!!at && Date.parse(at) > unlockedAt)))
}

export function repositoryLocked(error: unknown): boolean {
  if (typeof error === 'object' && error !== null && 'code' in error && error.code === 'repository_locked') return true
  const message = error instanceof Error ? error.message : typeof error === 'string' ? error : ''
  return /\(repository_locked\)/.test(message)
}
