import { useCallback, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { getMaintenanceNetworks, createMaintenanceNetwork, deleteMaintenanceNetwork } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { useDebouncedValue } from '@/hooks/use-debounced-value'
import type { MaintenanceNetworkItem } from '@/lib/api-types'
import { cn } from '@/lib/cn'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { AsyncState } from '@/components/async-state'
import { Dialog } from '@/components/dialog'

type Usage = 'all' | 'used' | 'unused'
type Origin = 'all' | 'stack_managed' | 'external'

const BUILTIN_NETWORKS = ['bridge', 'host', 'none', 'ingress']

function canDelete(net: MaintenanceNetworkItem): boolean {
  return net.is_unused && net.source === 'external' && !BUILTIN_NETWORKS.includes(net.name)
}

function deleteBlockedReason(net: MaintenanceNetworkItem): string | null {
  if (BUILTIN_NETWORKS.includes(net.name)) return 'Built-in network'
  if (!net.is_unused) return `In use by ${net.containers_using} container${net.containers_using !== 1 ? 's' : ''}`
  if (net.source === 'stack_managed') return `Managed by stack ${net.stacks_using[0]?.stack_id ?? ''}`
  return null
}

export function MaintenanceNetworks() {
  const [usage, setUsage] = useState<Usage>('all')
  const [origin, setOrigin] = useState<Origin>('all')
  const [search, setSearch] = useState('')
  const debouncedSearch = useDebouncedValue(search)
  const [showCreate, setShowCreate] = useState(false)
  const [createName, setCreateName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [pendingDelete, setPendingDelete] = useState<MaintenanceNetworkItem | null>(null)
  const [deletingName, setDeletingName] = useState<string | null>(null)
  const createNameRef = useRef<HTMLInputElement>(null)

  const { data, error, loading, refetch } = useApi(
    () => getMaintenanceNetworks({ usage: usage !== 'all' ? usage : undefined, origin: origin !== 'all' ? origin : undefined, q: debouncedSearch || undefined }),
    [usage, origin, debouncedSearch],
  )

  const networks = data?.items ?? []
  const unusedCount = networks.filter((n) => n.is_unused).length

  const handleCreate = useCallback(async () => {
    if (!createName.trim()) return
    setCreating(true)
    setCreateError(null)
    setActionError(null)
    try {
      await createMaintenanceNetwork({ name: createName.trim() })
      setCreateName('')
      setShowCreate(false)
      refetch()
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : 'Create failed')
    } finally {
      setCreating(false)
    }
  }, [createName, refetch])

  const handleDelete = useCallback(async (network: MaintenanceNetworkItem) => {
    setActionError(null)
    setDeletingName(network.name)
    try {
      await deleteMaintenanceNetwork(network.name)
      setPendingDelete(null)
      refetch()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Delete failed')
    } finally {
      setDeletingName(null)
    }
  }, [refetch])

  return (
    <section aria-busy={loading || creating || deletingName !== null} className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-medium text-[var(--text)]">Networks</h2>
          {data && <p className="mt-1 text-xs text-[var(--muted)]">{networks.length} networks · {unusedCount} unused</p>}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {(['all', 'used', 'unused'] as const).map((v) => (
            <button key={v} onClick={() => setUsage(v)} aria-pressed={usage === v} className={cn('rounded-md border px-2.5 py-1 text-xs transition', usage === v ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}>{v}</button>
          ))}
          <span className="text-[var(--muted)]">|</span>
          {(['all', 'stack_managed', 'external'] as const).map((v) => (
            <button key={v} onClick={() => setOrigin(v)} aria-pressed={origin === v} className={cn('rounded-md border px-2.5 py-1 text-xs transition', origin === v ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}>{v.replace('_', ' ')}</button>
          ))}
          <input type="text" value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search..." aria-label="Search networks" className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]" />
          <button onClick={() => setShowCreate(true)} className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-2.5 py-1 text-xs text-[var(--text)]">Create network</button>
          <button onClick={refetch} className="rounded-md border border-[var(--panel-border)] px-2.5 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]">Refresh</button>
        </div>
      </div>

      {/* Create modal */}
      {showCreate && (
        <Dialog
          title="Create network"
          onClose={() => setShowCreate(false)}
          initialFocusRef={createNameRef}
          busy={creating}
          preventClose={creating}
          panelClassName="max-w-sm"
        >
          <form
            onSubmit={(event) => {
              event.preventDefault()
              void handleCreate()
            }}
          >
            <input ref={createNameRef} type="text" value={createName} onChange={(e) => setCreateName(e.target.value)} placeholder="Network name" disabled={creating} className="mt-3 w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]" />
            {createError && <p role="alert" className="mt-2 text-xs text-[var(--danger)]">{createError}</p>}
            <div className="mt-4 flex justify-end gap-2">
              <button type="button" onClick={() => setShowCreate(false)} disabled={creating} className="rounded-md border border-[var(--panel-border)] px-3 py-1.5 text-xs text-[var(--muted)] disabled:opacity-40">Cancel</button>
              <button type="submit" disabled={creating || !createName.trim()} className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-3 py-1.5 text-xs text-[var(--text)] disabled:opacity-40">Create</button>
            </div>
          </form>
        </Dialog>
      )}

      {actionError && <div className="mt-3 rounded-md border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">{actionError}</div>}

      <div className="mt-4 space-y-2">
        <AsyncState
          loading={loading}
          error={error}
          hasData={data !== null}
          isEmpty={networks.length === 0}
          loadingLabel="Loading networks..."
          emptyMessage="No networks found matching filters."
          onRetry={refetch}
          loadingFallback={
            <>
              {[1, 2, 3].map((i) => <div key={i} className="h-14 animate-pulse rounded-[12px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />)}
            </>
          }
        >
          {networks.map((net) => (
            <div key={net.id} className="flex items-center gap-3 rounded-[12px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-4 py-3 text-xs">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-[var(--text)]">{net.name}</span>
                  {net.is_unused && <span className="text-[var(--muted)]">unused</span>}
                  {net.internal && <span className="text-[var(--warning)]">internal</span>}
                  {net.source === 'external' && <span className="text-[var(--muted)]">external</span>}
                </div>
                <div className="mt-1 flex flex-wrap gap-3 font-mono text-[var(--muted)]">
                  <span>driver: {net.driver}</span>
                  <span>scope: {net.scope}</span>
                  <span>{net.containers_using} container{net.containers_using !== 1 ? 's' : ''}</span>
                  {net.stacks_using.length > 0 && (
                    <span className="flex flex-wrap gap-1">
                      {net.stacks_using.map((s) => (
                        <Link key={s.stack_id} to={`/stacks/${s.stack_id}`} className="text-[var(--accent)] hover:underline">{s.stack_id}</Link>
                      ))}
                    </span>
                  )}
                  <span className="text-[var(--muted)]">{net.id.slice(0, 12)}</span>
                </div>
              </div>
              <span className="shrink-0" title={deleteBlockedReason(net) ?? undefined}>
                <button
                  onClick={() => {
                    setActionError(null)
                    setPendingDelete(net)
                  }}
                  disabled={!canDelete(net)}
                  aria-label={`Remove ${net.name}`}
                  className="rounded-md border border-[var(--danger)]/30 px-2 py-1 text-xs text-[var(--danger)] transition hover:bg-[var(--danger)]/10 disabled:opacity-30 disabled:hover:bg-transparent"
                >
                  Remove
                </button>
              </span>
            </div>
          ))}
        </AsyncState>
      </div>

      {pendingDelete && (
        <ConfirmDialog
          title={`Remove network "${pendingDelete.name}"?`}
          message="Review the network configuration and manual recovery path before removing it."
          review={{
            target: pendingDelete.id
              ? `${pendingDelete.name} (${pendingDelete.id.slice(0, 12)})`
              : pendingDelete.name,
            scope: [
              `Remove one unused external Docker network using the ${pendingDelete.driver} driver.`,
              `Network scope: ${pendingDelete.scope}; internal: ${pendingDelete.internal ? 'yes' : 'no'}.`,
            ],
            impact: [
              'Docker removes the network object and its network-specific configuration.',
              'Future deployments that reference this external network can fail until it is recreated.',
            ],
            snapshot: 'No automatic export of the network driver, IPAM, or options is created.',
            recovery: 'Recreate the network manually with its original settings, then redeploy or reconnect affected consumers.',
          }}
          confirmLabel="Remove network"
          confirming={deletingName === pendingDelete.name}
          onCancel={() => setPendingDelete(null)}
          onConfirm={() => handleDelete(pendingDelete)}
        />
      )}
    </section>
  )
}
