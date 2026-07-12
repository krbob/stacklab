import { useCallback, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { getMaintenanceVolumes, createMaintenanceVolume, deleteMaintenanceVolume } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { useDebouncedValue } from '@/hooks/use-debounced-value'
import type { MaintenanceVolumeItem } from '@/lib/api-types'
import { cn } from '@/lib/cn'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { AsyncState } from '@/components/async-state'
import { Dialog } from '@/components/dialog'

type Usage = 'all' | 'used' | 'unused'
type Origin = 'all' | 'stack_managed' | 'external'

function canDelete(vol: MaintenanceVolumeItem): boolean {
  return vol.is_unused && vol.source === 'external'
}

function deleteBlockedReason(vol: MaintenanceVolumeItem): string | null {
  if (!vol.is_unused) return `In use by ${vol.containers_using} container${vol.containers_using !== 1 ? 's' : ''}`
  if (vol.source === 'stack_managed') return `Managed by stack ${vol.stacks_using[0]?.stack_id ?? ''}`
  return null
}

export function MaintenanceVolumes() {
  const [usage, setUsage] = useState<Usage>('all')
  const [origin, setOrigin] = useState<Origin>('all')
  const [search, setSearch] = useState('')
  const debouncedSearch = useDebouncedValue(search)
  const [showCreate, setShowCreate] = useState(false)
  const [createName, setCreateName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [pendingDelete, setPendingDelete] = useState<MaintenanceVolumeItem | null>(null)
  const [deletingName, setDeletingName] = useState<string | null>(null)
  const createNameRef = useRef<HTMLInputElement>(null)

  const { data, error, loading, refetch } = useApi(
    () => getMaintenanceVolumes({ usage: usage !== 'all' ? usage : undefined, origin: origin !== 'all' ? origin : undefined, q: debouncedSearch || undefined }),
    [usage, origin, debouncedSearch],
  )

  const volumes = data?.items ?? []
  const unusedCount = volumes.filter((v) => v.is_unused).length

  const handleCreate = useCallback(async () => {
    if (!createName.trim()) return
    setCreating(true)
    setCreateError(null)
    setActionError(null)
    try {
      await createMaintenanceVolume({ name: createName.trim() })
      setCreateName('')
      setShowCreate(false)
      refetch()
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : 'Create failed')
    } finally {
      setCreating(false)
    }
  }, [createName, refetch])

  const handleDelete = useCallback(async (volume: MaintenanceVolumeItem) => {
    setActionError(null)
    setDeletingName(volume.name)
    try {
      await deleteMaintenanceVolume(volume.name)
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
          <h2 className="text-lg font-medium text-[var(--text)]">Volumes</h2>
          {data && <p className="mt-1 text-xs text-[var(--muted)]">{volumes.length} volumes · {unusedCount} unused</p>}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {(['all', 'used', 'unused'] as const).map((v) => (
            <button key={v} onClick={() => setUsage(v)} aria-pressed={usage === v} className={cn('rounded-md border px-2.5 py-1 text-xs transition', usage === v ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}>{v}</button>
          ))}
          <span className="text-[var(--muted)]">|</span>
          {(['all', 'stack_managed', 'external'] as const).map((v) => (
            <button key={v} onClick={() => setOrigin(v)} aria-pressed={origin === v} className={cn('rounded-md border px-2.5 py-1 text-xs transition', origin === v ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}>{v.replace('_', ' ')}</button>
          ))}
          <input type="text" value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search..." aria-label="Search volumes" className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]" />
          <button onClick={() => setShowCreate(true)} className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-2.5 py-1 text-xs text-[var(--text)]">Create volume</button>
          <button onClick={refetch} className="rounded-md border border-[var(--panel-border)] px-2.5 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]">Refresh</button>
        </div>
      </div>

      {/* Create modal */}
      {showCreate && (
        <Dialog
          title="Create volume"
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
            <input ref={createNameRef} type="text" value={createName} onChange={(e) => setCreateName(e.target.value)} placeholder="Volume name" disabled={creating} className="mt-3 w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]" />
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
          isEmpty={volumes.length === 0}
          loadingLabel="Loading volumes..."
          emptyMessage="No volumes found matching filters."
          onRetry={refetch}
          loadingFallback={
            <>
              {[1, 2, 3].map((i) => <div key={i} className="h-14 animate-pulse rounded-[12px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />)}
            </>
          }
        >
          {volumes.map((vol) => (
            <div key={vol.name} className="flex items-center gap-3 rounded-[12px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-4 py-3 text-xs">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-[var(--text)]">{vol.name}</span>
                  {vol.is_unused && <span className="text-[var(--muted)]">unused</span>}
                  {vol.source === 'external' && <span className="text-[var(--muted)]">external</span>}
                </div>
                <div className="mt-1 flex flex-wrap gap-3 font-mono text-[var(--muted)]">
                  <span className="truncate">{vol.mountpoint || '—'}</span>
                  <span>driver: {vol.driver}</span>
                  <span>scope: {vol.scope}</span>
                  <span>{vol.containers_using} container{vol.containers_using !== 1 ? 's' : ''}</span>
                  {vol.stacks_using.length > 0 && (
                    <span className="flex flex-wrap gap-1">
                      {vol.stacks_using.map((s) => (
                        <Link key={s.stack_id} to={`/stacks/${s.stack_id}`} className="text-[var(--accent)] hover:underline">{s.stack_id}</Link>
                      ))}
                    </span>
                  )}
                </div>
              </div>
              <span className="shrink-0" title={deleteBlockedReason(vol) ?? undefined}>
                <button
                  onClick={() => {
                    setActionError(null)
                    setPendingDelete(vol)
                  }}
                  disabled={!canDelete(vol)}
                  aria-label={`Remove ${vol.name}`}
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
          title={`Remove volume "${pendingDelete.name}"?`}
          message="This deletes the Docker volume. Any data inside it is removed permanently."
          items={[
            `volume: ${pendingDelete.name}`,
            pendingDelete.mountpoint ? `mountpoint: ${pendingDelete.mountpoint}` : 'mountpoint: unavailable',
          ]}
          requireText={pendingDelete.name}
          confirmLabel="Remove volume"
          confirming={deletingName === pendingDelete.name}
          onCancel={() => setPendingDelete(null)}
          onConfirm={() => handleDelete(pendingDelete)}
        />
      )}
    </section>
  )
}
