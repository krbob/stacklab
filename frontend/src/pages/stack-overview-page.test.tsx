import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { StackOverviewPage } from './stack-overview-page'
import type { StackDetailResponse } from '@/lib/api-types'

const mockInvokeAction = vi.fn()
const mockRefetch = vi.fn()
const mockUseJobStream = vi.fn()
let outletStack: StackDetailResponse['stack']

const baseStack: StackDetailResponse['stack'] = {
  id: 'demo',
  name: 'demo',
  root_path: '/srv/stacklab/stacks/demo',
  compose_file_path: '/srv/stacklab/stacks/demo/compose.yaml',
  env_file_path: '/srv/stacklab/stacks/demo/.env',
  config_path: '/srv/stacklab/config/demo',
  data_path: '/srv/stacklab/data/demo',
  display_state: 'running',
  runtime_state: 'running',
  config_state: 'in_sync',
  activity_state: 'idle',
  health_summary: {
    healthy_container_count: 0,
    unhealthy_container_count: 0,
    unknown_health_container_count: 0,
  },
  capabilities: {
    can_edit_definition: true,
    can_view_logs: true,
    can_view_stats: true,
    can_open_terminal: true,
  },
  available_actions: ['up', 'restart', 'stop', 'down', 'pull', 'build', 'recreate', 'save_definition', 'remove_stack_definition'],
  services: [],
  containers: [],
  last_deployed_at: null,
  last_action: null,
}

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useOutletContext: () => ({
      stack: outletStack,
      refetch: mockRefetch,
    }),
  }
})

vi.mock('@/lib/api-client', () => ({
  invokeAction: (...args: unknown[]) => mockInvokeAction(...args),
}))

vi.mock('@/hooks/use-job-stream', () => ({
  useJobStream: (...args: unknown[]) => mockUseJobStream(...args),
}))

describe('StackOverviewPage', () => {
  beforeEach(() => {
    outletStack = { ...baseStack }
    mockInvokeAction.mockReset()
    mockRefetch.mockReset()
    mockUseJobStream.mockReset()
    mockUseJobStream.mockImplementation(({ jobId }: { jobId: string | null }) => ({
      events: jobId ? [{
        job_id: jobId,
        stack_id: 'demo',
        action: 'pull',
        state: 'running',
        event: 'job_started',
        message: 'Job started.',
        timestamp: '2026-01-01T00:00:00Z',
        data: null,
        step: null,
      }] : [],
      state: jobId ? 'running' : null,
      clear: vi.fn(),
    }))
  })

  it('shows progress and disables actions while a stack action is running', async () => {
    mockInvokeAction.mockResolvedValue({
      job: { id: 'job_pull_1', stack_id: 'demo', action: 'pull', state: 'running' },
    })

    render(<StackOverviewPage />)

    fireEvent.click(screen.getByRole('button', { name: 'Pull' }))

    await waitFor(() => {
      expect(mockInvokeAction).toHaveBeenCalledWith('demo', 'pull')
    })
    expect(await screen.findByText('Job started.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Build' })).toBeDisabled()
    expect(mockRefetch).not.toHaveBeenCalled()
  })

  it('refetches when the active action job finishes', async () => {
    mockInvokeAction.mockResolvedValue({
      job: { id: 'job_stop_1', stack_id: 'demo', action: 'stop', state: 'running' },
    })

    let jobState = 'running'
    mockUseJobStream.mockImplementation(({ jobId }: { jobId: string | null }) => ({
      events: jobId ? [{
        job_id: jobId,
        stack_id: 'demo',
        action: 'stop',
        state: jobState,
        event: jobState === 'running' ? 'job_started' : 'job_finished',
        message: jobState === 'running' ? 'Job started.' : 'Job finished successfully.',
        timestamp: '2026-01-01T00:00:00Z',
        data: null,
        step: null,
      }] : [],
      state: jobId ? jobState : null,
      clear: vi.fn(),
    }))

    const { rerender } = render(<StackOverviewPage />)

    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))

    await waitFor(() => {
      expect(mockInvokeAction).toHaveBeenCalledWith('demo', 'stop')
    })
    expect(mockRefetch).not.toHaveBeenCalled()

    jobState = 'succeeded'
    rerender(<StackOverviewPage />)

    await waitFor(() => {
      expect(mockRefetch).toHaveBeenCalledTimes(1)
    })
  })

  it('does not render Stop for a stopped stack', () => {
    outletStack = {
      ...baseStack,
      display_state: 'stopped',
      runtime_state: 'stopped',
      available_actions: ['up', 'down', 'pull', 'build', 'save_definition', 'remove_stack_definition'],
    }

    render(<StackOverviewPage />)

    expect(screen.queryByRole('button', { name: 'Stop' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Down' })).toBeInTheDocument()
  })
})
