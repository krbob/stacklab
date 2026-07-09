import { useCallback, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Eye, EyeOff } from 'lucide-react'
import { getHostMetrics, getHostOverview, getStacklabLogs } from '@/lib/api-client'
import type { HostMetricSample, HostMetricsResponse, HostOverviewResponse, StacklabLogEntry } from '@/lib/api-types'
import { cn } from '@/lib/cn'
import { PageHeader } from '@/components/page-header'
import { formatBytes, formatUptime } from '@/pages/host-page-utils'

const OVERVIEW_POLL_INTERVAL_MS = 5_000
const METRICS_POLL_INTERVAL_MS = 1_000
const MAX_STACKLAB_LOG_ENTRIES = 1_000
type ProcessSortKey = 'cpu' | 'memory'

function PercentBar({ value, color }: { value: number; color: string }) {
  return (
    <div className="h-2 w-full rounded-full bg-[rgba(255,255,255,0.06)]">
      <div className={`h-2 rounded-full ${color}`} style={{ width: `${Math.min(value, 100)}%` }} />
    </div>
  )
}

function utilizationTone(value: number, normalBar = 'bg-[var(--accent)]', normalLine = 'var(--accent)') {
  if (value >= 90) {
    return {
      bar: 'bg-[var(--danger)]',
      line: 'var(--danger)',
      text: 'text-[var(--danger)]',
    }
  }
  if (value >= 80) {
    return {
      bar: 'bg-[var(--warning)]',
      line: 'var(--warning)',
      text: 'text-[var(--warning)]',
    }
  }
  return {
    bar: normalBar,
    line: normalLine,
    text: 'text-[var(--text)]',
  }
}

