import { useOutletContext } from 'react-router-dom'
import type { StackDetailResponse } from '@/lib/api-types'
import { useStatsStream } from '@/hooks/use-stats-stream'
import { useWs } from '@/hooks/use-ws'

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`
}

function formatRate(bytesPerSec: number): string {
  return `${formatBytes(bytesPerSec)}/s`
}

function PercentBar({ value, max, color }: { value: number; max: number; color: string }) {
  const pct = max > 0 ? Math.min((value / max) * 100, 100) : 0
  return (
    <div className="h-2 w-full rounded-full bg-[rgba(255,255,255,0.06)]">
      <div
        className={`h-2 rounded-full ${color}`}
        style={{ width: `${pct}%` }}
      />
    </div>
  )
}

function Sparkline({ values, height = 32, color = '#4fd1c5' }: { values: number[]; height?: number; color?: string }) {
  if (values.length < 2) return null

  const max = Math.max(...values, 0.01)
  const w = 120
  const points = values
    .map((v, i) => {
      const x = (i / (values.length - 1)) * w
      const y = height - (v / max) * height
      return `${x},${y}`
    })
    .join(' ')

  return (
    <svg viewBox={`0 0 ${w} ${height}`} className="h-8 w-30" preserveAspectRatio="none">
      <polyline
        points={points}
        fill="none"
        stroke={color}
        strokeWidth="1.5"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}

export function StackStatsPage() {
  const { stack } = useOutletContext<{ stack: StackDetailResponse['stack'] }>()
  const { connected } = useWs()
  const noRunning = !stack.containers.some((c) => c.status === 'running')

  const { current, history } = useStatsStream({
    stackId: stack.id,
    enabled: !noRunning,
  })

  if (noRunning) {
    return (
      <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-10 text-center">
        <p className="text-[var(--text)]">No stats available</p>
        <p className="mt-1 text-sm text-[var(--muted)]">
          Stats require at least one running container in this stack.
        </p>
      </div>
    )
  }

  if (!current) {
    return (
      <div className="flex flex-col gap-3">
        {!connected && (
          <div className="text-xs text-amber-400">Stream disconnected. Reconnecting...</div>
        )}
        <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-8 text-center">
          <div className="text-sm text-[var(--muted)]">Waiting for stats...</div>
        </div>
      </div>
    )
  }

  const totals = current.stack_totals

  return (
    <div className="flex flex-col gap-4">
      {!connected && (
        <div className="text-xs text-amber-400">Stream disconnected. Reconnecting...</div>
      )}

      {/* Stack totals */}
      <div className="flex flex-wrap gap-6 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-4 text-sm">
        <div>
          <span className="text-[var(--muted)]">CPU </span>
          <span className="text-[var(--text)]">{totals.cpu_percent.toFixed(1)}%</span>
        </div>
        <div>
          <span className="text-[var(--muted)]">RAM </span>
          <span className="text-[var(--text)]">
            {formatBytes(totals.memory_bytes)}
            {totals.memory_limit_bytes > 0 && (
              <span className="text-[var(--muted)]"> / {formatBytes(totals.memory_limit_bytes)}</span>
            )}
          </span>
        </div>
        <div>
          <span className="text-[var(--muted)]">Net </span>
          <span className="text-emerald-400">↓{formatRate(totals.network_rx_bytes_per_sec)}</span>
          <span className="mx-1 text-[var(--muted)]">·</span>
          <span className="text-amber-400">↑{formatRate(totals.network_tx_bytes_per_sec)}</span>
        </div>
      </div>

      {/* Per-container cards */}
      <div className="grid gap-3">
        {current.containers.map((c) => {
          const cpuHistory = history.map(
            (h) => h.containers.find((hc) => hc.container_id === c.container_id)?.cpu_percent ?? 0,
          )
          const memHistory = history.map(
            (h) => h.containers.find((hc) => hc.container_id === c.container_id)?.memory_bytes ?? 0,
          )

          return (
            <div
              key={c.container_id}
              className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4"
            >
              <div className="mb-3 text-base font-medium text-[var(--text)]">{c.service_name}</div>

              <div className="grid gap-3 md:grid-cols-2">
                {/* CPU */}
                <div className="flex flex-col gap-1">
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-[var(--muted)]">CPU</span>
                    <span className="text-[var(--text)]">{c.cpu_percent.toFixed(1)}%</span>
                  </div>
                  <PercentBar value={c.cpu_percent} max={100} color="bg-cyan-400" />
                  <Sparkline values={cpuHistory} color="#22d3ee" />
                </div>

                {/* Memory */}
                <div className="flex flex-col gap-1">
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-[var(--muted)]">RAM</span>
                    <span className="text-[var(--text)]">
                      {formatBytes(c.memory_bytes)}
                      {c.memory_limit_bytes > 0 && (
                        <span className="text-[var(--muted)]"> / {formatBytes(c.memory_limit_bytes)}</span>
                      )}
                    </span>
                  </div>
                  <PercentBar
                    value={c.memory_bytes}
                    max={c.memory_limit_bytes || c.memory_bytes * 2}
                    color="bg-violet-400"
                  />
                  <Sparkline values={memHistory} color="#a78bfa" />
                </div>
              </div>

              {/* Network */}
              <div className="mt-2 text-sm">
                <span className="text-[var(--muted)]">Net </span>
                <span className="text-emerald-400">↓{formatRate(c.network_rx_bytes_per_sec)}</span>
                <span className="mx-1 text-[var(--muted)]">·</span>
                <span className="text-amber-400">↑{formatRate(c.network_tx_bytes_per_sec)}</span>
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
