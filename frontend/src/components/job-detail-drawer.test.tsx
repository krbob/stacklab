import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { JobDetailDrawer } from './job-detail-drawer'
import { JobDrawerProvider } from '@/contexts/job-drawer-context'
import { useJobDrawer } from '@/hooks/use-job-drawer'

const mockGetJob = vi.fn()
const mockGetJobEvents = vi.fn()
const mockCancelJob = vi.fn()

vi.mock('@/lib/api-client', () => ({
  cancelJob: (...args: unknown[]) => mockCancelJob(...args),
  getJob: (...args: unknown[]) => mockGetJob(...args),
  getJobEvents: (...args: unknown[]) => mockGetJobEvents(...args),
}))

function OpenButton({ jobId }: { jobId: string }) {
  const { openJob } = useJobDrawer()
  return <button onClick={() => openJob(jobId)}>Open</button>
}

function renderDrawer(jobId = 'job_1') {
  return render(
    <MemoryRouter>
      <JobDrawerProvider>
        <OpenButton jobId={jobId} />
        <JobDetailDrawer />
      </JobDrawerProvider>
    </MemoryRouter>,
  )
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })

  return { promise, resolve, reject }
}

describe('JobDetailDrawer', () => {
  beforeEach(() => {
    mockGetJob.mockReset()
    mockGetJobEvents.mockReset()
    mockCancelJob.mockReset()
  })

  it('renders retained job detail with events', async () => {
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
      },
    })
    mockGetJobEvents.mockResolvedValue({
      job_id: 'job_1',
      retained: true,
      items: [
        {
          job_id: 'job_1',
          sequence: 1,
          event: 'job_started',
          state: 'running',
          message: 'Job started.',
          timestamp: '2026-04-09T08:00:01Z',
        },
      ],
    })

    renderDrawer()
    fireEvent.click(screen.getByRole('button', { name: 'Open' }))

    expect(await screen.findByText('Job detail')).toBeInTheDocument()
    const dialog = screen.getByRole('dialog', { name: 'Job detail' })
    expect(dialog.querySelector('[aria-busy]')).toHaveAttribute('aria-busy', 'false')
    expect(screen.getByRole('status')).toHaveTextContent('succeeded')
    expect(screen.getByRole('status')).toHaveAttribute('aria-live', 'polite')
    expect(await screen.findByText('Job started.')).toBeInTheDocument()
    expect(screen.getByText('job_1')).toBeInTheDocument()
    expect(screen.getByText('Action')).toBeInTheDocument()
  })

  it('preserves structured progress from retained events', async () => {
    mockGetJob.mockResolvedValue({
      job: {
        id: 'job_progress',
        stack_id: 'demo',
        action: 'pull',
        state: 'succeeded',
        requested_at: '2026-04-09T08:00:00Z',
      },
    })
    mockGetJobEvents.mockResolvedValue({
      job_id: 'job_progress',
      retained: true,
      items: [
        {
          job_id: 'job_progress',
          sequence: 1,
          event: 'job_step_started',
          state: 'running',
          message: 'Starting pull.',
          step: { index: 1, total: 1, action: 'pull' },
          timestamp: '2026-04-09T08:00:01Z',
        },
        {
          job_id: 'job_progress',
          sequence: 2,
          event: 'job_progress',
          state: 'running',
          message: 'Pulling layers.',
          step: { index: 1, total: 1, action: 'pull' },
          progress: { phase: 'pull', completed: 7, total: 12, unit: 'layers', detail: 'extracting' },
          timestamp: '2026-04-09T08:00:02Z',
        },
      ],
    })

    renderDrawer('job_progress')
    fireEvent.click(screen.getByRole('button', { name: 'Open' }))

    const progress = await screen.findByRole('progressbar', { name: 'pull progress' })
    expect(progress).toHaveAttribute('aria-valuenow', '7')
    expect(progress).toHaveAttribute('aria-valuemax', '12')
    expect(screen.getByText('extracting')).toBeInTheDocument()
  })

  it('shows retention notice when detailed output is gone', async () => {
    mockGetJob.mockResolvedValue({
      job: {
        id: 'job_2',
        stack_id: null,
        action: 'update_stacks',
        state: 'succeeded',
        requested_at: '2026-04-09T08:00:00Z',
        started_at: '2026-04-09T08:00:01Z',
        finished_at: '2026-04-09T08:00:05Z',
        workflow: null,
      },
    })
    mockGetJobEvents.mockResolvedValue({
      job_id: 'job_2',
      retained: false,
      message: 'Detailed output for this job is no longer retained.',
      items: [],
    })

    renderDrawer('job_2')
    fireEvent.click(screen.getByRole('button', { name: 'Open' }))

    expect(await screen.findByText('Detailed output for this job is no longer retained.')).toBeInTheDocument()
  })

  it('cancels a running job from the drawer', async () => {
    mockGetJob.mockResolvedValue({
      job: {
        id: 'job_3',
        stack_id: 'demo',
        action: 'pull',
        state: 'running',
        requested_at: '2026-04-09T08:00:00Z',
        started_at: '2026-04-09T08:00:01Z',
        finished_at: null,
        workflow: null,
      },
    })
    mockGetJobEvents.mockResolvedValue({
      job_id: 'job_3',
      retained: true,
      items: [],
    })
    mockCancelJob.mockResolvedValue({ job: { id: 'job_3', state: 'cancel_requested' } })

    renderDrawer('job_3')
    fireEvent.click(screen.getByRole('button', { name: 'Open' }))

    fireEvent.click(await screen.findByRole('button', { name: 'Cancel job' }))

    await waitFor(() => expect(mockCancelJob).toHaveBeenCalledWith('job_3'))
    await waitFor(() => expect(mockGetJob).toHaveBeenCalledTimes(2))
  })

  it('retries job details without reloading successful events', async () => {
    mockGetJob
      .mockRejectedValueOnce(new Error('job backend unavailable'))
      .mockResolvedValueOnce({
        job: {
          id: 'job_retry',
          stack_id: 'demo',
          action: 'pull',
          state: 'succeeded',
          requested_at: '2026-04-09T08:00:00Z',
        },
      })
    mockGetJobEvents.mockResolvedValue({
      job_id: 'job_retry',
      retained: true,
      items: [
        {
          job_id: 'job_retry',
          sequence: 1,
          event: 'job_started',
          state: 'running',
          message: 'The event history is available.',
          timestamp: '2026-04-09T08:00:01Z',
        },
      ],
    })

    renderDrawer('job_retry')
    fireEvent.click(screen.getByRole('button', { name: 'Open' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Failed to load job details: job backend unavailable',
    )
    expect(screen.getByText('The event history is available.')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Retry job details' }))

    expect(await screen.findByText('job_retry')).toBeInTheDocument()
    expect(mockGetJob).toHaveBeenCalledTimes(2)
    expect(mockGetJobEvents).toHaveBeenCalledTimes(1)
  })

  it('retries job events without hiding successful job details or showing a false empty state', async () => {
    mockGetJob.mockResolvedValue({
      job: {
        id: 'job_events_retry',
        stack_id: 'demo',
        action: 'pull',
        state: 'succeeded',
        requested_at: '2026-04-09T08:00:00Z',
      },
    })
    mockGetJobEvents
      .mockRejectedValueOnce(new Error('event store unavailable'))
      .mockResolvedValueOnce({
        job_id: 'job_events_retry',
        retained: true,
        items: [
          {
            job_id: 'job_events_retry',
            sequence: 1,
            event: 'job_succeeded',
            state: 'succeeded',
            message: 'The event history recovered.',
            timestamp: '2026-04-09T08:00:05Z',
          },
        ],
      })

    renderDrawer('job_events_retry')
    fireEvent.click(screen.getByRole('button', { name: 'Open' }))

    expect(await screen.findByText('job_events_retry')).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Failed to load job events: event store unavailable',
    )
    expect(screen.queryByText('No events recorded for this job.')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Retry job events' }))

    expect(await screen.findByText('The event history recovered.')).toBeInTheDocument()
    expect(mockGetJob).toHaveBeenCalledTimes(1)
    expect(mockGetJobEvents).toHaveBeenCalledTimes(2)
  })

  it('preserves running job snapshots and events across polling failures', async () => {
    vi.useFakeTimers()
    try {
      const jobResponse = {
        job: {
          id: 'job_4',
          stack_id: 'demo',
          action: 'pull',
          state: 'running',
          requested_at: '2026-04-09T08:00:00Z',
          started_at: '2026-04-09T08:00:01Z',
          finished_at: null,
          workflow: null,
        },
      }
      const eventsResponse = {
        job_id: 'job_4',
        retained: true,
        items: [
          {
            job_id: 'job_4',
            sequence: 1,
            event: 'job_started',
            state: 'running',
            message: 'Original event remains visible.',
            timestamp: '2026-04-09T08:00:01Z',
          },
        ],
      }
      mockGetJob.mockResolvedValue(jobResponse)
      mockGetJobEvents.mockResolvedValue(eventsResponse)

      renderDrawer('job_4')
      fireEvent.click(screen.getByRole('button', { name: 'Open' }))

      await act(async () => {
        await Promise.resolve()
        await Promise.resolve()
      })

      expect(screen.getByText('Job detail')).toBeInTheDocument()
      expect(screen.getByText('job_4')).toBeInTheDocument()
      expect(screen.getByText('Original event remains visible.')).toBeInTheDocument()
      expect(mockGetJob).toHaveBeenCalledTimes(1)
      expect(mockGetJobEvents).toHaveBeenCalledTimes(1)

      const jobRefresh = deferred<typeof jobResponse>()
      const eventsRefresh = deferred<typeof eventsResponse>()
      mockGetJob.mockReturnValueOnce(jobRefresh.promise)
      mockGetJobEvents.mockReturnValueOnce(eventsRefresh.promise)

      await act(async () => {
        vi.advanceTimersByTime(1000)
        await Promise.resolve()
      })

      expect(mockGetJob).toHaveBeenCalledTimes(2)
      expect(mockGetJobEvents).toHaveBeenCalledTimes(2)
      expect(screen.getByText('job_4')).toBeInTheDocument()
      expect(screen.getByText('Original event remains visible.')).toBeInTheDocument()
      expect(screen.getAllByText('Refreshing…')).toHaveLength(2)

      await act(async () => {
        jobRefresh.reject(new Error('job refresh failed'))
        eventsRefresh.reject(new Error('events refresh failed'))
        await Promise.allSettled([jobRefresh.promise, eventsRefresh.promise])
        await Promise.resolve()
      })

      expect(screen.getByText('job_4')).toBeInTheDocument()
      expect(screen.getByText('Original event remains visible.')).toBeInTheDocument()
      expect(screen.getByText('Failed to load job details: job refresh failed')).toBeInTheDocument()
      expect(screen.getByText('Failed to load job events: events refresh failed')).toBeInTheDocument()
      expect(screen.getAllByText('Showing the last successfully loaded data.')).toHaveLength(2)
      expect(screen.getByRole('button', { name: 'Retry job details' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Retry job events' })).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })
})
