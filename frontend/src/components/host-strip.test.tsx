import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ActivityContextValue } from '@/contexts/activity-context'
import { useActivity } from '@/hooks/use-activity'
import { HostStrip } from './host-strip'

const mockGetMeta = vi.fn()
const mockOpenJob = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getMeta: (...args: unknown[]) => mockGetMeta(...args),
}))

vi.mock('@/hooks/use-activity', () => ({
  useActivity: vi.fn(),
}))

vi.mock('@/hooks/use-job-drawer', () => ({
  useJobDrawer: () => ({ jobId: null, openJob: mockOpenJob, closeJob: vi.fn() }),
}))

const mockUseActivity = vi.mocked(useActivity)
const freshIdle: ActivityContextValue = {
  response: {
    items: [],
    summary: { active_count: 0, running_count: 0, queued_count: 0, cancel_requested_count: 0 },
  },
  freshness: 'fresh',
  updatedAt: 1,
}

describe('HostStrip activity state', () => {
  beforeEach(() => {
    mockGetMeta.mockReset()
    mockOpenJob.mockReset()
    mockGetMeta.mockResolvedValue({
      app: { name: 'Stacklab', version: 'test' },
      environment: { stack_root: '/srv/stacklab', platform: 'linux' },
      docker: { engine_version: '29.0.0', compose_version: '2.40.0' },
      features: { host_shell: true },
    })
    mockUseActivity.mockReturnValue(freshIdle)
  })

  it('shows idle only for a fresh empty activity snapshot', () => {
    render(<HostStrip />)

    expect(screen.getByText('idle')).toBeInTheDocument()
  })

  it('keeps activity visible while host metadata is loading', () => {
    mockGetMeta.mockReturnValue(new Promise(() => {}))

    render(<HostStrip />)

    expect(screen.getByRole('status')).toHaveTextContent('host metadata loading')
    expect(screen.getByText('idle')).toBeInTheDocument()
  })

  it('shows a metadata error without hiding activity and recovers on retry', async () => {
    mockGetMeta
      .mockRejectedValueOnce(new Error('metadata offline'))
      .mockResolvedValueOnce({
        app: { name: 'Stacklab', version: 'recovered' },
        environment: { stack_root: '/srv/stacklab', platform: 'linux' },
        docker: { engine_version: '29.1.0', compose_version: '2.41.0' },
        features: { host_shell: true },
      })

    render(<HostStrip />)

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Host metadata unavailable: metadata offline',
    )
    expect(screen.getByText('idle')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Retry host metadata' }))

    expect(await screen.findByText('stacklab recovered')).toBeInTheDocument()
    expect(screen.getByText('docker 29.1.0')).toBeInTheDocument()
    expect(screen.getByText('compose 2.41.0')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByText('idle')).toBeInTheDocument()
    expect(mockGetMeta).toHaveBeenCalledTimes(2)
  })

  it('does not show idle before the first activity snapshot', () => {
    mockUseActivity.mockReturnValue({ response: null, freshness: 'loading', updatedAt: null })

    render(<HostStrip />)

    expect(screen.getByText('activity loading')).toBeInTheDocument()
    expect(screen.queryByText('idle')).not.toBeInTheDocument()
  })

  it('shows unavailable activity with an automatic retry indication', () => {
    mockUseActivity.mockReturnValue({ response: null, freshness: 'unavailable', updatedAt: null })

    render(<HostStrip />)

    expect(screen.getByText('activity unavailable · retrying')).toBeInTheDocument()
    expect(screen.queryByText('idle')).not.toBeInTheDocument()
  })

  it('keeps a stale active job available without a running pulse', () => {
    mockUseActivity.mockReturnValue({
      response: {
        items: [{
          id: 'job_1',
          stack_id: 'demo',
          action: 'pull',
          state: 'running',
          requested_at: '2026-07-12T07:00:00Z',
          started_at: '2026-07-12T07:00:01Z',
        }],
        summary: { active_count: 1, running_count: 1, queued_count: 0, cancel_requested_count: 0 },
      },
      freshness: 'stale',
      updatedAt: 1,
    })

    render(<HostStrip />)

    const jobButton = screen.getByRole('button', { name: 'pull · demo · stale' })
    const dot = jobButton.parentElement?.querySelector('[aria-hidden="true"]')
    expect(dot).toHaveClass('bg-[var(--warning)]')
    expect(dot).not.toHaveClass('animate-pulse')
    fireEvent.click(jobButton)
    expect(mockOpenJob).toHaveBeenCalledWith('job_1')
  })
})
