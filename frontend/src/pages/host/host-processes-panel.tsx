import type { HostMetricSample } from '@/lib/api-types'
import { cn } from '@/lib/cn'
import { formatBytes } from '@/pages/host-page-utils'

export type ProcessSortKey = 'cpu' | 'memory'

export function HostProcessesPanel({
  processes,
  sortKey,
  onSortChange,
}: {
  processes: HostMetricSample['processes'] | null
  sortKey: ProcessSortKey
  onSortChange: (sortKey: ProcessSortKey) => void
}) {
  const sortedProcesses = sortProcesses(processes?.items ?? [], sortKey).slice(0, 12)

  return (
    <div className="min-w-0 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
      <div className="mb-3 flex min-w-0 flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="font-brand text-xs uppercase tracking-wider text-[var(--accent)]">Top processes</div>
          <div className="mt-1 text-xs text-[var(--muted)]">{processes?.total ?? 0} visible</div>
        </div>
        <div className="inline-flex rounded-md border border-[var(--panel-border)] bg-[rgba(0,0,0,0.16)] p-1">
          {(['cpu', 'memory'] as const).map((key) => (
            <button
              key={key}
              type="button"
              onClick={() => onSortChange(key)}
              aria-pressed={sortKey === key}
              className={cn(
                'rounded px-2.5 py-1 text-xs transition',
                sortKey === key ? 'bg-[var(--accent)] text-black' : 'text-[var(--muted)] hover:text-[var(--text)]',
              )}
            >
              {key === 'cpu' ? 'CPU' : 'Memory'}
            </button>
          ))}
        </div>
      </div>

      {sortedProcesses.length === 0 ? (
        <div className="text-sm text-[var(--muted)]">No process metrics available.</div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[820px] table-fixed text-left text-xs">
            <thead className="text-[var(--muted)]">
              <tr className="border-b border-[var(--panel-border)]">
                <th className="w-16 py-2 pr-3 font-medium">PID</th>
                <th className="w-28 px-3 py-2 font-medium">User</th>
                <th className="w-20 px-3 py-2 text-right font-medium">CPU</th>
                <th className="w-32 px-3 py-2 text-right font-medium">RAM</th>
                <th className="w-16 px-3 py-2 text-center font-medium">State</th>
                <th className="w-40 px-3 py-2 font-medium">Source</th>
                <th className="px-3 py-2 font-medium">Command</th>
              </tr>
            </thead>
            <tbody>
              {sortedProcesses.map((process) => (
                <tr key={process.pid} className="border-b border-[var(--panel-border)]/60 last:border-0">
                  <td className="py-2 pr-3 font-mono text-[var(--muted)]">{process.pid}</td>
                  <td className="truncate px-3 py-2 text-[var(--muted)]" title={process.user}>{process.user || '-'}</td>
                  <td className="px-3 py-2 text-right font-medium text-[var(--text)]">{formatPercent(process.cpu_percent)}</td>
                  <td className="px-3 py-2 text-right text-[var(--text)]">
                    <span>{formatBytes(process.memory_bytes)}</span>
                    <span className="ml-1 text-[var(--muted)]">({formatPercent(process.memory_percent)})</span>
                  </td>
                  <td className="px-3 py-2 text-center font-mono text-[var(--muted)]">{process.state || '-'}</td>
                  <td className="min-w-0 px-3 py-2" title={processSourceTitle(process)}>
                    <span className={processSourceClass(process)}>
                      <span className="truncate">{processSourceLabel(process)}</span>
                    </span>
                  </td>
                  <td className="min-w-0 px-3 py-2 text-[var(--text)]" title={processLabel(process)}>
                    <div className="truncate">{processLabel(process)}</div>
                    {process.display_command && process.display_command !== process.command && (
                      <div className="truncate text-[var(--muted)]">{process.command}</div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function sortProcesses(processes: NonNullable<HostMetricSample['processes']>['items'], sortKey: ProcessSortKey) {
  return [...processes].sort((left, right) => {
    if (sortKey === 'memory') {
      if (left.memory_bytes !== right.memory_bytes) return right.memory_bytes - left.memory_bytes
      if (left.cpu_percent !== right.cpu_percent) return right.cpu_percent - left.cpu_percent
    } else {
      if (left.cpu_percent !== right.cpu_percent) return right.cpu_percent - left.cpu_percent
      if (left.memory_bytes !== right.memory_bytes) return right.memory_bytes - left.memory_bytes
    }
    const leftLabel = processLabel(left)
    const rightLabel = processLabel(right)
    if (leftLabel !== rightLabel) return leftLabel.localeCompare(rightLabel)
    return left.pid - right.pid
  })
}

function processLabel(process: NonNullable<HostMetricSample['processes']>['items'][number]): string {
  return process.display_command || process.command
}

function processSourceLabel(process: NonNullable<HostMetricSample['processes']>['items'][number]): string {
  const container = process.container
  if (!container) {
    return 'Host'
  }
  if (container.stack_id) {
    const service = container.service_name || container.name || shortContainerId(container.id)
    return service ? `${container.stack_id} / ${service}` : container.stack_id
  }
  return `Docker / ${container.name || shortContainerId(container.id)}`
}

function processSourceTitle(process: NonNullable<HostMetricSample['processes']>['items'][number]): string {
  const container = process.container
  if (!container) {
    return 'Host process'
  }
  const parts = []
  if (container.stack_id) parts.push(`Stack: ${container.stack_id}`)
  if (container.service_name) parts.push(`Service: ${container.service_name}`)
  if (container.name) parts.push(`Container: ${container.name}`)
  if (container.id) parts.push(`ID: ${container.id}`)
  return parts.length > 0 ? parts.join(' · ') : 'Docker container'
}

function processSourceClass(process: NonNullable<HostMetricSample['processes']>['items'][number]): string {
  return cn(
    'inline-flex max-w-full items-center rounded border px-2 py-0.5 text-xs',
    process.container?.stack_id
      ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.10)] text-[var(--text)]'
      : process.container
        ? 'border-[rgba(255,255,255,0.14)] bg-[rgba(255,255,255,0.05)] text-[var(--muted)]'
        : 'border-transparent bg-transparent px-0 text-[var(--muted)]',
  )
}

function shortContainerId(id: string): string {
  return id.length > 12 ? id.slice(0, 12) : id
}

function formatPercent(value: number): string {
  return `${Math.max(0, value).toFixed(1)}%`
}
