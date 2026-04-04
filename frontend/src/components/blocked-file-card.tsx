import { ShieldAlert } from 'lucide-react'
import type { FilePermissions } from '@/lib/api-types'

interface BlockedFileCardProps {
  blockedReason: string | null
  permissions: FilePermissions | null
}

const reasonMessages: Record<string, string> = {
  not_readable: 'This file is currently not readable by the Stacklab service user.',
  not_writable: 'This file is currently not writable by the Stacklab service user.',
}

export function BlockedFileCard({ blockedReason, permissions }: BlockedFileCardProps) {
  return (
    <div className="flex h-full items-center justify-center rounded border border-amber-400/20 bg-amber-400/5">
      <div className="max-w-md p-8 text-center">
        <ShieldAlert className="mx-auto size-10 text-amber-400" />
        <h4 className="mt-3 text-base font-medium text-[var(--text)]">File access blocked</h4>
        <p className="mt-2 text-sm text-[var(--muted)]">
          {reasonMessages[blockedReason ?? ''] ?? 'Stacklab cannot access this file with the current service user.'}
        </p>

        {permissions && (
          <div className="mx-auto mt-4 grid w-fit grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-left font-mono text-xs">
            <span className="text-[var(--muted)]">Owner</span>
            <span className="text-[var(--text)]">{permissions.owner_name ?? permissions.owner_uid ?? '—'}</span>
            <span className="text-[var(--muted)]">Group</span>
            <span className="text-[var(--text)]">{permissions.group_name ?? permissions.group_gid ?? '—'}</span>
            <span className="text-[var(--muted)]">Mode</span>
            <span className="text-[var(--text)]">{permissions.mode}</span>
            <span className="text-[var(--muted)]">Readable</span>
            <span className={permissions.readable ? 'text-emerald-400' : 'text-red-400'}>
              {permissions.readable ? 'Yes' : 'No'}
            </span>
            <span className="text-[var(--muted)]">Writable</span>
            <span className={permissions.writable ? 'text-emerald-400' : 'text-red-400'}>
              {permissions.writable ? 'Yes' : 'No'}
            </span>
          </div>
        )}

        <p className="mt-4 text-xs text-[var(--muted)]">
          The container may have recreated this file with different ownership or permissions.
        </p>
      </div>
    </div>
  )
}
