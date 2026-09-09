import { ArrowDown, ArrowUp, ExternalLink } from 'lucide-react'
import { Link } from 'react-router-dom'
import type { StackListItem } from '@/lib/api-types'
import { cn } from '@/lib/cn'
import { DASHBOARD_HISTORY_WINDOW_MS, type DashboardHistory, type DashboardHistoryPoint } from '@/lib/dashboard-history'

export type StackSortKey = 'name' | 'cpu' | 'memory'
export type StackView = 'cards' | 'list'

const stateLabels: Record<string, { label: string; color: string }> = {
  running: { label: 'Running', color: 'text-[var(--ok)] bg-[var(--ok)]/10' },
  partial: { label: 'Partial', color: 'text-[var(--warning)] bg-[var(--warning)]/10' },
  error: { label: 'Error', color: 'text-[var(--danger)] bg-[var(--danger)]/10' },
  orphaned: { label: 'Orphaned', color: 'text-[var(--danger)] bg-[var(--danger)]/10' },
  stopped: { label: 'Stopped', color: 'text-[var(--muted)] bg-white/5' },
  defined: { label: 'Defined', color: 'text-[var(--muted)] bg-white/5' },
}

function formatMemory(bytes: number): string {
  if (bytes >= 2 ** 30) return (bytes / 2 ** 30).toFixed(1) + ' GiB'
  if (bytes >= 2 ** 20) return Math.round(bytes / 2 ** 20) + ' MiB'
  if (bytes >= 1024) return (bytes / 1024).toFixed(1) + ' KiB'
  return Math.max(0, bytes) + ' B'
}

function StackGlyph({ stack }: { stack: StackListItem }) {
  return (
    <span aria-hidden="true" className="flex size-9 shrink-0 items-center justify-center rounded-lg border border-[rgba(245,165,36,0.15)] bg-[rgba(245,165,36,0.08)] font-brand text-xs font-semibold text-[var(--accent)]">
      {(stack.metadata?.icon ?? stack.name).slice(0, 2).toUpperCase()}
    </span>
  )
}

function StackState({ stack }: { stack: StackListItem }) {
  const state = stack.activity_state === 'locked'
    ? { label: 'Working…', color: 'text-[var(--run)] bg-[var(--run)]/10' }
    : stateLabels[stack.display_state] ?? stateLabels.defined
  return <span className={cn('inline-block shrink-0 whitespace-nowrap rounded px-2 py-1 text-xs font-medium', state.color)}>{state.label}</span>
}

function ConfigBadges({ stack }: { stack: StackListItem }) {
  return (
    <>
      {stack.config_state === 'drifted' && <span className="text-xs text-[var(--warning)]">Config drift</span>}
      {stack.config_state === 'invalid' && <span className="text-xs text-[var(--danger)]">Invalid config</span>}
    </>
  )
}

function StackHealth({ stack }: { stack: StackListItem }) {
  const health = stack.health_summary
  const labels = [
    health.healthy_container_count > 0 && health.healthy_container_count + ' healthy',
    health.unhealthy_container_count > 0 && health.unhealthy_container_count + ' unhealthy',
    health.unknown_health_container_count > 0 && health.unknown_health_container_count + ' unchecked',
  ].filter(Boolean)
  return (
    <span className={cn('text-xs', health.unhealthy_container_count > 0 ? 'text-[var(--warning)]' : 'text-[var(--muted)]')}>
      {labels.join(' · ') || (stack.service_count.running === 0 ? 'No running containers' : 'Health unavailable')}
    </span>
  )
}

function UpdateBadge({ stack }: { stack: StackListItem }) {
  if (stack.updates?.state !== 'available') return null
  const count = stack.updates.services_with_updates
  return (
    <span title={count + (count === 1 ? ' service has an image update' : ' services have image updates')}
      className="inline-block shrink-0 rounded border border-[rgba(245,165,36,0.2)] bg-[rgba(245,165,36,0.08)] px-2 py-0.5 text-xs text-[var(--accent)]">
      {count > 1 ? count + ' updates' : 'Update available'}
    </span>
  )
}

function LastAction({ stack, nowMs }: { stack: StackListItem; nowMs: number }) {
  const action = stack.last_action
  if (!action) return <span className="text-xs text-[var(--muted)]">No recent activity</span>
  const elapsed = Math.max(0, nowMs - Date.parse(action.finished_at))
  const relative = !Number.isFinite(elapsed) ? null
    : elapsed < 60_000 ? 'just now'
    : elapsed < 3_600_000 ? Math.floor(elapsed / 60_000) + 'm ago'
    : elapsed < 86_400_000 ? Math.floor(elapsed / 3_600_000) + 'h ago'
    : Math.floor(elapsed / 86_400_000) + 'd ago'
  return (
    <div className={cn('min-w-0 text-xs', action.result === 'succeeded' ? 'text-[var(--muted)]' : 'text-[var(--danger)]')}>
      <span className="block break-words">{action.action.replaceAll('_', ' ')} · {action.result.replaceAll('_', ' ')}</span>
      {relative && <time dateTime={action.finished_at} title={new Date(action.finished_at).toLocaleString()} className="mt-0.5 block">{relative}</time>}
    </div>
  )
}

