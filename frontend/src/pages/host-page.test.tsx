import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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

vi.mock('@/components/system-health-center', () => ({
  SystemHealthCenter: () => <section aria-label="System health">System health center</section>,
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

  afterEach(() => {
    if (vi.isFakeTimers()) {
      vi.clearAllTimers()
    }
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('renders host overview cards and initial Stacklab logs', async () => {
    render(<HostPage />)

    expect(screen.getByRole('region', { name: 'System health' })).toHaveTextContent('System health center')
    expect(screen.getByRole('region', { name: 'Host overview' })).toBeInTheDocument()
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
	    expect(screen.getByRole('heading', { name: 'Stacklab logs' }).closest('section')).toHaveAttribute('id', 'stacklab-logs')

	    expect(mockGetHostOverview).toHaveBeenCalledTimes(1)
	    expect(mockGetHostMetrics).toHaveBeenCalled()
	    expect(mockGetHostMetrics).toHaveBeenNthCalledWith(1, undefined)
	    expect(mockGetStacklabLogs).toHaveBeenCalledWith({ limit: 200, cursor: undefined, level: undefined, include_http: false })
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
      expect(mockGetStacklabLogs).toHaveBeenLastCalledWith({ limit: 200, cursor: undefined, level: 'error', include_http: false })
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
      expect(mockGetStacklabLogs).toHaveBeenLastCalledWith({ limit: 200, cursor: undefined, level: undefined, include_http: true })
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

  it('recovers an initial host overview error without refetching metrics', async () => {
    vi.spyOn(globalThis, 'setInterval').mockImplementation(() => 1 as unknown as ReturnType<typeof setInterval>)
    mockGetHostOverview.mockRejectedValueOnce(new Error('Connection refused'))

    render(<HostPage />)

    const retry = await screen.findByRole('button', { name: 'Retry host overview' })
    expect(retry.closest('[role="alert"]')).toHaveTextContent('Failed to load host overview: Connection refused')
    expect(screen.queryByText('homelab')).not.toBeInTheDocument()
    expect(mockGetHostOverview).toHaveBeenCalledTimes(1)
    expect(mockGetHostMetrics).toHaveBeenCalledTimes(1)

    fireEvent.click(retry)

    expect(await screen.findByText('homelab')).toBeInTheDocument()
    expect(mockGetHostOverview).toHaveBeenCalledTimes(2)
    expect(mockGetHostMetrics).toHaveBeenCalledTimes(1)
  })

  it('shows an initial metrics error instead of a false waiting state and recovers on Retry', async () => {
    vi.spyOn(globalThis, 'setInterval').mockImplementation(() => 1 as unknown as ReturnType<typeof setInterval>)
    mockGetHostMetrics.mockRejectedValueOnce(new Error('Metrics unavailable'))

    render(<HostPage />)

    const retry = await screen.findByRole('button', { name: 'Retry host metrics' })
    expect(retry.closest('[role="alert"]')).toHaveTextContent('Failed to load host metrics: Metrics unavailable')
    expect(screen.queryByText('Waiting for host metrics...')).not.toBeInTheDocument()
    expect(mockGetHostOverview).toHaveBeenCalledTimes(1)
    expect(mockGetHostMetrics).toHaveBeenCalledTimes(1)

    fireEvent.click(retry)

    expect(await screen.findByRole('heading', { name: 'Host metrics' })).toBeInTheDocument()
    expect(mockGetHostOverview).toHaveBeenCalledTimes(1)
    expect(mockGetHostMetrics).toHaveBeenCalledTimes(2)
  })

  it('shows the waiting state only after a successful response without a current metrics sample', async () => {
    vi.spyOn(globalThis, 'setInterval').mockImplementation(() => 1 as unknown as ReturnType<typeof setInterval>)
    mockGetHostMetrics.mockResolvedValue({
      ...metrics,
      current: null,
      history: [],
    })

    render(<HostPage />)

    expect(await screen.findByText('Waiting for host metrics...')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Retry host metrics' })).not.toBeInTheDocument()
    expect(screen.queryByText(/Failed to load host metrics/)).not.toBeInTheDocument()
  })

  it('keeps the last metrics snapshot after a polling failure and recovers on Retry', async () => {
    const refreshedMetrics: HostMetricsResponse = {
      ...metrics,
      current: metrics.current && {
        ...metrics.current,
        sampled_at: '2026-04-04T12:00:11Z',
        cpu: {
          ...metrics.current.cpu,
          usage_percent: 3.2,
        },
      },
    }
    mockGetHostMetrics
      .mockResolvedValueOnce(metrics)
      .mockRejectedValueOnce(new Error('Metrics refresh failed'))
      .mockResolvedValueOnce(refreshedMetrics)
    let metricsPoll: (() => void) | null = null
    vi.spyOn(globalThis, 'setInterval').mockImplementation((handler: TimerHandler, timeout?: number) => {
      if (timeout === 1_000 && metricsPoll === null) {
        metricsPoll = () => {
          if (typeof handler === 'function') handler()
        }
      }
      return 1 as unknown as ReturnType<typeof setInterval>
    })

    render(<HostPage />)

    expect((await screen.findAllByText('17.5%')).length).toBeGreaterThan(0)
    expect(metricsPoll).not.toBeNull()

    await act(async () => {
      metricsPoll?.()
      await Promise.resolve()
      await Promise.resolve()
    })

    const retry = await screen.findByRole('button', { name: 'Retry host metrics' })
    expect(retry.closest('[role="alert"]')).toHaveTextContent('Metrics refresh failed')
    expect(retry.closest('[role="alert"]')).toHaveTextContent('Showing the last successfully loaded data.')
    expect(screen.getAllByText('17.5%').length).toBeGreaterThan(0)

    fireEvent.click(retry)

    expect((await screen.findAllByText('3.2%')).length).toBeGreaterThan(0)
    expect(mockGetHostMetrics).toHaveBeenCalledTimes(3)
  })

  it('keeps the last overview after a polling failure and recovers on Retry', async () => {
    mockGetHostOverview
      .mockResolvedValueOnce(overview)
      .mockRejectedValueOnce(new Error('Overview refresh failed'))
      .mockResolvedValueOnce({
        ...overview,
        host: { ...overview.host, hostname: 'homelab-refreshed' },
      })
    let overviewPoll: (() => void) | null = null
    vi.spyOn(globalThis, 'setInterval').mockImplementation((handler: TimerHandler, timeout?: number) => {
      if (timeout === 5_000 && overviewPoll === null) {
        overviewPoll = () => {
          if (typeof handler === 'function') handler()
        }
      }
      return 1 as unknown as ReturnType<typeof setInterval>
    })

    render(<HostPage />)

    expect(await screen.findByText('homelab')).toBeInTheDocument()
    expect(overviewPoll).not.toBeNull()

    await act(async () => {
      overviewPoll?.()
      await Promise.resolve()
      await Promise.resolve()
    })

    const retry = await screen.findByRole('button', { name: 'Retry host overview' })
    expect(retry.closest('[role="alert"]')).toHaveTextContent('Overview refresh failed')
    expect(retry.closest('[role="alert"]')).toHaveTextContent('Showing the last successfully loaded data.')
    expect(screen.getByText('homelab')).toBeInTheDocument()

    fireEvent.click(retry)

    expect(await screen.findByText('homelab-refreshed')).toBeInTheDocument()
    expect(mockGetHostOverview).toHaveBeenCalledTimes(3)
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

  it('does not overlap host overview polling while a request is in flight', async () => {
    const pendingOverview = deferred<HostOverviewResponse>()
    mockGetHostOverview
      .mockResolvedValueOnce(overview)
      .mockReturnValueOnce(pendingOverview.promise)
      .mockResolvedValue({
        ...overview,
        host: { ...overview.host, hostname: 'homelab-refreshed' },
      })
    let overviewPoll: (() => void) | null = null
    vi.spyOn(globalThis, 'setInterval').mockImplementation((handler: TimerHandler, timeout?: number) => {
      if (timeout === 5_000 && overviewPoll === null) {
        overviewPoll = () => {
          if (typeof handler === 'function') {
            handler()
          }
        }
      }
      return 1 as unknown as ReturnType<typeof setInterval>
    })
    vi.spyOn(globalThis, 'clearInterval').mockImplementation(() => {})

    render(<HostPage />)

    expect(await screen.findByText('homelab')).toBeInTheDocument()
    expect(mockGetHostOverview).toHaveBeenCalledTimes(1)
    expect(overviewPoll).not.toBeNull()

    await act(async () => {
      overviewPoll?.()
      await Promise.resolve()
    })
    expect(mockGetHostOverview).toHaveBeenCalledTimes(2)

    await act(async () => {
      overviewPoll?.()
      await Promise.resolve()
    })
    expect(mockGetHostOverview).toHaveBeenCalledTimes(2)

    await act(async () => {
      pendingOverview.resolve(overview)
      await pendingOverview.promise
    })

    await act(async () => {
      overviewPoll?.()
      await Promise.resolve()
    })
    expect(mockGetHostOverview).toHaveBeenCalledTimes(3)
  })

  it('pauses host polling while hidden and refreshes on visibility and focus', async () => {
    const hiddenSpy = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    vi.useFakeTimers()

    const view = render(<HostPage />)
    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(mockGetHostOverview).toHaveBeenCalledTimes(1)
    expect(mockGetHostMetrics).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000)
    })
    expect(mockGetHostOverview).toHaveBeenCalledTimes(2)
    expect(mockGetHostMetrics.mock.calls.length).toBeGreaterThan(1)

    hiddenSpy.mockReturnValue(true)
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
      await Promise.resolve()
    })
    const hiddenOverviewCalls = mockGetHostOverview.mock.calls.length
    const hiddenMetricsCalls = mockGetHostMetrics.mock.calls.length

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000)
    })
    expect(mockGetHostOverview).toHaveBeenCalledTimes(hiddenOverviewCalls)
    expect(mockGetHostMetrics).toHaveBeenCalledTimes(hiddenMetricsCalls)

    hiddenSpy.mockReturnValue(false)
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(mockGetHostOverview).toHaveBeenCalledTimes(hiddenOverviewCalls + 1)
    expect(mockGetHostMetrics).toHaveBeenCalledTimes(hiddenMetricsCalls + 1)

    await act(async () => {
      window.dispatchEvent(new Event('focus'))
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(mockGetHostOverview).toHaveBeenCalledTimes(hiddenOverviewCalls + 2)
    expect(mockGetHostMetrics).toHaveBeenCalledTimes(hiddenMetricsCalls + 2)

    view.unmount()
    const unmountedOverviewCalls = mockGetHostOverview.mock.calls.length
    const unmountedMetricsCalls = mockGetHostMetrics.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000)
      window.dispatchEvent(new Event('focus'))
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(mockGetHostOverview).toHaveBeenCalledTimes(unmountedOverviewCalls)
    expect(mockGetHostMetrics).toHaveBeenCalledTimes(unmountedMetricsCalls)
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

  it('pauses and resumes log following from the latest cursor', async () => {
    mockGetStacklabLogs
      .mockResolvedValueOnce(logsResponse)
      .mockResolvedValueOnce({
        items: [{
          timestamp: '2026-04-04T12:00:02Z',
          level: 'warn',
          message: 'First followed entry',
          cursor: 'cursor-3',
        }],
        next_cursor: 'cursor-3',
        has_more: false,
      })
      .mockResolvedValueOnce({
        items: [{
          timestamp: '2026-04-04T12:00:03Z',
          level: 'info',
          message: 'Following resumed',
          cursor: 'cursor-4',
        }],
        next_cursor: 'cursor-4',
        has_more: false,
      })
    vi.useFakeTimers()

    render(<HostPage />)
    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(mockGetStacklabLogs).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: 'Following' })).toHaveAttribute('aria-pressed', 'true')

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000)
    })
    expect(mockGetStacklabLogs).toHaveBeenLastCalledWith({
      limit: 50,
      cursor: 'cursor-2',
      level: undefined,
      include_http: false,
    })

    fireEvent.click(screen.getByRole('button', { name: 'Following' }))
    expect(screen.getByRole('button', { name: 'Paused' })).toHaveAttribute('aria-pressed', 'false')
    const pausedCallCount = mockGetStacklabLogs.mock.calls.length

    await act(async () => {
      await vi.advanceTimersByTimeAsync(6_000)
    })
    expect(mockGetStacklabLogs).toHaveBeenCalledTimes(pausedCallCount)

    fireEvent.click(screen.getByRole('button', { name: 'Paused' }))
    expect(screen.getByRole('button', { name: 'Following' })).toHaveAttribute('aria-pressed', 'true')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000)
    })
    expect(mockGetStacklabLogs).toHaveBeenLastCalledWith({
      limit: 50,
      cursor: 'cursor-3',
      level: undefined,
      include_http: false,
    })
  })

  it('deduplicates followed Stacklab logs by journal cursor', async () => {
    mockGetStacklabLogs
      .mockResolvedValueOnce(logsResponse)
      .mockResolvedValue({
        items: [
          logsResponse.items[1],
          {
            timestamp: '2026-04-04T12:00:02Z',
            level: 'warn',
            message: 'New followed entry',
            cursor: 'cursor-3',
          },
        ],
        next_cursor: 'cursor-3',
        has_more: false,
      })
    vi.useFakeTimers()

    render(<HostPage />)
    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
      await vi.advanceTimersByTimeAsync(3_000)
    })

    expect(screen.getAllByText('Failed to bind port')).toHaveLength(1)
    expect(screen.getByText('New followed entry')).toBeInTheDocument()
  })

  it('filters logs locally and resets the cursor on manual refresh', async () => {
    render(<HostPage />)

    expect(await screen.findByText('Started HTTP server')).toBeInTheDocument()
    expect(mockGetStacklabLogs).toHaveBeenCalledTimes(1)

    fireEvent.change(screen.getByPlaceholderText('Filter...'), {
      target: { value: 'failed to bind' },
    })
    expect(screen.queryByText('Started HTTP server')).not.toBeInTheDocument()
    expect(screen.getByText('Failed to bind port')).toBeInTheDocument()
    expect(mockGetStacklabLogs).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))

    await waitFor(() => {
      expect(mockGetStacklabLogs).toHaveBeenCalledTimes(2)
    })
    expect(mockGetStacklabLogs).toHaveBeenLastCalledWith({
      limit: 200,
      cursor: undefined,
      level: undefined,
      include_http: false,
    })
    expect(screen.queryByText('Started HTTP server')).not.toBeInTheDocument()
    expect(await screen.findByText('Failed to bind port')).toBeInTheDocument()
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
