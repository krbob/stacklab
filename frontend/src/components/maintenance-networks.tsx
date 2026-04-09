import { useState } from 'react'
import { getMaintenanceNetworks } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import type { MaintenanceNetworkItem } from '@/lib/api-types'
import { cn } from '@/lib/cn'

type Usage = 'all' | 'used' | 'unused'
type Origin = 'all' | 'stack_managed' | 'external'

export function MaintenanceNetworks() {
  const [usage, setUsage] = useState<Usage>('all')
  const [origin, setOrigin] = useState<Origin>('all')
  const [search, setSearch] = useState('')

  const { data, error, loading, refetch } = useApi(
    () => getMaintenanceNetworks({ usage: usage !== 'all' ? usage : undefined, origin: origin !== 'all' ? origin : undefined, q: search || undefined }),
    [usage, origin, search],
  )

  const networks = data?.items ?? []
  const unusedCount = networks.filter((n) => n.is_unused).length

  return (
    <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-medium text-[var(--text)]">Networks</h3>
          <p className="mt-1 text-xs text-[var(--muted)]">
            {networks.length} networks · {unusedCount} unused
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {(['all', 'used', 'unused'] as const).map((v) => (
            <button key={v} onClick={() => setUsage(v)} className={cn('rounded-full border px-2.5 py-1 text-xs transition', usage === v ? 'border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}>
              {v}
            </button>
          ))}
          <span className="text-zinc-700">|</span>
          {(['all', 'stack_managed', 'external'] as const).map((v) => (
            <button key={v} onClick={() => setOrigin(v)} className={cn('rounded-full border px-2.5 py-1 text-xs transition', origin === v ? 'border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}>
              {v.replace('_', ' ')}
            </button>
          ))}
          <input type="text" value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search..." className="rounded-full border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]" />
          <button onClick={refetch} className="rounded-full border border-[var(--panel-border)] px-2.5 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]">Refresh</button>
        </div>
      </div>

      {error && <div className="mt-3 rounded-md border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">{error.message}</div>}

      <div className="mt-4 space-y-1">
        {loading && networks.length === 0 && [1, 2, 3].map((i) => <div key={i} className="h-14 animate-pulse rounded-[12px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />)}
        {!loading && networks.length === 0 && <p className="py-6 text-center text-sm text-[var(--muted)]">No networks found matching filters.</p>}
        {networks.map((net) => <NetworkRow key={net.id} network={net} />)}
      </div>
    </section>
  )
}

function NetworkRow({ network }: { network: MaintenanceNetworkItem }) {
  return (
    <div className="rounded-[12px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-4 py-3 text-xs">
      <div className="flex items-center gap-2">
        <span className="font-mono text-[var(--text)]">{network.name}</span>
        {network.is_unused && <span className="text-zinc-500">unused</span>}
        {network.internal && <span className="text-amber-400">internal</span>}
        {network.source === 'external' && <span className="text-[var(--muted)]">external</span>}
      </div>
      <div className="mt-1 flex flex-wrap gap-3 font-mono text-[var(--muted)]">
        <span>driver: {network.driver}</span>
        <span>scope: {network.scope}</span>
        <span>{network.containers_using} container{network.containers_using !== 1 ? 's' : ''}</span>
        {network.stacks_using.length > 0 && <span className="text-[var(--accent)]">{network.stacks_using.map((s) => s.stack_id).join(', ')}</span>}
        <span className="text-zinc-600">{network.id.slice(0, 12)}</span>
      </div>
    </div>
  )
}
