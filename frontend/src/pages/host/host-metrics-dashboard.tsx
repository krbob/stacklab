import { useState } from 'react'
import { Eye, EyeOff } from 'lucide-react'
import type { HostMetricsResponse, HostOverviewResponse } from '@/lib/api-types'
import { formatBytes } from '@/pages/host-page-utils'
import { formatLoadAverage, formatRate, formatTemperature, maskPublicIP } from '@/pages/host/host-metric-format'
import {
  DiskIORow,
  FilesystemRow,
  MetricCard,
  SwapRow,
  TemperatureRow,
} from '@/pages/host/host-metric-widgets'
import { HostProcessesPanel, type ProcessSortKey } from '@/pages/host/host-processes-panel'
import { PercentBar } from '@/pages/host/metric-primitives'
import { utilizationTone } from '@/pages/host/metric-style'

export function HostMetricsDashboard({
  metrics,
  overview,
  loading,
  error,
}: {
  metrics: HostMetricsResponse | null
  overview: HostOverviewResponse
  loading: boolean
  error: Error | null
}) {
  const [processSort, setProcessSort] = useState<ProcessSortKey>('cpu')
  const [publicIPVisible, setPublicIPVisible] = useState(false)
  const current = metrics?.current ?? null
  const history = (metrics?.history ?? []).slice(-180)

  if (loading && !current) {
    return (
      <div className="mt-5 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        {[1, 2, 3, 4].map((i) => (
          <div key={i} className="h-36 animate-pulse rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />
        ))}
      </div>
    )
  }

  if (!current) {
    return (
      <div className="mt-5 rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-4 py-3 text-sm text-[var(--muted)]">
        Waiting for host metrics...
      </div>
    )
  }

  const primaryFilesystem = current.filesystems.find((filesystem) => filesystem.primary) ?? current.filesystems[0]
  const fallbackDisk = {
    mount_point: overview.resources.disk.path,
    device: '',
    fs_type: '',
    total_bytes: overview.resources.disk.total_bytes,
    used_bytes: overview.resources.disk.used_bytes,
    available_bytes: overview.resources.disk.available_bytes,
    usage_percent: overview.resources.disk.usage_percent,
    primary: true,
  }
  const storage = primaryFilesystem ?? fallbackDisk
  const networkRate = current.network.total_rx_bytes_per_sec + current.network.total_tx_bytes_per_sec
  const topInterface = current.network.interfaces[0]
  const sampledAt = new Date(current.sampled_at).toLocaleTimeString()
  const cpuTone = utilizationTone(current.cpu.usage_percent)
  const cpuTemperature = current.temperatures.cpu_celsius
  const memoryTone = utilizationTone(current.memory.usage_percent, 'bg-[#E8C07A]', '#E8C07A')
  const storageTone = utilizationTone(storage.usage_percent, 'bg-[var(--warning)]', '#D66F3F')

  return (
    <div className="mt-5 flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-lg font-medium text-[var(--text)]">Host metrics</h2>
        <div className="text-xs text-[var(--muted)]">Sampled {sampledAt}</div>
      </div>

      {error && (
        <div className="rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">
          Failed to load host metrics: {error.message}
        </div>
      )}

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          title="CPU"
          value={`${current.cpu.usage_percent.toFixed(1)}%`}
          detail={`${current.cpu.core_count} cores · load avg 1/5/15m ${formatLoadAverage(current.cpu.load_average)}${cpuTemperature !== null ? ` · ${formatTemperature(cpuTemperature)}` : ''}`}
          color={cpuTone.line}
          values={history.map((sample) => sample.cpu.usage_percent)}
          sparklineMax={100}
          sparklineLabel="CPU usage history"
          valueClassName={cpuTone.text}
        >
          <PercentBar value={current.cpu.usage_percent} color={cpuTone.bar} label="CPU usage" />
          <TemperatureRow temperatures={current.temperatures} />
        </MetricCard>

        <MetricCard
          title="Memory"
          value={`${current.memory.usage_percent.toFixed(1)}%`}
          detail={`${formatBytes(current.memory.used_bytes)} / ${formatBytes(current.memory.total_bytes)}`}
          color={memoryTone.line}
          values={history.map((sample) => sample.memory.usage_percent)}
          sparklineMax={100}
          sparklineLabel="Memory usage history"
          valueClassName={memoryTone.text}
        >
          <PercentBar value={current.memory.usage_percent} color={memoryTone.bar} label="Memory usage" />
          <SwapRow swap={current.swap} />
        </MetricCard>

        <MetricCard
          title="Network"
          value={`${formatRate(current.network.total_rx_bytes_per_sec)} ↓`}
          detail={`${formatRate(current.network.total_tx_bytes_per_sec)} ↑${topInterface ? ` · ${topInterface.name}` : ''}`}
          color="#5EC2B7"
          values={history.map((sample) => sample.network.total_rx_bytes_per_sec + sample.network.total_tx_bytes_per_sec)}
          sparklineLabel="Network throughput history"
        >
          <div className="space-y-1 text-xs text-[var(--muted)]">
            <div className="flex min-w-0 justify-between gap-2">
              <span>Combined</span>
              <span className="text-[var(--text)]">{formatRate(networkRate)}</span>
            </div>
            {current.network.public_ip && (
              <div className="flex min-w-0 justify-between gap-2">
                <span className="shrink-0">Public IP</span>
                <span className="flex min-w-0 items-center justify-end gap-2 text-right">
                  <span className="min-w-0 break-all text-[var(--text)]">{publicIPVisible ? current.network.public_ip : maskPublicIP(current.network.public_ip)}</span>
                  <button
                    type="button"
                    className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded border border-[var(--panel-border)] text-[var(--muted)] transition hover:text-[var(--text)]"
                    onClick={() => setPublicIPVisible((visible) => !visible)}
                    aria-pressed={publicIPVisible}
                    aria-label={publicIPVisible ? 'Hide public IP' : 'Show public IP'}
                    title={publicIPVisible ? 'Hide public IP' : 'Show public IP'}
                  >
                    {publicIPVisible ? <EyeOff className="h-3.5 w-3.5" aria-hidden="true" /> : <Eye className="h-3.5 w-3.5" aria-hidden="true" />}
                  </button>
                </span>
              </div>
            )}
          </div>
        </MetricCard>

        <MetricCard
          title="Storage"
          value={`${storage.usage_percent.toFixed(1)}%`}
          detail={`${formatBytes(storage.used_bytes)} / ${formatBytes(storage.total_bytes)}`}
          color={storageTone.line}
          values={history.map((sample) => {
            const filesystem = sample.filesystems.find((item) => item.primary) ?? sample.filesystems[0]
            return filesystem?.usage_percent ?? storage.usage_percent
          })}
          sparklineMax={100}
          sparklineLabel="Storage usage history"
          valueClassName={storageTone.text}
        >
          <PercentBar value={storage.usage_percent} color={storageTone.bar} label="Storage usage" />
          <DiskIORow diskIO={current.disk_io} />
        </MetricCard>
      </div>

      <HostProcessesPanel processes={current.processes ?? null} sortKey={processSort} onSortChange={setProcessSort} />

      <div className="grid gap-3 xl:grid-cols-[minmax(0,1.2fr)_minmax(280px,0.8fr)]">
        <div className="min-w-0 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
          <div className="mb-3 font-brand text-xs uppercase tracking-wider text-[var(--accent)]">Filesystems</div>
          <div className="space-y-3">
            {current.filesystems.length === 0 && (
              <div className="text-sm text-[var(--muted)]">No filesystem metrics available.</div>
            )}
            {current.filesystems.map((filesystem) => (
              <FilesystemRow key={`${filesystem.mount_point}:${filesystem.device}`} filesystem={filesystem} />
            ))}
          </div>
        </div>

        <div className="min-w-0 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
          <div className="mb-3 font-brand text-xs uppercase tracking-wider text-[var(--accent)]">Interfaces</div>
          <div className="space-y-2">
            {current.network.interfaces.length === 0 && (
              <div className="text-sm text-[var(--muted)]">No network metrics available.</div>
            )}
            {current.network.interfaces.slice(0, 6).map((networkInterface) => (
              <div key={networkInterface.name} className="min-w-0 rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.12)] px-3 py-2">
                <div className="flex min-w-0 items-center justify-between gap-3">
                  <div className="min-w-0 truncate text-sm font-medium text-[var(--text)]">{networkInterface.name}</div>
                  <div className="shrink-0 text-xs text-[var(--muted)]">
                    {formatRate(networkInterface.rx_bytes_per_sec)} ↓ · {formatRate(networkInterface.tx_bytes_per_sec)} ↑
                  </div>
                </div>
                <div className="mt-1 truncate text-xs text-[var(--muted)]">
                  RX {formatBytes(networkInterface.rx_bytes)} · TX {formatBytes(networkInterface.tx_bytes)}
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
