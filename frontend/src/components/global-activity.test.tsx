import { act, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { GlobalActivity } from './global-activity'
import type { ActiveJobsResponse, JobDetail } from '@/lib/api-types'

const mockGetActiveJobs = vi.fn()
const mockGetJob = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getActiveJobs: (...args: unknown[]) => mockGetActiveJobs(...args),
  getJob: (...args: unknown[]) => mockGetJob(...args),
}))

function renderActivity() {
  return render(
    <MemoryRouter>
      <GlobalActivity />
    </MemoryRouter>,
  )
}

const activeResponse: ActiveJobsResponse = {
  items: [
    {
      id: 'job_1',
      stack_id: 'demo',
      action: 'pull',
      state: 'running',
      requested_at: '2026-04-09T08:00:00Z',
      started_at: '2026-04-09T08:00:01Z',
      current_step: {
        index: 1,
        total: 2,
        action: 'pull',
        target_stack_id: 'demo',
      },
      latest_event: {
        event: 'job_step_started',
        message: 'Starting pull for demo.',
        timestamp: '2026-04-09T08:00:02Z',
        step: {
          index: 1,
          total: 2,
          action: 'pull',
          target_stack_id: 'demo',
        },
      },
      workflow: {
        steps: [
          { action: 'pull', state: 'running', target_stack_id: 'demo' },
          { action: 'up', state: 'queued', target_stack_id: 'demo' },
        ],
      },
    },
  ],
  summary: {
    active_count: 1,
    running_count: 1,
    queued_count: 0,
    cancel_requested_count: 0,
  },
}

describe('GlobalActivity', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mockGetActiveJobs.mockReset()
    mockGetJob.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('does not render when idle', async () => {
    mockGetActiveJobs.mockResolvedValue({
      items: [],
      summary: {
        active_count: 0,
        running_count: 0,
        queued_count: 0,
        cancel_requested_count: 0,
      },
    } satisfies ActiveJobsResponse)

    renderActivity()

    await act(async () => {
      vi.advanceTimersByTime(0)
      await Promise.resolve()
    })

    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('renders active job and opens popover', async () => {
    mockGetActiveJobs.mockResolvedValue(activeResponse)

    renderActivity()

    await act(async () => {
      vi.advanceTimersByTime(0)
      await Promise.resolve()
    })

    expect(screen.getByRole('button')).toBeInTheDocument()
    expect(screen.getByText('pull · demo')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button'))

    expect(screen.getByText('Activity')).toBeInTheDocument()
    expect(screen.getByText('1/2')).toBeInTheDocument()
  })

  it('shows succeeded recent job and auto-removes it after linger', async () => {
    mockGetActiveJobs
      .mockResolvedValueOnce(activeResponse)
      .mockResolvedValue({
        items: [],
        summary: {
          active_count: 0,
          running_count: 0,
          queued_count: 0,
          cancel_requested_count: 0,
        },
      } satisfies ActiveJobsResponse)
    mockGetJob.mockResolvedValue({
      job: {
        id: 'job_1',
        stack_id: 'demo',
        action: 'pull',
        state: 'succeeded',
        requested_at: '2026-04-09T08:00:00Z',
        started_at: '2026-04-09T08:00:01Z',
        finished_at: '2026-04-09T08:00:05Z',
        workflow: null,
      } satisfies JobDetail,
    })

    renderActivity()

    await act(async () => {
      vi.advanceTimersByTime(0)
      await Promise.resolve()
    })

    expect(screen.getByText('pull · demo')).toBeInTheDocument()

    await act(async () => {
      vi.advanceTimersByTime(3000)
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(screen.getByText('Done')).toBeInTheDocument()

    await act(async () => {
      vi.advanceTimersByTime(5000)
      await Promise.resolve()
    })

    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('keeps failed recent job visible', async () => {
    mockGetActiveJobs
      .mockResolvedValueOnce(activeResponse)
      .mockResolvedValue({
        items: [],
        summary: {
          active_count: 0,
          running_count: 0,
          queued_count: 0,
          cancel_requested_count: 0,
        },
      } satisfies ActiveJobsResponse)
    mockGetJob.mockResolvedValue({
      job: {
        id: 'job_1',
        stack_id: 'demo',
        action: 'pull',
        state: 'failed',
        requested_at: '2026-04-09T08:00:00Z',
        started_at: '2026-04-09T08:00:01Z',
        finished_at: '2026-04-09T08:00:05Z',
        workflow: null,
      } satisfies JobDetail,
    })

    renderActivity()

    await act(async () => {
      vi.advanceTimersByTime(0)
      await Promise.resolve()
    })

    expect(screen.getByText('pull · demo')).toBeInTheDocument()

    await act(async () => {
      vi.advanceTimersByTime(3000)
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(screen.getByText('Failed · pull · demo')).toBeInTheDocument()

    await act(async () => {
      vi.advanceTimersByTime(5000)
      await Promise.resolve()
    })

    expect(screen.getByText('Failed · pull · demo')).toBeInTheDocument()
  })
})
