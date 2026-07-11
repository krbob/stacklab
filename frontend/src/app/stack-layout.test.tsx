import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { StackDetailResponse } from '@/lib/api-types'
import { PageMetadataProvider } from './page-metadata'
import { StackLayout } from './stack-layout'

const mockGetStack = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getStack: (...args: unknown[]) => mockGetStack(...args),
}))

describe('StackLayout page metadata', () => {
  beforeEach(() => {
    mockGetStack.mockReset()
    document.title = 'Previous stack | Stacklab'
  })

  it('uses the stack ID for the loading heading and route-specific title', () => {
    mockGetStack.mockReturnValue(new Promise(() => {}))

    renderStackRoute('/stacks/demo/logs')

    expect(screen.getByRole('heading', { level: 1, name: 'Stack demo' })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    expect(document.title).toBe('Logs — demo | Stacklab')
  })

  it('updates the title with the resolved stack name and ID', async () => {
    mockGetStack.mockResolvedValue(stackResponse('demo', 'Home media'))

    renderStackRoute('/stacks/demo')

    expect(await screen.findByRole('heading', { level: 1, name: 'Home media' })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    await waitFor(() => {
      expect(document.title).toBe('Overview — Home media (demo) | Stacklab')
    })
  })

  it('keeps the requested route title when loading the stack fails', async () => {
    mockGetStack.mockRejectedValue(new Error('backend unavailable'))

    renderStackRoute('/stacks/demo/stats')

    expect(await screen.findByText('Failed to load stack: backend unavailable')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1, name: 'Stack demo' })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    expect(document.title).toBe('Stats — demo | Stacklab')
  })

  it('drops the previous stack title immediately when the route starts loading another stack', async () => {
    mockGetStack.mockImplementation((stackID: string) => {
      if (stackID === 'alpha') return Promise.resolve(stackResponse('alpha', 'Alpha stack'))
      return new Promise(() => {})
    })

    renderStackRoute('/stacks/alpha')
    expect(await screen.findByRole('heading', { level: 1, name: 'Alpha stack' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Open beta' }))

    expect(await screen.findByRole('heading', { level: 1, name: 'Stack beta' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 1, name: 'Alpha stack' })).not.toBeInTheDocument()
    expect(document.title).toBe('Overview — beta | Stacklab')
  })
})

function NavigationProbe() {
  const navigate = useNavigate()
  return <button onClick={() => navigate('/stacks/beta')}>Open beta</button>
}

function renderStackRoute(initialEntry: string) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <PageMetadataProvider>
        <Routes>
          <Route path="/stacks/:stackId" element={<StackLayout />}>
            <Route index element={<NavigationProbe />} />
            <Route path="logs" element={<p>Log viewer</p>} />
            <Route path="stats" element={<p>Stats dashboard</p>} />
          </Route>
        </Routes>
      </PageMetadataProvider>
    </MemoryRouter>,
  )
}

function stackResponse(id: string, name: string): StackDetailResponse {
  return {
    stack: {
      id,
      name,
      root_path: `/srv/stacklab/stacks/${id}`,
      compose_file_path: `/srv/stacklab/stacks/${id}/compose.yaml`,
      env_file_path: null,
      config_path: `/srv/stacklab/config/${id}`,
      data_path: `/srv/stacklab/data/${id}`,
      display_state: 'running',
      runtime_state: 'running',
      config_state: 'in_sync',
      activity_state: 'idle',
      health_summary: {
        healthy_container_count: 1,
        unhealthy_container_count: 0,
        unknown_health_container_count: 0,
      },
      capabilities: {
        can_edit_definition: true,
        can_view_logs: true,
        can_view_stats: true,
        can_open_terminal: true,
      },
      available_actions: [],
      services: [],
      containers: [],
      last_deployed_at: null,
      last_action: null,
      updates: null,
    },
  }
}
