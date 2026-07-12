import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MaintenanceVolumes } from './maintenance-volumes'
import type { MaintenanceVolumesResponse } from '@/lib/api-types'
import { createMaintenanceVolume, deleteMaintenanceVolume } from '@/lib/api-client'

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
	      size_bytes: 2048,
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
	      size_bytes: 4096,
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
    vi.mocked(createMaintenanceVolume).mockReset()
    vi.mocked(deleteMaintenanceVolume).mockReset()
    vi.mocked(createMaintenanceVolume).mockResolvedValue({ created: true, name: 'new_volume' })
    vi.mocked(deleteMaintenanceVolume).mockResolvedValue({ deleted: true, name: 'external_media' })
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

  it('opens an accessible create dialog and restores trigger focus after Escape', () => {
    render(
      <MemoryRouter>
        <MaintenanceVolumes />
      </MemoryRouter>,
    )

    const trigger = screen.getByRole('button', { name: 'Create volume' })
    trigger.focus()
    fireEvent.click(trigger)

    expect(screen.getByRole('dialog', { name: 'Create volume' })).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Volume name')).toHaveFocus()

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(screen.queryByRole('dialog', { name: 'Create volume' })).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

  it('keeps the create dialog open while creation is pending', async () => {
    vi.mocked(createMaintenanceVolume).mockReturnValue(new Promise(() => {}))

    render(
      <MemoryRouter>
        <MaintenanceVolumes />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Create volume' }))
    const input = screen.getByPlaceholderText('Volume name')
    fireEvent.change(input, { target: { value: 'new_volume' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(createMaintenanceVolume).toHaveBeenCalledWith({ name: 'new_volume' }))
    const dialog = screen.getByRole('dialog', { name: 'Create volume' })
    expect(dialog).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.getByRole('dialog', { name: 'Create volume' })).toBeInTheDocument()
  })

  it('keeps a create failure visible in the dialog', async () => {
    vi.mocked(createMaintenanceVolume).mockRejectedValueOnce(new Error('Volume create unavailable'))

    render(
      <MemoryRouter>
        <MaintenanceVolumes />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Create volume' }))
    const input = screen.getByPlaceholderText('Volume name')
    fireEvent.change(input, { target: { value: 'new_volume' } })
    fireEvent.submit(input.closest('form')!)

    expect(await screen.findByRole('alert')).toHaveTextContent('Volume create unavailable')
    expect(screen.getByRole('dialog', { name: 'Create volume' })).toBeInTheDocument()
  })

  it('requires typed confirmation before removing an external volume', async () => {
    const refetch = vi.fn()
    mockUseApi.mockReturnValue({
      data: volumesData,
      error: null,
      loading: false,
      refetch,
    })

    render(
      <MemoryRouter>
        <MaintenanceVolumes />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Remove external_media' }))

    expect(screen.getByRole('dialog', { name: 'Remove volume "external_media"?' })).toBeInTheDocument()
    expect(deleteMaintenanceVolume).not.toHaveBeenCalled()

    const confirm = screen.getByRole('button', { name: 'Remove volume' })
    expect(confirm).toBeDisabled()

    fireEvent.change(screen.getByLabelText('Type external_media to confirm'), { target: { value: 'external_media' } })
    fireEvent.click(confirm)

    await waitFor(() => expect(deleteMaintenanceVolume).toHaveBeenCalledWith('external_media'))
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
        <MaintenanceVolumes />
      </MemoryRouter>,
    )

    expect(screen.getByText(/No volumes found/)).toBeInTheDocument()
  })

  it('shows an initial error with Retry and no false empty summary', () => {
    const refetch = vi.fn()
    mockUseApi.mockReturnValue({
      data: null,
      error: new Error('Volume inventory unavailable'),
      loading: false,
      refetch,
    })

    render(
      <MemoryRouter>
        <MaintenanceVolumes />
      </MemoryRouter>,
    )

    expect(screen.getByRole('alert')).toHaveTextContent('Volume inventory unavailable')
    expect(screen.queryByText(/0 volumes/)).not.toBeInTheDocument()
    expect(screen.queryByText(/No volumes found/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(refetch).toHaveBeenCalledTimes(1)
  })
})