function StackLinks({ stack }: { stack: StackListItem }) {
  return (
    <div className="relative z-10 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
      {(stack.metadata?.links ?? []).map((link) => (
        <a key={link.url} href={link.url} target="_blank" rel="noreferrer"
          className="flex min-w-0 items-center gap-1 rounded-sm text-xs text-[var(--accent)] hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--accent)]">
          <span className="break-words [overflow-wrap:anywhere]">{link.label}</span>
          <ExternalLink aria-hidden="true" className="size-3 shrink-0" />
        </a>
      ))}
    </div>
  )
}

function HistoryChart({ points, metric, label, nowMs }: {
  points: DashboardHistoryPoint[]
  metric: 'cpu' | 'memory'
  label: string
  nowMs: number
}) {
  const maximum = Math.max(1, ...points.map((point) => point[metric]))
  const segments: DashboardHistoryPoint[][] = []
  for (const point of points) {
    const last = segments.at(-1)
    // Leave gaps across missed polls or a hidden tab instead of inventing a trend.
    if (!last || point.at - last[last.length - 1].at > 2_500) segments.push([point])
    else last.push(point)
  }
  const x = (point: DashboardHistoryPoint) => Math.max(0, Math.min(120, 120 * (1 - (nowMs - point.at) / DASHBOARD_HISTORY_WINDOW_MS)))
  const y = (point: DashboardHistoryPoint) => 30 - Math.max(0, point[metric]) / maximum * 26
  return (
    <svg role="img" aria-label={label + (points.length < 2 ? ' · collecting history' : ' · last 60 seconds')}
      viewBox="0 0 120 34" preserveAspectRatio="none"
      className={cn('h-9 w-full overflow-visible', metric === 'cpu' ? 'text-[var(--accent)]' : 'text-[var(--muted)]')}>
      <path d="M0 31H120" stroke="currentColor" strokeOpacity="0.2" strokeDasharray="2 4" />
      {segments.map((segment) => segment.length === 1
        ? <circle key={segment[0].at} cx={x(segment[0])} cy={y(segment[0])} r="1.5" fill="currentColor" />
        : <polyline key={segment[0].at} points={segment.map((point) => x(point) + ',' + y(point)).join(' ')}
            fill="none" stroke="currentColor" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />)}
    </svg>
  )
}

interface PresentationProps {
  stacks: StackListItem[]
  history: DashboardHistory
  nowMs: number
}

export function StackCards({ stacks, history, nowMs }: PresentationProps) {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,19rem),1fr))] items-stretch gap-4">
      {stacks.map((stack) => {
        const points = stack.stats ? history[stack.id]?.points ?? [] : []
        return (
          <article key={stack.id} data-testid={'stack-card-' + stack.id}
            className="@container relative flex min-w-0 flex-col rounded-xl border border-[var(--panel-border)] bg-linear-to-br from-[rgba(255,255,255,0.04)] to-[rgba(255,255,255,0.01)] transition-colors hover:border-[rgba(245,165,36,0.35)] focus-within:border-[rgba(245,165,36,0.5)]">
            <Link to={'/stacks/' + stack.id} aria-label={stack.name} className="absolute inset-0 rounded-xl focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--accent)]" />
            <div className="flex items-start gap-3 px-5 pt-5">
              <div className="hidden @min-[19rem]:block"><StackGlyph stack={stack} /></div>
              <div className="min-w-0 flex-1">
                <h2 className="font-brand text-base font-semibold leading-6 [overflow-wrap:anywhere]">{stack.name}</h2>
                <p className="mt-1 text-xs text-[var(--muted)]">{stack.service_count.running}/{stack.service_count.defined} services running</p>
              </div>
              <StackState stack={stack} />
            </div>
            <div className="mt-4 min-h-12 px-5">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <StackHealth stack={stack} />
                <UpdateBadge stack={stack} />
              </div>
              <div className="mt-1"><ConfigBadges stack={stack} /></div>
            </div>
            <dl className="grid grid-cols-2 gap-6 px-5 pb-4 pt-5">
              {(['cpu', 'memory'] as const).map((metric) => (
                <div key={metric} className="min-w-0">
                  <dt className="text-xs font-medium tracking-wider text-[var(--muted)]">{metric === 'cpu' ? 'CPU' : 'RAM'}</dt>
                  <dd aria-label={stack.name + (metric === 'cpu' ? ' CPU usage' : ' RAM usage')} className="mb-2 mt-2 whitespace-nowrap font-mono text-xl font-medium tabular-nums">
                    {stack.stats ? metric === 'cpu' ? stack.stats.cpu_percent.toFixed(1) + '%' : formatMemory(stack.stats.memory_bytes) : '—'}
                  </dd>
                  <HistoryChart points={points} metric={metric} label={stack.name + (metric === 'cpu' ? ' CPU history' : ' RAM history')} nowMs={nowMs} />
                </div>
              ))}
            </dl>
            <p className="px-5 pb-4 text-xs text-[var(--muted)]">
              {!stack.stats ? 'No resource sample' : points.length < 2 ? 'Collecting history…' : 'Last 60s'}
            </p>
            <footer className="mt-auto flex flex-wrap items-center justify-between gap-3 border-t border-[var(--panel-border)] px-5 py-3">
              <LastAction stack={stack} nowMs={nowMs} />
              <StackLinks stack={stack} />
            </footer>
          </article>
        )
      })}
    </div>
  )
}

