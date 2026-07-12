import type { HostMetricsResponse, HostOverviewResponse } from '@/lib/api-types'
import { formatBytes, formatUptime } from '@/pages/host-page-utils'
import { PercentBar } from '@/pages/host/metric-primitives'
import { utilizationTone } from '@/pages/host/metric-style'

export function HostOverviewCards({
  overview,
  metrics,
  nowMs,
  fetchedAtMs,
}: {
  overview: HostOverviewResponse
  metrics: HostMetricsResponse | null
  nowMs: number
  fetchedAtMs: number | null
}) {
  const { host, stacklab, docker, resources } = overview
  const currentResources = metrics?.current ?? null
  const cpu = currentResources?.cpu ?? resources.cpu
  const memory = currentResources?.memory ?? resources.memory
  const swap = currentResources?.swap ?? null
  const primaryFilesystem = currentResources?.filesystems.find((filesystem) => filesystem.primary) ?? currentResources?.filesystems[0]
  const disk = primaryFilesystem ?? {
    used_bytes: resources.disk.used_bytes,
    total_bytes: resources.disk.total_bytes,
    usage_percent: resources.disk.usage_percent,
  }
  const liveUptimeSeconds = fetchedAtMs == null
    ? host.uptime_seconds
    : host.uptime_seconds + Math.max(0, Math.floor((nowMs - fetchedAtMs) / 1000))
  const cpuTone = utilizationTone(cpu.usage_percent)
  const memoryTone = utilizationTone(memory.usage_percent, 'bg-[#E8C07A]', '#E8C07A')
  const diskTone = utilizationTone(disk.usage_percent, 'bg-[var(--warning)]', '#D66F3F')

  return (
    <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
      {/* Stacklab */}
      <div className="min-w-0 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
        <div className="font-brand text-xs uppercase tracking-wider text-[var(--accent)]">Stacklab</div>
        <div className="mt-2 break-all text-lg font-medium text-[var(--text)]">{stacklab.version}</div>
        <div className="mt-1 space-y-0.5 text-xs text-[var(--muted)]">
          {stacklab.commit && <div>Commit: {stacklab.commit.slice(0, 8)}</div>}
          <div>Started: {new Date(stacklab.started_at).toLocaleString()}</div>
        </div>
      </div>

      {/* Host */}
      <div className="min-w-0 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
        <div className="font-brand text-xs uppercase tracking-wider text-[var(--accent)]">System</div>
        <div className="mt-2 break-all text-lg font-medium text-[var(--text)]">{host.hostname}</div>
        <div className="mt-1 space-y-0.5 text-xs text-[var(--muted)]">
          <div>{host.os_name}</div>
          <div>Kernel: {host.kernel_version}</div>
          <div>Uptime: {formatUptime(liveUptimeSeconds)}</div>
          <div>{host.architecture}</div>
        </div>
      </div>

      {/* Docker */}
      <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
        <div className="font-brand text-xs uppercase tracking-wider text-[var(--accent)]">Docker</div>
        <div className="mt-2 text-lg font-medium text-[var(--text)]">Engine {docker.engine_version}</div>
        <div className="mt-1 text-xs text-[var(--muted)]">Compose {docker.compose_version}</div>
      </div>

      {/* Resources */}
      <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
        <div className="font-brand text-xs uppercase tracking-wider text-[var(--accent)]">Resources</div>
        <div className="mt-3 space-y-2">
          <div>
            <div className="flex justify-between text-xs">
              <span className="text-[var(--muted)]">CPU ({cpu.core_count} cores)</span>
              <span className={cpuTone.text}>{cpu.usage_percent.toFixed(1)}%</span>
            </div>
            <PercentBar value={cpu.usage_percent} color={cpuTone.bar} label="CPU usage" />
          </div>
          <div>
            <div className="flex justify-between text-xs">
              <span className="text-[var(--muted)]">Memory</span>
              <span className={memoryTone.text}>{formatBytes(memory.used_bytes)} / {formatBytes(memory.total_bytes)}</span>
            </div>
            <PercentBar value={memory.usage_percent} color={memoryTone.bar} label="Memory usage" />
          </div>
          {swap && (
            <div>
              <div className="flex justify-between text-xs">
                <span className="text-[var(--muted)]">Swap</span>
                <span className={utilizationTone(swap.usage_percent).text}>{swap.total_bytes > 0 ? `${formatBytes(swap.used_bytes)} / ${formatBytes(swap.total_bytes)}` : 'disabled'}</span>
              </div>
              <PercentBar value={swap.usage_percent} color={utilizationTone(swap.usage_percent, 'bg-[#8FB8DE]', '#8FB8DE').bar} label="Swap usage" />
            </div>
          )}
          <div>
            <div className="flex justify-between text-xs">
              <span className="text-[var(--muted)]">Disk</span>
              <span className={diskTone.text}>{formatBytes(disk.used_bytes)} / {formatBytes(disk.total_bytes)}</span>
            </div>
            <PercentBar value={disk.usage_percent} color={diskTone.bar} label="Disk usage" />
          </div>
        </div>
      </div>
    </div>
  )
}
