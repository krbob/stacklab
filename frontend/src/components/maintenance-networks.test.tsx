import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MaintenanceNetworks } from './maintenance-networks'
import type { MaintenanceNetworksResponse } from '@/lib/api-types'

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
})