export function StackTable({ stacks, history, nowMs, sortKey, onSortChange }: PresentationProps & {
  sortKey: StackSortKey
  onSortChange: (key: StackSortKey) => void
}) {
  function sortHeader(label: string, key: StackSortKey, numeric = false) {
    return (
      <th scope="col" aria-sort={sortKey === key ? key === 'name' ? 'ascending' : 'descending' : 'none'} className="px-4 py-3">
        <button type="button" onClick={() => onSortChange(key)} className={cn('flex w-full items-center gap-1 rounded-sm hover:text-[var(--text)] focus-visible:outline-2 focus-visible:outline-[var(--accent)]', numeric && 'justify-end')}>
          {label}
          {sortKey === key && (key === 'name' ? <ArrowUp aria-hidden="true" className="size-3" /> : <ArrowDown aria-hidden="true" className="size-3" />)}
        </button>
      </th>
    )
  }
  return (
    <>
      <p className="mb-2 text-xs text-[var(--muted)] lg:hidden">Scroll horizontally to see all columns.</p>
      <div role="region" aria-label="Stack list" tabIndex={0} className="overflow-x-auto rounded-lg border border-[var(--panel-border)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--accent)]">
        <table className="w-full min-w-[64rem] text-left text-xs">
          <caption className="sr-only">Stacks, health, resource usage, and recent activity</caption>
          <thead className="bg-black/20 font-medium text-[var(--muted)]">
            <tr>
              {sortHeader('Stack', 'name')}
              <th scope="col" className="px-4 py-3">State</th>
              <th scope="col" className="px-4 py-3">Services / health</th>
              {sortHeader('CPU', 'cpu', true)}
              {sortHeader('RAM', 'memory', true)}
              <th scope="col" className="w-28 px-4 py-3">CPU · 60s</th>
              <th scope="col" className="px-4 py-3">Last action</th>
              <th scope="col" className="px-4 py-3">Updates / links</th>
            </tr>
          </thead>
          <tbody>
            {stacks.map((stack) => (
              <tr key={stack.id} data-testid={'stack-row-' + stack.id} className="border-t border-[var(--panel-border)] hover:bg-white/3 focus-within:bg-white/3">
                <th scope="row" className="max-w-72 px-4 py-4 font-normal">
                  <div className="flex items-center gap-3">
                    <StackGlyph stack={stack} />
                    <div className="min-w-0">
                      <h2 className="font-brand text-sm font-semibold [overflow-wrap:anywhere]">
                        <Link to={'/stacks/' + stack.id} className="rounded-sm hover:text-[var(--accent)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--accent)]">{stack.name}</Link>
                      </h2>
                      <ConfigBadges stack={stack} />
                    </div>
                  </div>
                </th>
                <td className="px-4 py-4"><StackState stack={stack} /></td>
                <td className="px-4 py-4">
                  <div className="mb-1 whitespace-nowrap">{stack.service_count.running}/{stack.service_count.defined} services running</div>
                  <StackHealth stack={stack} />
                </td>
                <td aria-label={stack.name + ' CPU usage'} className="whitespace-nowrap px-4 py-4 text-right font-mono text-sm tabular-nums">{stack.stats ? stack.stats.cpu_percent.toFixed(1) + '%' : '—'}</td>
                <td aria-label={stack.name + ' RAM usage'} className="whitespace-nowrap px-4 py-4 text-right font-mono text-sm tabular-nums">{stack.stats ? formatMemory(stack.stats.memory_bytes) : '—'}</td>
                <td className="min-w-28 px-4 py-4"><HistoryChart points={stack.stats ? history[stack.id]?.points ?? [] : []} metric="cpu" label={stack.name + ' CPU history'} nowMs={nowMs} /></td>
                <td className="px-4 py-4"><LastAction stack={stack} nowMs={nowMs} /></td>
                <td className="px-4 py-4"><div className="flex flex-col items-start gap-2"><UpdateBadge stack={stack} /><StackLinks stack={stack} /></div></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  )
}
