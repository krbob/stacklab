import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { JobDetailDrawer } from './job-detail-drawer'
import { JobDrawerProvider } from '@/contexts/job-drawer-context'
import { useJobDrawer } from '@/hooks/use-job-drawer'

const mockGetJob = vi.fn()
const mockGetJobEvents = vi.fn()

vi.mock('@/lib/api-client', () => ({
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

describe('JobDetailDrawer', () => {
  beforeEach(() => {
    mockGetJob.mockReset()
    mockGetJobEvents.mockReset()
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
    expect(await screen.findByText('Job started.')).toBeInTheDocument()
    expect(screen.getByText('job_1')).toBeInTheDocument()
    expect(screen.getByText('Action')).toBeInTheDocument()
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
})
