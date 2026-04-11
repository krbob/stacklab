import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { GlobalActivity } from './global-activity'
import { JobDetailDrawer } from './job-detail-drawer'
import { JobDrawerProvider } from '@/contexts/job-drawer-context'
import type { ActiveJobsResponse, JobDetail } from '@/lib/api-types'

const mockGetActiveJobs = vi.fn()
const mockGetJob = vi.fn()
const mockGetJobEvents = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getActiveJobs: (...args: unknown[]) => mockGetActiveJobs(...args),
  getJob: (...args: unknown[]) => mockGetJob(...args),
  getJobEvents: (...args: unknown[]) => mockGetJobEvents(...args),
}))

function renderActivity() {
  return render(
    <MemoryRouter>
      <JobDrawerProvider>
        <GlobalActivity />
        <JobDetailDrawer />
      </JobDrawerProvider>
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
    mockGetJobEvents.mockReset()
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

  it('opens drawer and closes popover when clicking a job row', async () => {
    mockGetActiveJobs.mockResolvedValue(activeResponse)
    mockGetJob.mockResolvedValue({
      job: {
        id: 'job_1',
        stack_id: 'demo',
        action: 'pull',
        state: 'running',
        requested_at: '2026-04-09T08:00:00Z',
        started_at: '2026-04-09T08:00:01Z',
        finished_at: null,
        workflow: null,
      } satisfies JobDetail,
    })
    mockGetJobEvents.mockResolvedValue({
      job_id: 'job_1',
      retained: true,
      items: [],
    })

    renderActivity()

    await act(async () => {
      vi.advanceTimersByTime(0)
      await Promise.resolve()
    })

    fireEvent.click(screen.getAllByRole('button')[0])
    expect(screen.getByText('Activity')).toBeInTheDocument()

    const popover = screen.getByText('Activity').parentElement
    expect(popover).not.toBeNull()
    fireEvent.click(within(popover as HTMLElement).getAllByRole('button')[0])

    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(screen.getByText('Job detail')).toBeInTheDocument()
    expect(screen.queryByText('Activity')).not.toBeInTheDocument()
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
