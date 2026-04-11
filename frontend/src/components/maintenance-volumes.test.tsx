import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MaintenanceVolumes } from './maintenance-volumes'
import type { MaintenanceVolumesResponse } from '@/lib/api-types'

const mockUseApi = vi.fn()

vi.mock('@/hooks/use-api', () => ({
  useApi: (...args: unknown[]) => mockUseApi(...args),
}))

vi.mock('@/lib/api-client', () => ({
  getMaintenanceVolumes: vi.fn(),
  createMaintenanceVolume: vi.fn(),
  deleteMaintenanceVolume: vi.fn(),
}))

const volumesData: MaintenanceVolumesResponse = {
  items: [
    {
      name: 'demo_data',
      driver: 'local',
      mountpoint: '/var/lib/docker/volumes/demo_data/_data',
      scope: 'local',
      options_count: 0,
      containers_using: 1,
      stacks_using: [{ stack_id: 'demo', service_names: ['app'] }],
      is_unused: false,
      source: 'stack_managed',
    },
    {
      name: 'external_media',
      driver: 'local',
      mountpoint: '/var/lib/docker/volumes/external_media/_data',
      scope: 'local',
      options_count: 1,
      containers_using: 0,
      stacks_using: [],
      is_unused: true,
      source: 'external',
    },
  ],
}

describe('MaintenanceVolumes', () => {
  beforeEach(() => {
    mockUseApi.mockReset()
    mockUseApi.mockReturnValue({
      data: volumesData,
      error: null,
      loading: false,
      refetch: vi.fn(),
    })
  })

  it('renders volume list with summary, mountpoint, and stack links', () => {
    render(
      <MemoryRouter>
        <MaintenanceVolumes />
      </MemoryRouter>,
    )

    expect(screen.getByText('Volumes')).toBeInTheDocument()
    expect(screen.getByText(/2 volumes/)).toBeInTheDocument()
    expect(screen.getByText(/1 unused/)).toBeInTheDocument()
    expect(screen.getByText('/var/lib/docker/volumes/demo_data/_data')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'demo' })).toHaveAttribute('href', '/stacks/demo')
  })

  it('shows external badge', () => {
    render(
      <MemoryRouter>
        <MaintenanceVolumes />
      </MemoryRouter>,
    )

    expect(screen.getAllByText('external').length).toBeGreaterThanOrEqual(1)
  })

  it('disables remove for managed volumes and enables it for unused external volumes', () => {
    render(
      <MemoryRouter>
        <MaintenanceVolumes />
      </MemoryRouter>,
    )

    expect(screen.getByRole('button', { name: 'Remove demo_data' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Remove external_media' })).toBeEnabled()
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
        <MaintenanceVolumes />
      </MemoryRouter>,
    )

    expect(screen.getByText(/No volumes found/)).toBeInTheDocument()
  })
})
