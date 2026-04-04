import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MaintenancePage } from './maintenance-page'
import type { StackListResponse } from '@/lib/api-types'

const mockUpdateStacksMaintenance = vi.fn()
const mockUseApi = vi.fn()
const mockUseJobStream = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getStacks: vi.fn(),
  updateStacksMaintenance: (...args: unknown[]) => mockUpdateStacksMaintenance(...args),
}))

vi.mock('@/hooks/use-api', () => ({
  useApi: (...args: unknown[]) => mockUseApi(...args),
}))

vi.mock('@/hooks/use-job-stream', () => ({
  useJobStream: (...args: unknown[]) => mockUseJobStream(...args),
}))

const stacksData: StackListResponse = {
  items: [
    {
      id: 'builder',
      name: 'builder',
      display_state: 'defined',
      runtime_state: 'defined',
      config_state: 'unknown',
      activity_state: 'idle',
      health_summary: {
        healthy_container_count: 0,
        unhealthy_container_count: 0,
        unknown_health_container_count: 0,
      },
      service_count: { defined: 1, running: 0 },
      last_action: null,
    },
    {
      id: 'demo',
      name: 'demo',
      display_state: 'running',
      runtime_state: 'running',
      config_state: 'in_sync',
      activity_state: 'idle',
      health_summary: {
        healthy_container_count: 1,
        unhealthy_container_count: 0,
        unknown_health_container_count: 0,
      },
      service_count: { defined: 1, running: 1 },
      last_action: null,
    },
  ],
  summary: {
    stack_count: 2,
    running_count: 1,
    stopped_count: 0,
    error_count: 0,
    container_count: { running: 1, total: 1 },
  },
}

describe('MaintenancePage', () => {
  beforeEach(() => {
    mockUseApi.mockReset()
    mockUseJobStream.mockReset()
    mockUpdateStacksMaintenance.mockReset()

    mockUseApi.mockReturnValue({
      data: stacksData,
      error: null,
      loading: false,
      refetch: vi.fn(),
    })
    mockUseJobStream.mockImplementation(({ jobId }: { jobId: string | null }) => (
      jobId
        ? {
            events: [
              {
                job_id: jobId,
                stack_id: null,
                action: 'update_stacks',
                state: 'running',
                event: 'job_step_started',
                message: 'Starting pull for demo.',
                data: null,
                step: { index: 1, total: 2, action: 'pull', target_stack_id: 'demo' },
                timestamp: '2026-04-04T16:00:00Z',
              },
            ],
            state: 'running',
            clear: vi.fn(),
          }
        : {
            events: [],
            state: null,
            clear: vi.fn(),
          }
    ))
  })

  it('starts selected-stack maintenance and renders step progress', async () => {
    mockUpdateStacksMaintenance.mockResolvedValue({
      job: { id: 'job_maint_123', stack_id: null, action: 'update_stacks', state: 'running' },
    })

    render(<MaintenancePage />)

    fireEvent.click(screen.getByLabelText('Selected stacks'))
    fireEvent.click(screen.getByLabelText(/demo/))
    fireEvent.click(screen.getByTestId('maintenance-start'))

    await waitFor(() => {
      expect(mockUpdateStacksMaintenance).toHaveBeenCalledWith({
        target: {
          mode: 'selected',
          stack_ids: ['demo'],
        },
        options: {
          pull_images: true,
          build_images: true,
          remove_orphans: true,
          prune_after: {
            enabled: false,
            include_volumes: false,
          },
        },
      })
    })

    expect(await screen.findByText('Running')).toBeInTheDocument()
    expect(screen.getByText('Step 1/2')).toBeInTheDocument()
    const stepRow = screen.getByText('pull').closest('div')
    expect(stepRow).not.toBeNull()
    expect(within(stepRow!).getByText('demo')).toBeInTheDocument()
  })

  it('never sends include_volumes when prune_after is disabled', async () => {
    mockUpdateStacksMaintenance.mockResolvedValue({
      job: { id: 'job_maint_456', stack_id: null, action: 'update_stacks', state: 'running' },
    })

    render(<MaintenancePage />)

    fireEvent.click(screen.getByText('Run prune after update'))
    fireEvent.click(screen.getByText('Include volumes in prune'))
    fireEvent.click(screen.getByText('Run prune after update'))
    fireEvent.click(screen.getByTestId('maintenance-start'))

    await waitFor(() => {
      expect(mockUpdateStacksMaintenance).toHaveBeenCalledWith(expect.objectContaining({
        options: expect.objectContaining({
          prune_after: {
            enabled: false,
            include_volumes: false,
          },
        }),
      }))
    })
  })

  it('starts all-stacks maintenance with correct payload', async () => {
    mockUpdateStacksMaintenance.mockResolvedValue({
      job: { id: 'job_all', stack_id: null, action: 'update_stacks', state: 'running' },
    })

    render(<MaintenancePage />)

    // "All stacks" is default
    fireEvent.click(screen.getByTestId('maintenance-start'))

    await waitFor(() => {
      expect(mockUpdateStacksMaintenance).toHaveBeenCalledWith(expect.objectContaining({
        target: { mode: 'all', stack_ids: undefined },
      }))
    })
  })

  it('shows error when maintenance request fails', async () => {
    mockUpdateStacksMaintenance.mockRejectedValue(new Error('Docker unavailable'))

    render(<MaintenancePage />)

    fireEvent.click(screen.getByTestId('maintenance-start'))

    expect(await screen.findByText('Docker unavailable')).toBeInTheDocument()
  })

  it('disables start button while job is running', async () => {
    mockUpdateStacksMaintenance.mockResolvedValue({
      job: { id: 'job_run', stack_id: null, action: 'update_stacks', state: 'running' },
    })

    render(<MaintenancePage />)
    fireEvent.click(screen.getByTestId('maintenance-start'))

    await waitFor(() => {
      expect(mockUpdateStacksMaintenance).toHaveBeenCalled()
    })

    expect(screen.getByTestId('maintenance-start')).toBeDisabled()
    expect(screen.getByText('Running...')).toBeInTheDocument()
  })
})
