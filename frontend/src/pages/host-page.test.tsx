import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { HostPage } from './host-page'
import { formatUptime } from './host-page-utils'
import type { HostMetricsResponse, HostOverviewResponse, StacklabLogsResponse } from '@/lib/api-types'

const mockGetHostOverview = vi.fn()
const mockGetHostMetrics = vi.fn()
const mockGetStacklabLogs = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getHostOverview: (...args: unknown[]) => mockGetHostOverview(...args),
  getHostMetrics: (...args: unknown[]) => mockGetHostMetrics(...args),
  getStacklabLogs: (...args: unknown[]) => mockGetStacklabLogs(...args),
}))

const overview: HostOverviewResponse = {
  host: {
    hostname: 'homelab',
    os_name: 'Debian GNU/Linux',
    kernel_version: '6.1.0',
    architecture: 'amd64',
    uptime_seconds: 90061,
  },
  stacklab: {
    version: '2026.04.0',
    commit: 'abcdef1234567890',
    started_at: '2026-04-04T12:00:00Z',
  },
  docker: {
    engine_version: '28.5.1',
    compose_version: '2.40.0',
  },
  resources: {
    cpu: {
      core_count: 8,
      load_average: [0.4, 0.3, 0.2],
      usage_percent: 17.5,
    },
    memory: {
      total_bytes: 8 * 1024 * 1024 * 1024,
      used_bytes: 3 * 1024 * 1024 * 1024,
      available_bytes: 5 * 1024 * 1024 * 1024,
      usage_percent: 37.5,
    },
    disk: {
      path: '/opt/stacklab',
      total_bytes: 100 * 1024 * 1024 * 1024,
      used_bytes: 25 * 1024 * 1024 * 1024,
      available_bytes: 75 * 1024 * 1024 * 1024,
      usage_percent: 25,
    },
  },
}

const logsResponse: StacklabLogsResponse = {
  items: [
    {
      timestamp: '2026-04-04T12:00:00Z',
      level: 'info',
      message: 'Started HTTP server',
      cursor: 'cursor-1',
    },
    {
      timestamp: '2026-04-04T12:00:01Z',
      level: 'error',
      message: 'Failed to bind port',
      cursor: 'cursor-2',
    },
  ],
  next_cursor: 'cursor-2',
  has_more: false,
}

