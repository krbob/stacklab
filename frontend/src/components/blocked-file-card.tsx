import { useCallback, useEffect, useState } from 'react'
import { ShieldAlert } from 'lucide-react'
import type { FilePermissions, WorkspaceRepairCapability } from '@/lib/api-types'
import { cn } from '@/lib/cn'

interface RepairResult {
  repaired: boolean
  changed_items: number
  target_permissions_before: FilePermissions
  target_permissions_after: FilePermissions
  warnings?: string[]
}

interface BlockedFileCardProps {
  stateKey?: string
  blockedReason: string | null
  permissions: FilePermissions | null
  repairCapability?: WorkspaceRepairCapability | null
  onRepair?: (recursive: boolean) => Promise<RepairResult>
  allowRecursive?: boolean
}

const reasonMessages: Record<string, string> = {
  not_readable: 'This file is currently not readable by the Stacklab service user.',
  not_writable: 'This file is currently not writable by the Stacklab service user.',
}

export function BlockedFileCard({ stateKey, blockedReason, permissions, repairCapability, onRepair, allowRecursive = false }: BlockedFileCardProps) {
  const [recursive, setRecursive] = useState(false)
  const [repairing, setRepairing] = useState(false)
  const [repairResult, setRepairResult] = useState<RepairResult | null>(null)
  const [repairError, setRepairError] = useState<string | null>(null)

  useEffect(() => {
    setRecursive(false)
    setRepairing(false)
    setRepairResult(null)
    setRepairError(null)
  }, [stateKey])

  useEffect(() => {
    if (!repairResult) return
    const timeout = window.setTimeout(() => {
      setRepairResult(null)
    }, 10_000)
    return () => window.clearTimeout(timeout)
  }, [repairResult])

  const handleRepair = useCallback(async () => {
    if (!onRepair) return
    setRepairing(true)
    setRepairError(null)
    setRepairResult(null)
    try {
      const result = await onRepair(recursive)
      setRepairResult(result)
    } catch (err) {
      setRepairError(err instanceof Error ? err.message : 'Repair failed')
    } finally {
      setRepairing(false)
    }
  }, [onRepair, recursive])

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

        {/* Repair controls */}
        {repairCapability?.supported && onRepair && !repairResult && (
          <div className="mt-4 space-y-2">
            <button
              onClick={handleRepair}
              disabled={repairing}
              className="rounded-md border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-4 py-2 text-xs text-[var(--text)] transition hover:bg-[rgba(34,197,94,0.2)] disabled:opacity-40"
            >
              {repairing ? 'Repairing...' : 'Repair access'}
            </button>
            {allowRecursive && repairCapability.recursive && (
              <label className="flex items-center justify-center gap-2 text-xs text-[var(--muted)]">
                <input type="checkbox" checked={recursive} onChange={(e) => setRecursive(e.target.checked)} disabled={repairing} className="rounded" />
                Repair recursively
              </label>
            )}
            <p className="text-[10px] text-[var(--muted)]">
              Uses the configured workspace helper. Limited to managed roots.
            </p>
          </div>
        )}

        {repairCapability && !repairCapability.supported && (
          <p className="mt-4 text-[10px] text-[var(--muted)]">
            Automatic repair is not available. {repairCapability.reason}
          </p>
        )}

        {repairError && (
          <div className="mt-3 text-xs text-red-400">{repairError}</div>
        )}

        {/* Repair result */}
        {repairResult && (
          <div className={cn('mt-4 rounded-md border px-4 py-3 text-left text-xs', repairResult.repaired ? 'border-emerald-400/20 bg-emerald-400/5' : 'border-amber-400/20 bg-amber-400/5')}>
            <div className={repairResult.repaired ? 'text-emerald-400' : 'text-amber-400'}>
              {repairResult.repaired ? `Repaired (${repairResult.changed_items} item${repairResult.changed_items !== 1 ? 's' : ''} changed)` : 'No changes needed'}
            </div>

            <div className="mt-2 grid grid-cols-[auto_1fr_1fr] gap-x-3 gap-y-0.5 font-mono">
              <span className="text-[var(--muted)]" />
              <span className="text-zinc-500">Before</span>
              <span className="text-zinc-500">After</span>

              <span className="text-[var(--muted)]">Owner</span>
              <span className="text-[var(--muted)]">{repairResult.target_permissions_before.owner_name ?? '—'}</span>
              <span className="text-[var(--text)]">{repairResult.target_permissions_after.owner_name ?? '—'}</span>

              <span className="text-[var(--muted)]">Mode</span>
              <span className="text-[var(--muted)]">{repairResult.target_permissions_before.mode}</span>
              <span className="text-[var(--text)]">{repairResult.target_permissions_after.mode}</span>

              <span className="text-[var(--muted)]">Readable</span>
              <span className={repairResult.target_permissions_before.readable ? 'text-emerald-400' : 'text-red-400'}>
                {repairResult.target_permissions_before.readable ? 'Yes' : 'No'}
              </span>
              <span className={repairResult.target_permissions_after.readable ? 'text-emerald-400' : 'text-red-400'}>
                {repairResult.target_permissions_after.readable ? 'Yes' : 'No'}
              </span>
            </div>

            {repairResult.warnings && repairResult.warnings.length > 0 && (
              <div className="mt-2 text-amber-400">
                {repairResult.warnings.map((w, i) => <div key={i}>{w}</div>)}
              </div>
            )}

            <div className="mt-3 flex justify-end">
              <button
                type="button"
                onClick={() => setRepairResult(null)}
                className="rounded-full border border-[var(--panel-border)] px-3 py-1 text-[10px] text-[var(--muted)] transition hover:text-[var(--text)]"
              >
                Done
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
