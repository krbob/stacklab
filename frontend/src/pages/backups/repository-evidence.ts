import type { BackupDestination } from '@/lib/backup-api'

export const checkScopeLabel = (scope?: string) => ({ metadata: 'Metadata', subset: 'Sampled data read', all: 'Full data read' } as Record<string, string>)[scope ?? ''] ?? 'Scope not recorded'

export function requiredCheckScope(destination: BackupDestination): 'metadata' | 'all' | undefined {
  if (destination.data_check.status === 'failed' || destination.check_hold?.scope === 'all') return 'all'
  if (destination.metadata_check.status === 'failed' || destination.check_hold) return 'metadata'
}

export function repositoryLocked(error: unknown): boolean {
  if (typeof error === 'object' && error !== null && 'code' in error && error.code === 'repository_locked') return true
  const message = error instanceof Error ? error.message : typeof error === 'string' ? error : ''
  return /\(repository_locked\)/.test(message)
}

