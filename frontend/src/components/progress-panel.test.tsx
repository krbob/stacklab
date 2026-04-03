import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ProgressPanel } from './progress-panel'

// Mock the job stream hook
vi.mock('@/hooks/use-job-stream', () => ({
  useJobStream: vi.fn(() => ({ events: [], state: null })),
}))

import { useJobStream } from '@/hooks/use-job-stream'
const mockUseJobStream = vi.mocked(useJobStream)

describe('ProgressPanel', () => {
  it('renders nothing when jobId is null', () => {
    const { container } = render(<ProgressPanel jobId={null} />)
    expect(container.firstChild).toBeNull()
  })

  it('shows pending state with no events', () => {
    mockUseJobStream.mockReturnValue({ events: [], state: null, clear: vi.fn() })
    render(<ProgressPanel jobId="job_123" />)
    expect(screen.getByText('Pending')).toBeInTheDocument()
  })

  it('shows running state', () => {
    mockUseJobStream.mockReturnValue({
      events: [{
        job_id: 'job_123', stack_id: 'test', action: 'pull', state: 'running',
        event: 'job_started', message: 'Job started.', timestamp: '2026-01-01T00:00:00Z',
        data: null, step: null,
      }],
      state: 'running',
      clear: vi.fn(),
    })
    render(<ProgressPanel jobId="job_123" />)
    expect(screen.getByText('Running')).toBeInTheDocument()
  })

  it('shows succeeded state', () => {
    mockUseJobStream.mockReturnValue({
      events: [{
        job_id: 'job_123', stack_id: 'test', action: 'pull', state: 'succeeded',
        event: 'job_finished', message: 'Job finished.', timestamp: '2026-01-01T00:00:00Z',
        data: null, step: null,
      }],
      state: 'succeeded',
      clear: vi.fn(),
    })
    render(<ProgressPanel jobId="job_123" />)
    expect(screen.getByText('Succeeded')).toBeInTheDocument()
  })

  it('shows failed state', () => {
    mockUseJobStream.mockReturnValue({
      events: [{
        job_id: 'job_123', stack_id: 'test', action: 'up', state: 'failed',
        event: 'job_error', message: 'Container failed to start', timestamp: '2026-01-01T00:00:00Z',
        data: null, step: null,
      }],
      state: 'failed',
      clear: vi.fn(),
    })
    render(<ProgressPanel jobId="job_123" />)
    expect(screen.getByText('Failed')).toBeInTheDocument()
  })

  it('shows warning indicator on succeeded with warnings', () => {
    mockUseJobStream.mockReturnValue({
      events: [
        {
          job_id: 'job_123', stack_id: 'test', action: 'save_definition', state: 'succeeded',
          event: 'job_finished', message: 'Saved.', timestamp: '2026-01-01T00:00:00Z',
          data: null, step: null,
        },
        {
          job_id: 'job_123', stack_id: 'test', action: 'save_definition', state: 'succeeded',
          event: 'job_warning', message: 'Config invalid after save', timestamp: '2026-01-01T00:00:01Z',
          data: null, step: null,
        },
      ],
      state: 'succeeded',
      clear: vi.fn(),
    })
    render(<ProgressPanel jobId="job_123" />)
    expect(screen.getByText('Succeeded')).toBeInTheDocument()
    expect(screen.getByText('with warnings')).toBeInTheDocument()
  })

  it('calls onDone when job reaches terminal state', () => {
    const onDone = vi.fn()
    mockUseJobStream.mockReturnValue({
      events: [],
      state: 'succeeded',
      clear: vi.fn(),
    })
    render(<ProgressPanel jobId="job_123" onDone={onDone} />)
    expect(onDone).toHaveBeenCalledWith('succeeded')
  })
})
