import type { ReactNode } from 'react'
import type { HostMetricSample } from '@/lib/api-types'
import { cn } from '@/lib/cn'
import { formatBytes } from '@/pages/host-page-utils'
import { formatRate, formatTemperature } from '@/pages/host/host-metric-format'
import { PercentBar } from '@/pages/host/metric-primitives'
import { utilizationTone } from '@/pages/host/metric-style'

export function MetricCard({
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

export function FilesystemRow({ filesystem }: { filesystem: HostMetricSample['filesystems'][number] }) {
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
      <PercentBar value={filesystem.usage_percent} color={tone.bar} label={`${filesystem.mount_point} usage`} />
    </div>
  )
}

export function SwapRow({ swap }: { swap: HostMetricSample['swap'] }) {
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
      <PercentBar value={swap.usage_percent} color={tone.bar} label="Swap usage" />
    </div>
  )
}

export function TemperatureRow({ temperatures }: { temperatures: HostMetricSample['temperatures'] }) {
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

export function DiskIORow({ diskIO }: { diskIO: HostMetricSample['disk_io'] }) {
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
