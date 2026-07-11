import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { StackOverviewPage } from './stack-overview-page'
import type { StackDetailResponse } from '@/lib/api-types'

const mockInvokeAction = vi.fn()
const mockUpdateStacksMaintenance = vi.fn()
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
  updates: null,
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
  updateStacksMaintenance: (...args: unknown[]) => mockUpdateStacksMaintenance(...args),
}))

vi.mock('@/hooks/use-job-stream', () => ({
  useJobStream: (...args: unknown[]) => mockUseJobStream(...args),
}))

describe('StackOverviewPage', () => {
  beforeEach(() => {
    outletStack = { ...baseStack }
    mockInvokeAction.mockReset()
    mockUpdateStacksMaintenance.mockReset()
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

    renderOverview()

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

    const { rerender } = renderOverview()

    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
    expect(screen.getByRole('dialog', { name: 'Stop stack "demo"?' })).toBeInTheDocument()
    expect(mockInvokeAction).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Stop stack' }))

    await waitFor(() => {
      expect(mockInvokeAction).toHaveBeenCalledWith('demo', 'stop')
    })
    expect(mockRefetch).not.toHaveBeenCalled()

    jobState = 'succeeded'
    rerender(wrapOverview())

    await waitFor(() => {
      expect(mockRefetch).toHaveBeenCalledTimes(1)
    })
  })

  it('does not stop a stack when confirmation is cancelled', () => {
    renderOverview()

    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(mockInvokeAction).not.toHaveBeenCalled()
  })

  it('explains the data impact before taking a stack down', async () => {
    outletStack = {
      ...baseStack,
      containers: [{
        id: 'abc123',
        name: 'demo-web-1',
        service_name: 'web',
        image_id: 'sha256:abc',
        image_ref: 'nginx:alpine',
        status: 'running',
        health_status: null,
        started_at: '2026-01-01T00:00:00Z',
        ports: [],
        networks: [],
      }],
    }
    mockInvokeAction.mockResolvedValue({
      job: { id: 'job_down_1', stack_id: 'demo', action: 'down', state: 'running' },
    })

    renderOverview()
    fireEvent.click(screen.getByRole('button', { name: 'Down' }))

    expect(screen.getByRole('dialog', { name: 'Take stack "demo" down?' })).toBeInTheDocument()
    expect(screen.getByText('1 running container(s)')).toBeInTheDocument()
    expect(screen.getByText('Persistent volumes are not deleted')).toBeInTheDocument()
    expect(mockInvokeAction).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Take down' }))
    await waitFor(() => {
      expect(mockInvokeAction).toHaveBeenCalledWith('demo', 'down')
    })
  })

  it('does not render Stop for a stopped stack', () => {
    outletStack = {
      ...baseStack,
      display_state: 'stopped',
      runtime_state: 'stopped',
      available_actions: ['up', 'down', 'pull', 'build', 'save_definition', 'remove_stack_definition'],
    }

    renderOverview()

    expect(screen.queryByRole('button', { name: 'Stop' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Down' })).toBeInTheDocument()
  })

  it('links service cards to filtered logs and shell views', () => {
    outletStack = {
      ...baseStack,
      services: [{
        name: 'web',
        mode: 'image',
        image_ref: 'nginx:alpine',
        build_context: null,
        dockerfile_path: null,
        ports: [],
        volumes: [],
        depends_on: [],
        healthcheck_present: true,
      }],
      containers: [{
        id: 'abc123',
        name: 'demo-web-1',
        service_name: 'web',
        image_id: 'sha256:abc',
        image_ref: 'nginx:alpine',
        status: 'running',
        health_status: null,
        started_at: '2026-01-01T00:00:00Z',
        ports: [],
        networks: [],
      }],
    }

    renderOverview()

    expect(screen.getByRole('link', { name: 'Logs' })).toHaveAttribute('href', '/stacks/demo/logs?service=web')
    expect(screen.getByRole('link', { name: 'Shell' })).toHaveAttribute('href', '/stacks/demo/terminal?service=web')
  })

  it('starts update for this stack when updates are available', async () => {
    outletStack = {
      ...baseStack,
      updates: {
        state: 'available',
        services_with_updates: 1,
        checked_at: '2026-07-09T03:00:00Z',
      },
    }
    mockUpdateStacksMaintenance.mockResolvedValue({
      job: { id: 'job_update_1', stack_id: null, action: 'update_stacks', state: 'running' },
    })

    renderOverview()
    fireEvent.click(screen.getByRole('button', { name: 'Update' }))

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
    expect(await screen.findByText('Job started.')).toBeInTheDocument()
  })
})

function wrapOverview() {
  return (
    <MemoryRouter initialEntries={['/stacks/demo']}>
      <StackOverviewPage />
    </MemoryRouter>
  )
}

function renderOverview() {
  return render(wrapOverview())
}
