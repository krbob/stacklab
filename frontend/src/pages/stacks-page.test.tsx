import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { StacksPage } from './stacks-page'
import { WsProvider } from '@/contexts/ws-context'
import type { StackListItem, StackListResponse } from '@/lib/api-types'

const mockGetStacks = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getStacks: (...args: unknown[]) => mockGetStacks(...args),
}))

function makeStack(partial: Partial<StackListItem> & Pick<StackListItem, 'id'>): StackListItem {
  return {
    name: partial.id,
    created_at: '2026-07-09T08:00:00Z',
    updated_at: '2026-07-09T08:00:00Z',
    metadata: null,
    display_state: 'running',
    runtime_state: 'running',
    config_state: 'in_sync',
    activity_state: 'idle',
    health_summary: { healthy_container_count: 1, unhealthy_container_count: 0, unknown_health_container_count: 0 },
    service_count: { defined: 1, running: 1 },
    last_action: null,
    stats: null,
    updates: null,
    ...partial,
  }
}

const response: StackListResponse = {
  items: [
    makeStack({ id: 'adguardhome', stats: { cpu_percent: 0.4, memory_bytes: 93323264, sampled_at: '2026-07-04T14:00:00Z' } }),
    makeStack({
      id: 'transmission',
      display_state: 'error',
      runtime_state: 'error',
      metadata: { links: [{ label: 'Web UI', url: 'https://t.example.net' }] },
    }),
    makeStack({ id: 'jellyfin', config_state: 'drifted' }),
  ],
  summary: {
    stack_count: 3,
    defined_count: 0,
    running_count: 2,
    stopped_count: 0,
    error_count: 1,
    orphaned_count: 0,
    container_count: { running: 3, total: 3 },
  },
}

function renderPage() {
  mockGetStacks.mockResolvedValue(response)
  return render(
    <MemoryRouter>
      <WsProvider>
        <StacksPage />
      </WsProvider>
    </MemoryRouter>,
  )
}

describe('StacksPage', () => {
  it('renders tiles with stats, drift badge, and metadata links', async () => {
    renderPage()

    expect(screen.getByRole('heading', { level: 1, name: 'Stacks' })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    await waitFor(() => expect(screen.getByTestId('stack-card-adguardhome')).toBeInTheDocument())
    expect(screen.getByText('cpu 0.4%')).toBeInTheDocument()
    expect(screen.getByText('mem 89M')).toBeInTheDocument()
    expect(screen.getByText('drift')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Web UI' })).toHaveAttribute('href', 'https://t.example.net')
  })

  it('filters by name and by problems chip', async () => {
    renderPage()
    await waitFor(() => expect(screen.getByTestId('stack-card-jellyfin')).toBeInTheDocument())

    fireEvent.change(screen.getByTestId('stacks-filter'), { target: { value: 'adguard' } })
    expect(screen.queryByTestId('stack-card-jellyfin')).not.toBeInTheDocument()
    expect(screen.getByTestId('stack-card-adguardhome')).toBeInTheDocument()

    fireEvent.change(screen.getByTestId('stacks-filter'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: /Problems/ }))
    expect(screen.getByTestId('stack-card-transmission')).toBeInTheDocument()
    expect(screen.queryByTestId('stack-card-adguardhome')).not.toBeInTheDocument()
  })
})
