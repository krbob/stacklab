import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { HostPage } from './host-page'
import { formatUptime } from './host-page-utils'
import type { HostOverviewResponse, StacklabLogsResponse } from '@/lib/api-types'

const mockGetHostOverview = vi.fn()
const mockGetStacklabLogs = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getHostOverview: (...args: unknown[]) => mockGetHostOverview(...args),
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

describe('HostPage', () => {
  beforeEach(() => {
    vi.useRealTimers()
    mockGetHostOverview.mockReset()
    mockGetStacklabLogs.mockReset()
    mockGetHostOverview.mockResolvedValue(overview)
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
    expect(await screen.findByText('Started HTTP server')).toBeInTheDocument()
    expect(screen.getByText('Failed to bind port')).toBeInTheDocument()

    expect(mockGetHostOverview).toHaveBeenCalledTimes(1)
    expect(mockGetStacklabLogs).toHaveBeenCalledWith({ limit: 200, cursor: undefined, level: undefined })
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
      expect(mockGetStacklabLogs).toHaveBeenLastCalledWith({ limit: 200, cursor: undefined, level: 'error' })
    })
    expect(await screen.findByText('Failed to bind port')).toBeInTheDocument()
  })

  it('shows empty logs state', async () => {
    mockGetStacklabLogs.mockResolvedValue({
      items: [],
      next_cursor: null,
      has_more: false,
    })

    render(<HostPage />)

    await screen.findByText('homelab')
    expect(await screen.findByText(/No logs available/)).toBeInTheDocument()
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

  it('polls host overview and updates resource values without remounting', async () => {
    mockGetHostOverview
      .mockResolvedValueOnce(overview)
      .mockResolvedValueOnce({
        ...overview,
        resources: {
          ...overview.resources,
          cpu: {
            ...overview.resources.cpu,
            usage_percent: 3.2,
          },
        },
      })
    let overviewPoll: (() => void) | null = null
    const setIntervalSpy = vi.spyOn(globalThis, 'setInterval')
    setIntervalSpy.mockImplementation((handler: TimerHandler, timeout?: number) => {
      if (timeout === 15_000) {
        overviewPoll = () => {
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

    expect(await screen.findByText('17.5%')).toBeInTheDocument()
    expect(overviewPoll).not.toBeNull()

    await act(async () => {
      overviewPoll?.()
      await Promise.resolve()
    })

    await waitFor(() => {
      expect(mockGetHostOverview).toHaveBeenCalledTimes(2)
    })
    expect(await screen.findByText('3.2%')).toBeInTheDocument()

    setIntervalSpy.mockRestore()
    clearIntervalSpy.mockRestore()
  })

  it('formats uptime with seconds precision', () => {
    expect(formatUptime(61)).toBe('1m 1s')
    expect(formatUptime(3661)).toBe('1h 1m 1s')
    expect(formatUptime(90061)).toBe('1d 1h 1m 1s')
  })
})