export function HostPage() {
  const [overview, setOverview] = useState<HostOverviewResponse | null>(null)
  const [overviewError, setOverviewError] = useState<Error | null>(null)
  const [overviewLoading, setOverviewLoading] = useState(true)
  const [overviewUpdatedAt, setOverviewUpdatedAt] = useState<number | null>(null)
  const [metrics, setMetrics] = useState<HostMetricsResponse | null>(null)
  const [metricsError, setMetricsError] = useState<Error | null>(null)
  const [metricsLoading, setMetricsLoading] = useState(true)
  const [nowMs, setNowMs] = useState(() => Date.now())
  const [pageVisible, setPageVisible] = useState(() => !document.hidden)
  const initialOverviewLoadRef = useRef(true)
  const initialMetricsLoadRef = useRef(true)
  const metricsRef = useRef<HostMetricsResponse | null>(null)
  const overviewInFlightRef = useRef(false)
  const metricsInFlightRef = useRef(false)

  const loadOverview = useCallback(async () => {
    if (overviewInFlightRef.current) return
    overviewInFlightRef.current = true

    if (initialOverviewLoadRef.current) {
      setOverviewLoading(true)
    }

    try {
      const nextOverview = await getHostOverview()
      setOverview(nextOverview)
      setOverviewError(null)
      setOverviewUpdatedAt(Date.now())
    } catch (error) {
      setOverviewError(error instanceof Error ? error : new Error('Failed to load host overview'))
    } finally {
      overviewInFlightRef.current = false
      initialOverviewLoadRef.current = false
      setOverviewLoading(false)
    }
  }, [])

  const loadMetrics = useCallback(async () => {
    if (metricsInFlightRef.current) return
    metricsInFlightRef.current = true

    if (initialMetricsLoadRef.current) {
      setMetricsLoading(true)
    }

    try {
      const since = latestMetricSampleTimestamp(metricsRef.current)
      const nextMetrics = await getHostMetrics(since ? { since } : undefined)
      setMetrics((previous) => {
        const merged = previous && since ? mergeHostMetrics(previous, nextMetrics) : nextMetrics
        metricsRef.current = merged
        return merged
      })
      setMetricsError(null)
    } catch (error) {
      setMetricsError(error instanceof Error ? error : new Error('Failed to load host metrics'))
    } finally {
      metricsInFlightRef.current = false
      initialMetricsLoadRef.current = false
      setMetricsLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadOverview()
    void loadMetrics()
  }, [loadMetrics, loadOverview])

  // Auto-refresh overview metadata.
  useEffect(() => {
    if (!pageVisible) return
    const interval = setInterval(() => {
      void loadOverview()
    }, OVERVIEW_POLL_INTERVAL_MS)
    return () => clearInterval(interval)
  }, [loadOverview, pageVisible])

  // Keep host metrics in dash-like active mode while this page is open.
  useEffect(() => {
    if (!pageVisible) return
    const interval = setInterval(() => {
      void loadMetrics()
    }, METRICS_POLL_INTERVAL_MS)
    return () => clearInterval(interval)
  }, [loadMetrics, pageVisible])

  useEffect(() => {
    function handleWindowFocus() {
      void loadOverview()
      void loadMetrics()
    }

    function handleVisibilityChange() {
      const visible = !document.hidden
      setPageVisible(visible)
      if (visible) {
        void loadOverview()
        void loadMetrics()
      }
    }

    window.addEventListener('focus', handleWindowFocus)
    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => {
      window.removeEventListener('focus', handleWindowFocus)
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  }, [loadMetrics, loadOverview])

  useEffect(() => {
    if (!overview || !pageVisible) return
    const interval = setInterval(() => setNowMs(Date.now()), 1_000)
    return () => clearInterval(interval)
  }, [overview, pageVisible])

  return (
    <div className="flex flex-col gap-4">
      {/* Overview cards */}
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <PageHeader kicker="System" title="Host" />

        {overviewLoading && !overview && (
          <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            {[1, 2, 3, 4].map((i) => (
              <div key={i} className="h-32 animate-pulse rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />
            ))}
          </div>
        )}

        {overviewError && (
          <div className="mt-4 rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">
            Failed to load host overview: {overviewError.message}
          </div>
        )}

        {overview && (
          <>
            <OverviewCards overview={overview} metrics={metrics} nowMs={nowMs} fetchedAtMs={overviewUpdatedAt} />
            <HostMetricsDashboard
              metrics={metrics}
              overview={overview}
              loading={metricsLoading}
              error={metricsError}
            />
          </>
        )}
      </section>

      {/* Stacklab logs */}
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <StacklabLogs />
      </section>
    </div>
  )
}

function latestMetricSampleTimestamp(metrics: HostMetricsResponse | null): string | null {
  if (!metrics) return null
  const lastHistorySample = metrics.history[metrics.history.length - 1]
  if (!lastHistorySample) return metrics.current?.sampled_at ?? null
  if (!metrics.current) return lastHistorySample.sampled_at
  return Date.parse(metrics.current.sampled_at) > Date.parse(lastHistorySample.sampled_at)
    ? metrics.current.sampled_at
    : lastHistorySample.sampled_at
}

function mergeHostMetrics(previous: HostMetricsResponse, next: HostMetricsResponse): HostMetricsResponse {
  const byTimestamp = new Map<string, HostMetricSample>()
  for (const sample of previous.history) {
    byTimestamp.set(sample.sampled_at, sample)
  }
  for (const sample of next.history) {
    byTimestamp.set(sample.sampled_at, sample)
  }

  const currentTime = Date.parse(next.current?.sampled_at ?? previous.current?.sampled_at ?? '')
  const cutoff = Number.isFinite(currentTime)
    ? currentTime - next.history_window_seconds * 1000
    : Number.NEGATIVE_INFINITY
  const history = Array.from(byTimestamp.values())
    .sort((left, right) => Date.parse(left.sampled_at) - Date.parse(right.sampled_at))
    .filter((sample) => Date.parse(sample.sampled_at) >= cutoff)

  return {
    ...next,
    current: next.current ?? previous.current,
    history,
  }
}

function OverviewCards({
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
            <PercentBar value={cpu.usage_percent} color={cpuTone.bar} />
          </div>
          <div>
            <div className="flex justify-between text-xs">
              <span className="text-[var(--muted)]">Memory</span>
              <span className={memoryTone.text}>{formatBytes(memory.used_bytes)} / {formatBytes(memory.total_bytes)}</span>
            </div>
            <PercentBar value={memory.usage_percent} color={memoryTone.bar} />
          </div>
          {swap && (
            <div>
              <div className="flex justify-between text-xs">
                <span className="text-[var(--muted)]">Swap</span>
                <span className={utilizationTone(swap.usage_percent).text}>{swap.total_bytes > 0 ? `${formatBytes(swap.used_bytes)} / ${formatBytes(swap.total_bytes)}` : 'disabled'}</span>
              </div>
              <PercentBar value={swap.usage_percent} color={utilizationTone(swap.usage_percent, 'bg-[#8FB8DE]', '#8FB8DE').bar} />
            </div>
          )}
          <div>
            <div className="flex justify-between text-xs">
              <span className="text-[var(--muted)]">Disk</span>
              <span className={diskTone.text}>{formatBytes(disk.used_bytes)} / {formatBytes(disk.total_bytes)}</span>
            </div>
            <PercentBar value={disk.usage_percent} color={diskTone.bar} />
          </div>
        </div>
      </div>
    </div>
  )
}

function HostMetricsDashboard({
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
        <h3 className="text-lg font-medium text-[var(--text)]">Host metrics</h3>
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
          <PercentBar value={current.cpu.usage_percent} color={cpuTone.bar} />
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
          <PercentBar value={current.memory.usage_percent} color={memoryTone.bar} />
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
          <PercentBar value={storage.usage_percent} color={storageTone.bar} />
          <DiskIORow diskIO={current.disk_io} />
        </MetricCard>
      </div>

      <TopProcessesPanel processes={current.processes ?? null} sortKey={processSort} onSortChange={setProcessSort} />

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

function TopProcessesPanel({
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
    'inline-flex max-w-full items-center rounded border px-2 py-0.5 text-[11px]',
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

function MetricCard({
  title,
  value,
  detail,
  color,
  values,
  sparklineMax,
  sparklineLabel,
  valueClassName,
  children,
}: {
  title: string
  value: string
  detail: string
  color: string
  values: number[]
  sparklineMax?: number
  sparklineLabel?: string
  valueClassName?: string
  children: ReactNode
}) {
  return (
    <div className="min-w-0 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
      <div className="font-brand text-xs uppercase tracking-wider text-[var(--accent)]">{title}</div>
      <div className={cn('mt-2 truncate text-2xl font-medium', valueClassName ?? 'text-[var(--text)]')}>{value}</div>
      <div className="mt-1 truncate text-xs text-[var(--muted)]">{detail}</div>
      <div className="mt-3">
        <Sparkline values={values} color={color} max={sparklineMax} label={sparklineLabel ?? `${title} history`} />
      </div>
      <div className="mt-3">{children}</div>
    </div>
  )
}

function Sparkline({ values, color, max, label }: { values: number[]; color: string; max?: number; label: string }) {
  const points = sparklinePoints(values, max)
  return (
    <svg viewBox="0 0 120 36" role="img" aria-label={label} className="h-9 w-full overflow-visible">
      <polyline
        points={points}
        fill="none"
        stroke={color}
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}

function sparklinePoints(values: number[], maxOverride?: number): string {
  if (values.length === 0) {
    return '0,34 120,34'
  }
  const max = maxOverride ?? Math.max(...values, 1)
  if (values.length === 1) {
    const y = sparklineY(values[0], max)
    return `0,${y} 120,${y}`
  }

  return values.map((value, index) => {
    const x = (index / (values.length - 1)) * 120
    const y = sparklineY(value, max)
    return `${roundSVGCoord(x)},${y}`
  }).join(' ')
}

function sparklineY(value: number, max: number): number {
  return roundSVGCoord(34 - (Math.max(0, value) / max) * 30)
}

function roundSVGCoord(value: number): number {
  return Math.round(value * 10) / 10
}

function FilesystemRow({ filesystem }: { filesystem: HostMetricSample['filesystems'][number] }) {
  const label = filesystem.device || filesystem.fs_type || 'filesystem'
  const tone = utilizationTone(filesystem.usage_percent, filesystem.primary ? 'bg-[var(--accent)]' : 'bg-[var(--warning)]')
  return (
    <div className="min-w-0">
      <div className="mb-1 flex min-w-0 items-start justify-between gap-3 text-xs">
        <div className="min-w-0">
          <div className="break-all font-medium text-[var(--text)]">
            {filesystem.mount_point}
            {filesystem.primary && <span className="ml-2 text-[var(--accent)]">primary</span>}
          </div>
          <div className="truncate text-[var(--muted)]">{label}</div>
        </div>
        <div className="shrink-0 text-right text-[var(--muted)]">
          <div className={tone.text}>{filesystem.usage_percent.toFixed(1)}%</div>
          <div>{formatBytes(filesystem.used_bytes)} / {formatBytes(filesystem.total_bytes)}</div>
        </div>
      </div>
      <PercentBar value={filesystem.usage_percent} color={tone.bar} />
    </div>
  )
}

function SwapRow({ swap }: { swap: HostMetricSample['swap'] }) {
  const tone = utilizationTone(swap.usage_percent, 'bg-[#8FB8DE]', '#8FB8DE')
  if (swap.total_bytes === 0) {
    return (
      <div className="mt-2 flex items-center justify-between gap-2 text-xs">
        <span className="text-[var(--muted)]">Swap</span>
        <span className="text-[var(--muted)]">disabled</span>
      </div>
    )
  }

  return (
    <div className="mt-2">
      <div className="flex items-center justify-between gap-2 text-xs">
        <span className="text-[var(--muted)]">Swap</span>
        <span className={tone.text}>{formatBytes(swap.used_bytes)} / {formatBytes(swap.total_bytes)}</span>
      </div>
      <PercentBar value={swap.usage_percent} color={tone.bar} />
    </div>
  )
}

function TemperatureRow({ temperatures }: { temperatures: HostMetricSample['temperatures'] }) {
  const cpuTemperature = temperatures.cpu_celsius
  const cpuSensor = temperatures.cpu_sensor ?? null
  const topSensor = cpuSensor ?? temperatures.sensors[0]
  const visibleTemperature = cpuTemperature ?? topSensor?.temperature_celsius ?? null
  const label = cpuTemperature !== null ? 'CPU temp' : 'Sensor temp'
  const sensorLabel = cpuSensor ? cpuTemperatureSensorDisplay(cpuSensor) : topSensor ? temperatureSensorLabel(topSensor) : null

  if (visibleTemperature === null) {
    return (
      <div className="mt-2 flex items-center justify-between gap-2 text-xs">
        <span className="text-[var(--muted)]">CPU temp</span>
        <span className="text-[var(--muted)]">unavailable</span>
      </div>
    )
  }

  return (
    <div className="mt-2 space-y-1 text-xs">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[var(--muted)]">{label}</span>
        <span className={temperatureTextClass(visibleTemperature)}>{formatTemperature(visibleTemperature)}</span>
      </div>
      {sensorLabel && topSensor && (
        <div className="truncate text-[var(--muted)]" title={temperatureSensorLabel(topSensor)}>
          {sensorLabel}
        </div>
      )}
    </div>
  )
}

function cpuTemperatureSensorDisplay(sensor: HostMetricSample['temperatures']['sensors'][number]): string {
  const name = sensor.name.toLowerCase()
  const label = sensor.label.toLowerCase()
  if (label.includes('tctl') || label.includes('tdie') || label.includes('package') || name.includes('x86_pkg_temp')) {
    return 'CPU package sensor'
  }
  if (label.includes('core')) {
    return 'CPU core sensor'
  }
  return 'CPU sensor'
}

function DiskIORow({ diskIO }: { diskIO: HostMetricSample['disk_io'] }) {
  const topDevice = diskIO.devices[0]
  return (
    <div className="mt-2 space-y-1 text-xs text-[var(--muted)]">
      <div className="flex min-w-0 items-center justify-between gap-2">
        <span className="shrink-0">Disk I/O</span>
        <span className="min-w-0 truncate text-right text-[var(--text)]">{formatRate(diskIO.total_read_bytes_per_sec)} read · {formatRate(diskIO.total_write_bytes_per_sec)} write</span>
      </div>
      {topDevice && (
        <div className="truncate">
          {topDevice.name}: {formatRate(topDevice.read_bytes_per_sec)} read · {formatRate(topDevice.write_bytes_per_sec)} write
        </div>
      )}
    </div>
  )
}

function temperatureTextClass(value: number): string {
  if (value >= 85) return 'text-[var(--danger)]'
  if (value >= 75) return 'text-[var(--warning)]'
  return 'text-[var(--text)]'
}

function temperatureSensorLabel(sensor: HostMetricSample['temperatures']['sensors'][number]): string {
  if (sensor.label) {
    return `${sensor.name} · ${sensor.label}`
  }
  return sensor.name
}

function formatLoadAverage(values: number[]): string {
  return values.map((value) => value.toFixed(2)).join(' / ')
}

function formatTemperature(value: number): string {
  return `${value.toFixed(1)} °C`
}

function formatPercent(value: number): string {
  return `${Math.max(0, value).toFixed(1)}%`
}

function maskPublicIP(value: string): string {
  return value.includes(':') ? '****:****:****' : '***.***.***.***'
}

function formatRate(bytesPerSecond: number): string {
  return `${formatBytes(Math.max(0, Math.round(bytesPerSecond)))}/s`
}

const LOG_LEVELS = ['debug', 'info', 'warn', 'error'] as const

function StacklabLogs() {
  const [entries, setEntries] = useState<StacklabLogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [level, setLevel] = useState<string>('')
  const [filter, setFilter] = useState('')
  const [includeHttpAccess, setIncludeHttpAccess] = useState(false)
  const [following, setFollowing] = useState(true)
  const cursorRef = useRef<string | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const intervalRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)

  const fetchLogs = useCallback(async (append: boolean) => {
    try {
      const result = await getStacklabLogs({
        limit: append ? 50 : 200,
        cursor: append ? (cursorRef.current ?? undefined) : undefined,
        level: level || undefined,
        includeHttpAccess,
      })

      if (append) {
        if (result.items.length > 0) {
          setEntries((prev) => trimLogEntries([...prev, ...result.items]))
        }
      } else {
        setEntries(trimLogEntries(result.items))
      }

      if (result.next_cursor) {
        cursorRef.current = result.next_cursor
      }
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load logs')
    } finally {
      setLoading(false)
    }
  }, [includeHttpAccess, level])

  // Initial load
  useEffect(() => {
    setLoading(true)
    setEntries([])
    cursorRef.current = null
    fetchLogs(false)
  }, [fetchLogs])

  // Polling follow mode
  useEffect(() => {
    if (following) {
      intervalRef.current = setInterval(() => fetchLogs(true), 3_000)
    }
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [following, fetchLogs])

  // Auto-scroll when following
  useEffect(() => {
    if (following && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [entries, following])

  const filteredEntries = filter
    ? entries.filter((e) => e.message.toLowerCase().includes(filter.toLowerCase()))
    : entries

  const levelColor: Record<string, string> = {
    debug: 'text-stone-500',
    info: 'text-[var(--muted)]',
    warn: 'text-[var(--warning)]',
    error: 'text-[var(--danger)]',
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-lg font-medium text-[var(--text)]">Stacklab logs</h3>

        <div className="flex flex-wrap items-center gap-2">
          {/* Level filter */}
          <div className="flex gap-1">
            <button
              onClick={() => setLevel('')}
              className={cn(
                'rounded-md border px-2.5 py-1 text-xs transition',
                !level
                  ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                  : 'border-[var(--panel-border)] text-[var(--muted)]',
              )}
            >
              All
            </button>
            {LOG_LEVELS.map((l) => (
              <button
                key={l}
                onClick={() => setLevel(l)}
                className={cn(
                  'rounded-md border px-2.5 py-1 text-xs transition',
                  level === l
                    ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                    : 'border-[var(--panel-border)] text-[var(--muted)]',
                )}
              >
                {l}
              </button>
            ))}
          </div>

          <input
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter..."
            className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]"
          />

          <button
            type="button"
            title="Show HTTP access logs"
            onClick={() => setIncludeHttpAccess((value) => !value)}
            className={cn(
              'rounded-md border px-2.5 py-1 text-xs transition',
              includeHttpAccess
                ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                : 'border-[var(--panel-border)] text-[var(--muted)]',
            )}
          >
            HTTP
          </button>

          <button
            onClick={() => setFollowing(!following)}
            className={cn(
              'rounded-md border px-2.5 py-1 text-xs transition',
              following
                ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                : 'border-[var(--panel-border)] text-[var(--muted)]',
            )}
          >
            {following ? 'Following' : 'Paused'}
          </button>

          <button
            onClick={() => { setEntries([]); cursorRef.current = null; setLoading(true); fetchLogs(false) }}
            className="rounded-md border border-[var(--panel-border)] px-2.5 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
          >
            Refresh
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">
          {error}
        </div>
      )}

      <div
        ref={scrollRef}
        className="h-[calc(100vh-430px)] min-h-[320px] overflow-y-auto rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.3)] p-3 font-mono text-xs leading-5"
      >
        {loading && entries.length === 0 && (
          <div className="py-8 text-center text-[var(--muted)]">Loading logs...</div>
        )}

        {!loading && entries.length === 0 && !error && (
          <div className="py-8 text-center text-[var(--muted)]">
            No Stacklab log entries match the current view.
          </div>
        )}

        {filteredEntries.map((entry) => (
          <div key={entry.cursor || `${entry.timestamp}-${entry.message}`} className="flex gap-2 hover:bg-[rgba(255,255,255,0.02)]">
            <span className="shrink-0 text-stone-600">
              {new Date(entry.timestamp).toLocaleTimeString()}
            </span>
            <span className={cn('shrink-0 w-12', levelColor[entry.level] ?? 'text-[var(--muted)]')}>
              {entry.level}
            </span>
            <span className="text-[var(--text)] break-all">{entry.message}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function trimLogEntries(entries: StacklabLogEntry[]): StacklabLogEntry[] {
  if (entries.length <= MAX_STACKLAB_LOG_ENTRIES) return entries
  return entries.slice(entries.length - MAX_STACKLAB_LOG_ENTRIES)
}
