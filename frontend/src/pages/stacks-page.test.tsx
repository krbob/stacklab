import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { StacksPage } from './stacks-page'
import type { StackListItem, StackListResponse } from '@/lib/api-types'

const mockGetStacks = vi.fn()
const mockCheckImageUpdates = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getStacks: (...args: unknown[]) => mockGetStacks(...args),
  checkImageUpdates: (...args: unknown[]) => mockCheckImageUpdates(...args),
}))

vi.mock('@/hooks/use-job-stream', () => ({
  useJobStream: () => ({ state: null, events: [] }),
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
  return render(
    <MemoryRouter>
      <StacksPage />
    </MemoryRouter>,
  )
}

describe('StacksPage', () => {
  beforeEach(() => {
    mockGetStacks.mockReset().mockResolvedValue(response)
    mockCheckImageUpdates.mockReset()
  })

  afterEach(() => vi.useRealTimers())

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

  it('gives the full stack name its own wrapping row when status badges are present', async () => {
    const name = 'wireguard-remote-access-europe'
    mockGetStacks.mockResolvedValueOnce({
      ...response,
      items: [makeStack({
        id: name,
        name,
        updates: {
          state: 'available',
          services_with_updates: 1,
          checked_at: '2026-07-09T03:00:00Z',
        },
      })],
    })

    renderPage()

    const card = await screen.findByTestId(`stack-card-${name}`)
    const heading = within(card).getByRole('heading', { level: 2, name })
    expect(heading).toHaveClass('[overflow-wrap:anywhere]')
    expect(heading).not.toHaveClass('truncate')
    expect(heading.parentElement).not.toHaveTextContent('update')
    expect(within(card).getByText('update')).toBeInTheDocument()
    expect(within(card).getByText('Running')).toBeInTheDocument()
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

  it('shows an initial error without false counts and recovers on Retry', async () => {
    mockGetStacks
      .mockReset()
      .mockRejectedValueOnce(new Error('backend unavailable'))
      .mockResolvedValueOnce(response)

    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to load stacks: backend unavailable')
    expect(screen.getByRole('button', { name: 'Problems' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Updates' })).toBeInTheDocument()
    expect(screen.queryByText('No stacks found')).not.toBeInTheDocument()
    expect(screen.queryByText('3 stacks')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))

    expect(await screen.findByTestId('stack-card-adguardhome')).toBeInTheDocument()
    expect(mockGetStacks).toHaveBeenCalledTimes(2)
  })

  it('keeps the last stack cards visible when a background poll fails', async () => {
    vi.useFakeTimers()
    renderPage()

    await act(async () => {
      await Promise.resolve()
    })
    expect(screen.getByTestId('stack-card-adguardhome')).toBeInTheDocument()

    mockGetStacks.mockRejectedValueOnce(new Error('poll unavailable'))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000)
    })

    expect(screen.getByRole('alert')).toHaveTextContent('Failed to load stacks: poll unavailable')
    expect(screen.getByText('Showing the last successfully loaded data.')).toBeInTheDocument()
    expect(screen.getByTestId('stack-card-adguardhome')).toBeInTheDocument()
    expect(screen.getByText('3 stacks')).toBeInTheDocument()
  })

  it('shows a useful empty state after a successful response', async () => {
    mockGetStacks.mockResolvedValueOnce({
      items: [],
      summary: {
        stack_count: 0,
        defined_count: 0,
        running_count: 0,
        stopped_count: 0,
        error_count: 0,
        orphaned_count: 0,
        container_count: { running: 0, total: 0 },
      },
    })

    renderPage()

    expect(await screen.findByText('No stacks found')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Create your first stack' })).toHaveAttribute('href', '/stacks/new')
  })

  it('surfaces image-update start failures with a retry action', async () => {
    mockCheckImageUpdates.mockRejectedValueOnce(new Error('Docker unavailable'))
    renderPage()
    await screen.findByTestId('stack-card-adguardhome')

    fireEvent.click(screen.getByRole('button', { name: 'Check updates' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to check image updates: Docker unavailable')
    expect(screen.getByRole('button', { name: 'Retry check' })).toBeInTheDocument()
  })
})
