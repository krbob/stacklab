import type { BackupSource } from '@/lib/backup-api'

const names: Record<string, string> = {
  stack_definition: 'Stack definition and supporting files',
  managed_config: 'Managed configuration',
  stacklab: 'Stacklab recovery',
  service_mount: 'Service data',
}

export function SourceLabel({ source }: { source: BackupSource }) {
  return <span className="min-w-0 break-words">
    <span className="block">{source.service ? `${source.service} → ${source.mount}` : names[source.kind] ?? source.kind}</span>
    <span className="mt-1 block break-all text-xs text-[var(--muted)]">{[source.stack_id, source.path].filter(Boolean).join(' · ') || 'Private service state, configuration and SQLite'}</span>
  </span>
}
