import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { StacksPage } from './stacks-page'
import type { StackListItem, StackListResponse } from '@/lib/api-types'

const mockGetStacks = vi.fn()
const mockGetStackStats = vi.fn()
const mockCheckImageUpdates = vi.fn()
const mockUpdateStacksMaintenance = vi.fn()
const mockUseJobStream = vi.fn()
const mockOpenJob = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getStacks: (...args: unknown[]) => mockGetStacks(...args),
  getStackStats: (...args: unknown[]) => mockGetStackStats(...args),
  checkImageUpdates: (...args: unknown[]) => mockCheckImageUpdates(...args),
  updateStacksMaintenance: (...args: unknown[]) => mockUpdateStacksMaintenance(...args),
}))

vi.mock('@/hooks/use-job-stream', () => ({
  useJobStream: (...args: unknown[]) => mockUseJobStream(...args),
}))

vi.mock('@/hooks/use-job-drawer', () => ({
  useJobDrawer: () => ({ openJob: mockOpenJob }),
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
    mockGetStackStats.mockReset().mockResolvedValue({
      items: Object.fromEntries(response.items.filter((stack) => stack.stats).map((stack) => [stack.id, stack.stats])),
    })
    mockCheckImageUpdates.mockReset()
    mockUpdateStacksMaintenance.mockReset().mockResolvedValue({ job: { id: 'update-job' } })
    mockUseJobStream.mockReset().mockReturnValue({ state: null, events: [] })
    mockOpenJob.mockReset()
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

  it('sorts by numeric CPU and RAM usage, with ties by name and missing samples last', async () => {
    const items = ['missing', 'zero', 'beta', 'alpha', 'memory'].map((id) => makeStack({ id }))
    mockGetStacks.mockResolvedValueOnce({ ...response, items })
    mockGetStackStats.mockResolvedValue({
      items: {
        zero: { cpu_percent: 0, memory_bytes: 0, sampled_at: '2026-09-09T10:00:00Z' },
        beta: { cpu_percent: 12, memory_bytes: 64 << 20, sampled_at: '2026-09-09T10:00:00Z' },
        alpha: { cpu_percent: 12, memory_bytes: 256 << 20, sampled_at: '2026-09-09T10:00:00Z' },
        memory: { cpu_percent: 9, memory_bytes: 1 << 30, sampled_at: '2026-09-09T10:00:00Z' },
      },
    })
    renderPage()
    await screen.findByText('cpu 9.0%')
    const order = () => screen.getAllByRole('heading', { level: 2 }).map((heading) => heading.textContent)
    expect(order()).toEqual(['alpha', 'beta', 'memory', 'missing', 'zero'])

    fireEvent.change(screen.getByRole('combobox', { name: 'Sort by' }), { target: { value: 'cpu' } })
    expect(order()).toEqual(['alpha', 'beta', 'memory', 'zero', 'missing'])
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort by' }), { target: { value: 'memory' } })
    expect(order()).toEqual(['memory', 'alpha', 'beta', 'zero', 'missing'])
    fireEvent.change(screen.getByTestId('stacks-filter'), { target: { value: 'a' } })
    expect(order()).toEqual(['alpha', 'beta'])
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort by' }), { target: { value: 'name' } })
    fireEvent.change(screen.getByTestId('stacks-filter'), { target: { value: '' } })
    expect(order()).toEqual(['alpha', 'beta', 'memory', 'missing', 'zero'])
  })

  it('updates usage and ranking every second without reloading inventory or announcing loading', async () => {
    vi.useFakeTimers()
    renderPage()
    await act(async () => { await Promise.resolve() })
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort by' }), { target: { value: 'cpu' } })
    mockGetStackStats.mockResolvedValueOnce({
      items: {
        jellyfin: { cpu_percent: 80, memory_bytes: 512 << 20, sampled_at: '2026-09-09T10:00:01Z' },
      },
    })

    await act(async () => { await vi.advanceTimersByTimeAsync(1_000) })

    expect(mockGetStacks).toHaveBeenCalledTimes(1)
    expect(mockGetStackStats).toHaveBeenCalledTimes(2)
    expect(screen.getAllByRole('heading', { level: 2 })[0]).toHaveTextContent('jellyfin')
    expect(screen.getByRole('combobox', { name: 'Sort by' })).toHaveValue('cpu')
    expect(screen.getByText('cpu 80.0%')).toBeInTheDocument()
    expect(screen.queryByText('cpu 0.4%')).not.toBeInTheDocument()
    expect(screen.queryByText('Refreshing…')).not.toBeInTheDocument()
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

  it('hides Update all when no image updates are available', async () => {
    renderPage()
    await screen.findByTestId('stack-card-adguardhome')
    expect(screen.queryByRole('button', { name: /Update all/ })).not.toBeInTheDocument()
  })

  function withAvailableUpdates(): StackListResponse {
    return {
      ...response,
      items: response.items.map((stack) => stack.id === 'transmission' ? stack : {
        ...stack,
        updates: { state: 'available', services_with_updates: 1, checked_at: '2026-09-09T10:00:00Z' },
      }),
    }
  }

  it('reviews all available updates regardless of the dashboard filter and preserves inactive stacks', async () => {
    const updates = withAvailableUpdates()
    updates.items[0] = { ...updates.items[0], display_state: 'stopped', runtime_state: 'stopped' }
    mockGetStacks.mockResolvedValue(updates)
    renderPage()
    await screen.findByTestId('stack-card-adguardhome')
    fireEvent.change(screen.getByTestId('stacks-filter'), { target: { value: 'jelly' } })
    fireEvent.click(screen.getByRole('button', { name: 'Update all (2)' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('adguardhome')).toBeInTheDocument()
    expect(within(dialog).getByText('jellyfin')).toBeInTheDocument()
    expect(within(dialog).queryByText('transmission')).not.toBeInTheDocument()
    expect(within(dialog).getByText(/stopped stacks will stay stopped/)).toBeInTheDocument()
    expect(mockUpdateStacksMaintenance).not.toHaveBeenCalled()

    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(mockUpdateStacksMaintenance).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Update all (2)' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Start updates' }))

    await waitFor(() => expect(mockOpenJob).toHaveBeenCalledWith('update-job'))
    expect(mockUpdateStacksMaintenance).toHaveBeenCalledExactlyOnceWith({
      target: { mode: 'selected', stack_ids: ['adguardhome', 'jellyfin'] },
      options: {
        pull_images: true,
        build_images: false,
        remove_orphans: false,
        preserve_inactive: true,
        prune_after: { enabled: false, include_volumes: false },
      },
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Updating…' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Check updates' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'View update details' }))
    expect(mockOpenJob).toHaveBeenCalledTimes(2)
  })

  it('keeps the reviewed target fixed while the inventory refreshes', async () => {
    vi.useFakeTimers()
    mockGetStacks.mockResolvedValueOnce(withAvailableUpdates())
    renderPage()
    await act(async () => { await Promise.resolve() })
    fireEvent.click(screen.getByRole('button', { name: 'Update all (2)' }))
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(within(screen.getByRole('dialog')).getByText('adguardhome')).toBeInTheDocument()
    await act(async () => {
      fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Start updates' }))
    })
    expect(mockUpdateStacksMaintenance.mock.calls[0][0].target.stack_ids).toEqual(['adguardhome', 'jellyfin'])
  })

  it('keeps the review open on start failure and supports retry', async () => {
    mockGetStacks.mockResolvedValue(withAvailableUpdates())
    mockUpdateStacksMaintenance.mockRejectedValueOnce(new Error('stack locked'))
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Update all (2)' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Start updates' }))
    expect(await screen.findByText(/Could not start updates: stack locked/)).toBeInTheDocument()
    expect(mockOpenJob).not.toHaveBeenCalled()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Start updates' }))
    await waitFor(() => expect(mockOpenJob).toHaveBeenCalledWith('update-job'))
  })

  it.each(['succeeded', 'failed', 'cancelled', 'timed_out'])('refreshes update badges and reports a %s job', async (state) => {
    mockGetStacks.mockResolvedValueOnce(withAvailableUpdates())
    let jobState = 'running'
    mockUseJobStream.mockImplementation(({ jobId }: { jobId: string | null }) => ({
      state: jobId === 'update-job' ? jobState : null,
      events: [],
    }))
    const view = renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Update all (2)' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Start updates' }))
    await screen.findByText('Updating stacks…')
    jobState = state
    view.rerender(<MemoryRouter><StacksPage /></MemoryRouter>)
    await waitFor(() => expect(mockGetStacks).toHaveBeenCalledTimes(2))
    expect(screen.queryByRole('button', { name: /Update all/ })).not.toBeInTheDocument()
    expect(screen.getByText(state === 'succeeded'
      ? 'Updates completed.'
      : 'Updates ' + state.replace('_', ' ') + '. Open details to review the result.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Check updates' })).toBeEnabled()
  })
})
