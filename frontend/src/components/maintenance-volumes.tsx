import { useCallback, useState } from 'react'
import { Link } from 'react-router-dom'
import { getMaintenanceVolumes, createMaintenanceVolume, deleteMaintenanceVolume } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import type { MaintenanceVolumeItem } from '@/lib/api-types'
import { cn } from '@/lib/cn'

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
  const [showCreate, setShowCreate] = useState(false)
  const [createName, setCreateName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const { data, error, loading, refetch } = useApi(
    () => getMaintenanceVolumes({ usage: usage !== 'all' ? usage : undefined, origin: origin !== 'all' ? origin : undefined, q: search || undefined }),
    [usage, origin, search],
  )

  const volumes = data?.items ?? []
  const unusedCount = volumes.filter((v) => v.is_unused).length

  const handleCreate = useCallback(async () => {
    if (!createName.trim()) return
    setCreating(true)
    setCreateError(null)
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

  const handleDelete = useCallback(async (name: string) => {
    try {
      await deleteMaintenanceVolume(name)
      refetch()
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Delete failed')
    }
  }, [refetch])

  return (
    <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-medium text-[var(--text)]">Volumes</h3>
          <p className="mt-1 text-xs text-[var(--muted)]">{volumes.length} volumes · {unusedCount} unused</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {(['all', 'used', 'unused'] as const).map((v) => (
            <button key={v} onClick={() => setUsage(v)} className={cn('rounded-full border px-2.5 py-1 text-xs transition', usage === v ? 'border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}>{v}</button>
          ))}
          <span className="text-zinc-700">|</span>
          {(['all', 'stack_managed', 'external'] as const).map((v) => (
            <button key={v} onClick={() => setOrigin(v)} className={cn('rounded-full border px-2.5 py-1 text-xs transition', origin === v ? 'border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}>{v.replace('_', ' ')}</button>
          ))}
          <input type="text" value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search..." className="rounded-full border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]" />
          <button onClick={() => setShowCreate(true)} className="rounded-full border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-2.5 py-1 text-xs text-[var(--text)]">Create volume</button>
          <button onClick={refetch} className="rounded-full border border-[var(--panel-border)] px-2.5 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]">Refresh</button>
        </div>
      </div>

      {/* Create modal */}
      {showCreate && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={() => setShowCreate(false)}>
          <div className="w-full max-w-sm rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5" onClick={(e) => e.stopPropagation()}>
            <h4 className="text-sm font-medium text-[var(--text)]">Create volume</h4>
            <input type="text" value={createName} onChange={(e) => setCreateName(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') handleCreate() }} placeholder="Volume name" autoFocus disabled={creating} className="mt-3 w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]" />
            {createError && <p className="mt-2 text-xs text-red-400">{createError}</p>}
            <div className="mt-4 flex justify-end gap-2">
              <button onClick={() => setShowCreate(false)} className="rounded-md border border-[var(--panel-border)] px-3 py-1.5 text-xs text-[var(--muted)]">Cancel</button>
              <button onClick={handleCreate} disabled={creating || !createName.trim()} className="rounded-md border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-3 py-1.5 text-xs text-[var(--text)] disabled:opacity-40">Create</button>
            </div>
          </div>
        </div>
      )}

      {error && <div className="mt-3 rounded-md border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">{error.message}</div>}

      <div className="mt-4 space-y-1">
        {loading && volumes.length === 0 && [1, 2, 3].map((i) => <div key={i} className="h-14 animate-pulse rounded-[12px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />)}
        {!loading && volumes.length === 0 && <p className="py-6 text-center text-sm text-[var(--muted)]">No volumes found matching filters.</p>}
        {volumes.map((vol) => (
          <div key={vol.name} className="flex items-center gap-3 rounded-[12px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-4 py-3 text-xs">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="font-mono text-[var(--text)]">{vol.name}</span>
                {vol.is_unused && <span className="text-zinc-500">unused</span>}
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
            <button
              onClick={() => handleDelete(vol.name)}
              disabled={!canDelete(vol)}
              title={deleteBlockedReason(vol) ?? 'Remove volume'}
              className="shrink-0 rounded-full border border-red-400/30 px-2 py-1 text-xs text-red-400 transition hover:bg-red-400/10 disabled:opacity-30 disabled:hover:bg-transparent"
            >
              Remove
            </button>
          </div>
        ))}
      </div>
    </section>
  )
}
