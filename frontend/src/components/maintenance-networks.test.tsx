import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MaintenanceNetworks } from './maintenance-networks'
import type { MaintenanceNetworksResponse } from '@/lib/api-types'
import { createMaintenanceNetwork, deleteMaintenanceNetwork } from '@/lib/api-client'

const mockUseApi = vi.fn()

vi.mock('@/hooks/use-api', () => ({
  useApi: (...args: unknown[]) => mockUseApi(...args),
}))

vi.mock('@/lib/api-client', () => ({
  getMaintenanceNetworks: vi.fn(),
  createMaintenanceNetwork: vi.fn(),
  deleteMaintenanceNetwork: vi.fn(),
}))

const networksData: MaintenanceNetworksResponse = {
  items: [
    {
      id: 'network-demo-123456',
      name: 'demo_default',
      driver: 'bridge',
      scope: 'local',
      internal: false,
      attachable: false,
      ingress: false,
      containers_using: 2,
      stacks_using: [{ stack_id: 'demo', service_names: ['app'] }],
      is_unused: false,
      source: 'stack_managed',
    },
    {
      id: 'network-ext-987654',
      name: 'external_shared',
      driver: 'bridge',
      scope: 'local',
      internal: true,
      attachable: false,
      ingress: false,
      containers_using: 0,
      stacks_using: [],
      is_unused: true,
      source: 'external',
    },
  ],
}

describe('MaintenanceNetworks', () => {
  beforeEach(() => {
    mockUseApi.mockReset()
    vi.mocked(createMaintenanceNetwork).mockReset()
    vi.mocked(deleteMaintenanceNetwork).mockReset()
    vi.mocked(createMaintenanceNetwork).mockResolvedValue({ created: true, name: 'new_network' })
    vi.mocked(deleteMaintenanceNetwork).mockResolvedValue({ deleted: true, name: 'external_shared' })
    mockUseApi.mockReturnValue({
      data: networksData,
      error: null,
      loading: false,
      refetch: vi.fn(),
    })
  })

  it('renders network list with summary and stack links', () => {
    render(
      <MemoryRouter>
        <MaintenanceNetworks />
      </MemoryRouter>,
    )

    expect(screen.getByText('Networks')).toBeInTheDocument()
    expect(screen.getByText(/2 networks/)).toBeInTheDocument()
    expect(screen.getByText(/1 unused/)).toBeInTheDocument()
    expect(screen.getByText('demo_default')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'demo' })).toHaveAttribute('href', '/stacks/demo')
  })

  it('shows external and internal badges', () => {
    render(
      <MemoryRouter>
        <MaintenanceNetworks />
      </MemoryRouter>,
    )

    expect(screen.getAllByText('external').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('internal')).toBeInTheDocument()
  })

  it('disables remove for managed networks and enables it for unused external networks', () => {
    render(
      <MemoryRouter>
        <MaintenanceNetworks />
      </MemoryRouter>,
    )

    expect(screen.getByRole('button', { name: 'Remove demo_default' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Remove external_shared' })).toBeEnabled()
  })

  it('opens an accessible create dialog and restores trigger focus after Escape', () => {
    render(
      <MemoryRouter>
        <MaintenanceNetworks />
      </MemoryRouter>,
    )

    const trigger = screen.getByRole('button', { name: 'Create network' })
    trigger.focus()
    fireEvent.click(trigger)

    expect(screen.getByRole('dialog', { name: 'Create network' })).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Network name')).toHaveFocus()

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(screen.queryByRole('dialog', { name: 'Create network' })).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

  it('keeps the create dialog open while creation is pending', async () => {
    vi.mocked(createMaintenanceNetwork).mockReturnValue(new Promise(() => {}))

    render(
      <MemoryRouter>
        <MaintenanceNetworks />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Create network' }))
    const input = screen.getByPlaceholderText('Network name')
    fireEvent.change(input, { target: { value: 'new_network' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(createMaintenanceNetwork).toHaveBeenCalledWith({ name: 'new_network' }))
    const dialog = screen.getByRole('dialog', { name: 'Create network' })
    expect(dialog).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.getByRole('dialog', { name: 'Create network' })).toBeInTheDocument()
  })

  it('keeps a create failure visible in the dialog', async () => {
    vi.mocked(createMaintenanceNetwork).mockRejectedValueOnce(new Error('Network create unavailable'))

    render(
      <MemoryRouter>
        <MaintenanceNetworks />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Create network' }))
    const input = screen.getByPlaceholderText('Network name')
    fireEvent.change(input, { target: { value: 'new_network' } })
    fireEvent.submit(input.closest('form')!)

    expect(await screen.findByRole('alert')).toHaveTextContent('Network create unavailable')
    expect(screen.getByRole('dialog', { name: 'Create network' })).toBeInTheDocument()
  })

  it('requires confirmation before removing an external network', async () => {
    const refetch = vi.fn()
    mockUseApi.mockReturnValue({
      data: networksData,
      error: null,
      loading: false,
      refetch,
    })

    render(
      <MemoryRouter>
        <MaintenanceNetworks />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Remove external_shared' }))

    const dialog = screen.getByRole('dialog', { name: 'Remove network "external_shared"?' })
    const review = within(dialog).getByRole('region', { name: 'Review operation' })
    expect(review).toHaveTextContent('external_shared (network-ext-)')
    expect(review).toHaveTextContent('Network scope: local; internal: yes.')
    expect(review).toHaveTextContent('Future deployments that reference this external network can fail')
    expect(review).toHaveTextContent('No automatic export')
    expect(review).toHaveTextContent('Recreate the network manually')
    expect(deleteMaintenanceNetwork).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Remove network' }))

    await waitFor(() => expect(deleteMaintenanceNetwork).toHaveBeenCalledWith('external_shared'))
    expect(refetch).toHaveBeenCalled()
  })

  it('shows empty state', () => {
    mockUseApi.mockReturnValue({
      data: { items: [] },
      error: null,
      loading: false,
      refetch: vi.fn(),
    })

    render(
      <MemoryRouter>
        <MaintenanceNetworks />
      </MemoryRouter>,
    )

    expect(screen.getByText(/No networks found/)).toBeInTheDocument()
  })

  it('shows an initial error with Retry and no false empty summary', () => {
    const refetch = vi.fn()
    mockUseApi.mockReturnValue({
      data: null,
      error: new Error('Network inventory unavailable'),
      loading: false,
      refetch,
    })

    render(
      <MemoryRouter>
        <MaintenanceNetworks />
      </MemoryRouter>,
    )

    expect(screen.getByRole('alert')).toHaveTextContent('Network inventory unavailable')
    expect(screen.queryByText(/0 networks/)).not.toBeInTheDocument()
    expect(screen.queryByText(/No networks found/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(refetch).toHaveBeenCalledTimes(1)
  })
})