function makeLogEntry(index: number) {
  return {
    timestamp: `2026-04-04T12:${String(Math.floor(index / 60)).padStart(2, '0')}:${String(index % 60).padStart(2, '0')}Z`,
    level: 'info',
    message: `log-entry-${index}`,
    cursor: `cursor-${index}`,
  } satisfies StacklabLogsResponse['items'][number]
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

const metrics: HostMetricsResponse = {
  sample_interval_seconds: 1,
  background_sample_interval_seconds: 30,
  active_sample_interval_seconds: 1,
  history_window_seconds: 1800,
  current: {
    sampled_at: '2026-04-04T12:00:10Z',
    cpu: overview.resources.cpu,
    memory: overview.resources.memory,
    swap: {
      total_bytes: 2 * 1024 * 1024 * 1024,
      used_bytes: 512 * 1024 * 1024,
      available_bytes: 1536 * 1024 * 1024,
      usage_percent: 25,
    },
    temperatures: {
      cpu_celsius: 42.5,
      cpu_sensor: {
        name: 'coretemp',
        label: 'Package id 0',
        temperature_celsius: 42.5,
      },
      sensors: [
        {
          name: 'coretemp',
          label: 'Package id 0',
          temperature_celsius: 42.5,
        },
      ],
    },
    filesystems: [
      {
        mount_point: '/srv/stacklab',
        device: '/dev/nvme0n1p2',
        fs_type: 'ext4',
        total_bytes: 200 * 1024 * 1024 * 1024,
        used_bytes: 50 * 1024 * 1024 * 1024,
        available_bytes: 150 * 1024 * 1024 * 1024,
        usage_percent: 25,
        primary: true,
      },
    ],
    disk_io: {
      total_read_bytes_per_sec: 4096,
      total_write_bytes_per_sec: 2048,
      devices: [
        {
          name: 'nvme0n1',
          read_bytes: 1024 * 1024,
          write_bytes: 512 * 1024,
          read_bytes_per_sec: 4096,
          write_bytes_per_sec: 2048,
        },
      ],
    },
    network: {
      total_rx_bytes_per_sec: 2048,
      total_tx_bytes_per_sec: 1024,
      public_ip: '8.8.8.8',
      interfaces: [
        {
          name: 'eth0',
          rx_bytes: 200 * 1024,
          tx_bytes: 100 * 1024,
          rx_bytes_per_sec: 2048,
          tx_bytes_per_sec: 1024,
        },
      ],
    },
    processes: {
      total: 3,
      items: [
        {
          pid: 1234,
          user: 'stacklab',
          state: 'S',
          cpu_percent: 12.5,
          memory_bytes: 256 * 1024 * 1024,
          memory_percent: 3.1,
          command: 'stacklab',
        },
        {
          pid: 2222,
          user: 'postgres',
          state: 'R',
          cpu_percent: 2,
          memory_bytes: 1024 * 1024 * 1024,
          memory_percent: 12.5,
          command: 'postgres',
        },
        {
          pid: 3333,
          user: 'minecraft',
          state: 'S',
          cpu_percent: 1,
          memory_bytes: 768 * 1024 * 1024,
          memory_percent: 9.4,
          command: 'java',
          display_command: 'java -jar server.jar nogui',
          container: {
            id: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
            name: 'minecraft-server-1',
            stack_id: 'minecraft',
            service_name: 'server',
          },
        },
      ],
    },
  },
  history: [
    {
      sampled_at: '2026-04-04T12:00:09Z',
      cpu: { ...overview.resources.cpu, usage_percent: 12.5 },
      memory: { ...overview.resources.memory, usage_percent: 35 },
      swap: {
        total_bytes: 2 * 1024 * 1024 * 1024,
        used_bytes: 256 * 1024 * 1024,
        available_bytes: 1792 * 1024 * 1024,
        usage_percent: 12.5,
      },
      temperatures: {
        cpu_celsius: 41.8,
        cpu_sensor: {
          name: 'coretemp',
          label: 'Package id 0',
          temperature_celsius: 41.8,
        },
        sensors: [
          {
            name: 'coretemp',
            label: 'Package id 0',
            temperature_celsius: 41.8,
          },
        ],
      },
      filesystems: [
        {
          mount_point: '/srv/stacklab',
          device: '/dev/nvme0n1p2',
          fs_type: 'ext4',
          total_bytes: 200 * 1024 * 1024 * 1024,
          used_bytes: 49 * 1024 * 1024 * 1024,
          available_bytes: 151 * 1024 * 1024 * 1024,
          usage_percent: 24.5,
          primary: true,
        },
      ],
      disk_io: {
        total_read_bytes_per_sec: 2048,
        total_write_bytes_per_sec: 1024,
        devices: [
          {
            name: 'nvme0n1',
            read_bytes: 1020 * 1024,
            write_bytes: 510 * 1024,
            read_bytes_per_sec: 2048,
            write_bytes_per_sec: 1024,
          },
        ],
      },
      network: {
        total_rx_bytes_per_sec: 1024,
        total_tx_bytes_per_sec: 512,
        public_ip: '8.8.8.8',
        interfaces: [
          {
            name: 'eth0',
            rx_bytes: 198 * 1024,
            tx_bytes: 99 * 1024,
            rx_bytes_per_sec: 1024,
            tx_bytes_per_sec: 512,
          },
        ],
      },
    },
  ],
}

describe('HostPage', () => {
  beforeEach(() => {
    vi.useRealTimers()
    mockGetHostOverview.mockReset()
    mockGetHostMetrics.mockReset()
    mockGetStacklabLogs.mockReset()
    mockGetHostOverview.mockResolvedValue(overview)
    mockGetHostMetrics.mockResolvedValue(metrics)
    mockGetStacklabLogs.mockResolvedValue(logsResponse)
  })

  it('renders host overview cards and initial Stacklab logs', async () => {
    render(<HostPage />)

    expect(await screen.findByText('2026.04.0')).toBeInTheDocument()
    expect(screen.getByText('homelab')).toBeInTheDocument()
    expect(screen.getByText('Debian GNU/Linux')).toBeInTheDocument()
    expect(screen.getByText('amd64')).toBeInTheDocument()
    expect(screen.getByText(/Engine 28\.5\.1/)).toBeInTheDocument()
    expect(screen.getByText(/Compose 2\.40\.0/)).toBeInTheDocument()
    expect(screen.getByText('Host metrics')).toBeInTheDocument()
    expect(screen.getAllByText('Swap').length).toBeGreaterThan(0)
    expect(screen.getAllByText('512.0 MiB / 2.0 GiB').length).toBeGreaterThan(0)
    expect(screen.getByText('CPU temp')).toBeInTheDocument()
    expect(screen.getAllByText('42.5 °C').length).toBeGreaterThan(0)
    expect(screen.getByText(/load avg 1\/5\/15m 0\.40 \/ 0\.30 \/ 0\.20/)).toBeInTheDocument()
    expect(screen.getByText('CPU package sensor')).toBeInTheDocument()
    expect(screen.getAllByText(/4\.0 KiB\/s read/).length).toBeGreaterThan(0)
    expect(screen.getByText(/nvme0n1:/)).toBeInTheDocument()
    expect(screen.getByText('/srv/stacklab')).toBeInTheDocument()
    expect(screen.getAllByText('eth0').length).toBeGreaterThan(0)
    expect(screen.getAllByText(/2\.0 KiB\/s/).length).toBeGreaterThan(0)
    expect(screen.getByText('Public IP')).toBeInTheDocument()
    expect(screen.getByText('***.***.***.***')).toBeInTheDocument()
    expect(screen.queryByText('8.8.8.8')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Show public IP' }))
    expect(screen.getByText('8.8.8.8')).toBeInTheDocument()
    expect(screen.getByText('Top processes')).toBeInTheDocument()
    expect(screen.getByText('3 visible')).toBeInTheDocument()
    expect(screen.getAllByText('postgres').length).toBeGreaterThan(0)
    expect(screen.getByText('minecraft / server')).toBeInTheDocument()
    expect(screen.getByText('java -jar server.jar nogui')).toBeInTheDocument()
    expect(screen.getByText('1234')).toBeInTheDocument()
    expect(screen.getAllByText('12.5%').length).toBeGreaterThan(0)
	    expect(await screen.findByText('Started HTTP server')).toBeInTheDocument()
	    expect(screen.getByText('Failed to bind port')).toBeInTheDocument()

	    expect(mockGetHostOverview).toHaveBeenCalledTimes(1)
	    expect(mockGetHostMetrics).toHaveBeenCalled()
	    expect(mockGetHostMetrics).toHaveBeenNthCalledWith(1, undefined)
	    expect(mockGetStacklabLogs).toHaveBeenCalledWith({ limit: 200, cursor: undefined, level: undefined, includeHttpAccess: false })
	  })

  it('reloads logs when level filter changes', async () => {
    mockGetStacklabLogs
      .mockResolvedValueOnce(logsResponse)
      .mockResolvedValueOnce({
        ...logsResponse,
        items: [logsResponse.items[1]],
        next_cursor: 'cursor-2',
      })

    render(<HostPage />)

    await screen.findByText('Started HTTP server')
    fireEvent.click(screen.getByRole('button', { name: 'error' }))

    await waitFor(() => {
      expect(mockGetStacklabLogs).toHaveBeenLastCalledWith({ limit: 200, cursor: undefined, level: 'error', includeHttpAccess: false })
    })
    expect(await screen.findByText('Failed to bind port')).toBeInTheDocument()
  })

  it('reloads logs when HTTP access visibility changes', async () => {
    mockGetStacklabLogs
      .mockResolvedValueOnce(logsResponse)
      .mockResolvedValueOnce({
        ...logsResponse,
        items: [
          ...logsResponse.items,
          {
            timestamp: '2026-04-04T12:00:02Z',
            level: 'info',
            message: 'time=2026-04-04T12:00:02Z level=INFO msg="http request" method=GET path=/api/host/metrics status=200',
            cursor: 'cursor-3',
          },
        ],
        next_cursor: 'cursor-3',
      })

    render(<HostPage />)

    await screen.findByText('Started HTTP server')
    fireEvent.click(screen.getByRole('button', { name: 'HTTP' }))

    await waitFor(() => {
      expect(mockGetStacklabLogs).toHaveBeenLastCalledWith({ limit: 200, cursor: undefined, level: undefined, includeHttpAccess: true })
    })
    expect(await screen.findByText(/path=\/api\/host\/metrics/)).toBeInTheDocument()
  })

  it('shows empty logs state', async () => {
    mockGetStacklabLogs.mockResolvedValue({
      items: [],
      next_cursor: null,
      has_more: false,
    })

    render(<HostPage />)

    await screen.findByText('homelab')
    expect(await screen.findByText(/No Stacklab log entries match/)).toBeInTheDocument()
  })

  it('shows host overview error', async () => {
    mockGetHostOverview.mockRejectedValue(new Error('Connection refused'))

    render(<HostPage />)

    expect(await screen.findByText(/Connection refused/)).toBeInTheDocument()
  })

  it('displays architecture in system card', async () => {
    render(<HostPage />)
    expect(await screen.findByText('amd64')).toBeInTheDocument()
  })

  it('polls host metrics and updates resource values without remounting', async () => {
    mockGetHostMetrics
      .mockResolvedValueOnce(metrics)
      .mockResolvedValueOnce({
        ...metrics,
        current: metrics.current && {
          ...metrics.current,
          sampled_at: '2026-04-04T12:00:11Z',
          cpu: {
            ...metrics.current.cpu,
            usage_percent: 3.2,
          },
        },
        history: metrics.current
          ? [{
              ...metrics.current,
              sampled_at: '2026-04-04T12:00:11Z',
              cpu: {
                ...metrics.current.cpu,
                usage_percent: 3.2,
              },
            }]
          : [],
      })
    let metricsPoll: (() => void) | null = null
    const setIntervalSpy = vi.spyOn(globalThis, 'setInterval')
    setIntervalSpy.mockImplementation((handler: TimerHandler, timeout?: number) => {
      if (timeout === 1_000 && metricsPoll === null) {
        metricsPoll = () => {
          if (typeof handler === 'function') {
            handler()
          }
        }
      }
      return 1 as unknown as ReturnType<typeof setInterval>
    })
    const clearIntervalSpy = vi.spyOn(globalThis, 'clearInterval')
    clearIntervalSpy.mockImplementation(() => {})

    render(<HostPage />)

    expect((await screen.findAllByText('17.5%')).length).toBeGreaterThan(0)
    expect(metricsPoll).not.toBeNull()

    await act(async () => {
      metricsPoll?.()
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(mockGetHostMetrics).toHaveBeenCalledTimes(2)
    })
    expect(mockGetHostMetrics).toHaveBeenNthCalledWith(1, undefined)
    expect(mockGetHostMetrics).toHaveBeenNthCalledWith(2, { since: '2026-04-04T12:00:10Z' })
    expect((await screen.findAllByText('3.2%')).length).toBeGreaterThan(0)

    setIntervalSpy.mockRestore()
    clearIntervalSpy.mockRestore()
  })

  it('does not overlap host metrics polling while a request is in flight', async () => {
    const pendingMetrics = deferred<HostMetricsResponse>()
    mockGetHostMetrics
      .mockResolvedValueOnce(metrics)
      .mockReturnValue(pendingMetrics.promise)
    let metricsPoll: (() => void) | null = null
    const setIntervalSpy = vi.spyOn(globalThis, 'setInterval')
    setIntervalSpy.mockImplementation((handler: TimerHandler, timeout?: number) => {
      if (timeout === 1_000 && metricsPoll === null) {
        metricsPoll = () => {
          if (typeof handler === 'function') {
            handler()
          }
        }
      }
      return 1 as unknown as ReturnType<typeof setInterval>
    })
    const clearIntervalSpy = vi.spyOn(globalThis, 'clearInterval')
    clearIntervalSpy.mockImplementation(() => {})

    render(<HostPage />)

    expect(await screen.findByText('Host metrics')).toBeInTheDocument()
    expect(mockGetHostMetrics).toHaveBeenCalledTimes(1)
    expect(metricsPoll).not.toBeNull()

    await act(async () => {
      metricsPoll?.()
      await Promise.resolve()
    })
    expect(mockGetHostMetrics).toHaveBeenCalledTimes(2)

    await act(async () => {
      metricsPoll?.()
      await Promise.resolve()
    })
    expect(mockGetHostMetrics).toHaveBeenCalledTimes(2)

    await act(async () => {
      pendingMetrics.resolve({
        ...metrics,
        current: metrics.current && {
          ...metrics.current,
          sampled_at: '2026-04-04T12:00:11Z',
        },
      })
      await pendingMetrics.promise
    })

    setIntervalSpy.mockRestore()
    clearIntervalSpy.mockRestore()
  })

  it('keeps followed Stacklab logs bounded to the newest entries', async () => {
    const initialLogs = Array.from({ length: 995 }, (_, index) => makeLogEntry(index))
    const appendedLogs = Array.from({ length: 10 }, (_, index) => makeLogEntry(995 + index))
    mockGetStacklabLogs
      .mockResolvedValueOnce({
        items: initialLogs,
        next_cursor: 'cursor-994',
        has_more: false,
      })
      .mockResolvedValueOnce({
        items: appendedLogs,
        next_cursor: 'cursor-1004',
        has_more: false,
      })
    let logsPoll: (() => void) | null = null
    const setIntervalSpy = vi.spyOn(globalThis, 'setInterval')
    setIntervalSpy.mockImplementation((handler: TimerHandler, timeout?: number) => {
      if (timeout === 3_000 && logsPoll === null) {
        logsPoll = () => {
          if (typeof handler === 'function') {
            handler()
          }
        }
      }
      return 1 as unknown as ReturnType<typeof setInterval>
    })
    const clearIntervalSpy = vi.spyOn(globalThis, 'clearInterval')
    clearIntervalSpy.mockImplementation(() => {})

    render(<HostPage />)

    expect(await screen.findByText('log-entry-994')).toBeInTheDocument()
    expect(logsPoll).not.toBeNull()

    await act(async () => {
      logsPoll?.()
      await Promise.resolve()
    })

    expect(await screen.findByText('log-entry-1004')).toBeInTheDocument()
    expect(screen.queryByText('log-entry-0')).not.toBeInTheDocument()
    expect(screen.queryByText('log-entry-4')).not.toBeInTheDocument()
    expect(screen.getByText('log-entry-5')).toBeInTheDocument()

    setIntervalSpy.mockRestore()
    clearIntervalSpy.mockRestore()
  })

  it('uses fixed 0-100 scale for percentage sparklines', async () => {
    mockGetHostMetrics.mockResolvedValue({
      ...metrics,
      current: metrics.current && {
        ...metrics.current,
        cpu: {
          ...metrics.current.cpu,
          usage_percent: 1,
        },
      },
      history: [
        {
          ...metrics.history[0],
          cpu: {
            ...metrics.history[0].cpu,
            usage_percent: 1,
          },
        },
      ],
    })

    render(<HostPage />)

    const cpuSparkline = await screen.findByRole('img', { name: 'CPU usage history' })
    expect(cpuSparkline.querySelector('polyline')?.getAttribute('points')).toBe('0,33.7 120,33.7')
  })

  it('uses danger tone for saturated resources', async () => {
    mockGetHostMetrics.mockResolvedValue({
      ...metrics,
      current: metrics.current && {
        ...metrics.current,
        memory: {
          ...metrics.current.memory,
          usage_percent: 99,
        },
      },
    })

    render(<HostPage />)

    const saturatedValues = await screen.findAllByText('99.0%')
    expect(saturatedValues.some((node) => node.className.includes('text-[var(--danger)]'))).toBe(true)
  })

  it('formats uptime with seconds precision', () => {
    expect(formatUptime(61)).toBe('1m 1s')
    expect(formatUptime(3661)).toBe('1h 1m 1s')
    expect(formatUptime(90061)).toBe('1d 1h 1m 1s')
  })
})
