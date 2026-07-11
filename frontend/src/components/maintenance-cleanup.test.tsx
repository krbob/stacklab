import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MaintenanceCleanup } from './maintenance-cleanup'

const mockGetMaintenancePrunePreview = vi.fn()
const mockRunMaintenancePrune = vi.fn()
const mockUseApi = vi.fn()
const mockUseJobStream = vi.fn()
const mockRefetch = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getMaintenancePrunePreview: (...args: unknown[]) => mockGetMaintenancePrunePreview(...args),
  runMaintenancePrune: (...args: unknown[]) => mockRunMaintenancePrune(...args),
}))

vi.mock('@/hooks/use-api', () => ({
  useApi: (...args: unknown[]) => mockUseApi(...args),
}))

vi.mock('@/hooks/use-job-stream', () => ({
  useJobStream: (...args: unknown[]) => mockUseJobStream(...args),
}))

describe('MaintenanceCleanup', () => {
  beforeEach(() => {
    mockGetMaintenancePrunePreview.mockReset()
    mockRunMaintenancePrune.mockReset()
    mockUseApi.mockReset()
    mockUseJobStream.mockReset()
    mockRefetch.mockReset()

    mockUseApi.mockReturnValue({
      data: {
        preview: {
          images: { count: 2, reclaimable_bytes: 1024 },
          build_cache: { count: 1, reclaimable_bytes: 2048 },
          stopped_containers: { count: 0, reclaimable_bytes: 0 },
          volumes: { count: 1, reclaimable_bytes: 0, items: [{ reference: 'external_media', size_bytes: 0, reason: 'unused_external_volume' }] },
          total_reclaimable_bytes: 3072,
        },
      },
      error: null,
      loading: false,
      updatedAt: 1,
      refetch: mockRefetch,
    })

    mockUseJobStream.mockImplementation(({ jobId }: { jobId: string | null }) => (
      jobId
        ? {
            events: [],
            state: 'succeeded',
            clear: vi.fn(),
          }
        : {
            events: [],
            state: null,
            clear: vi.fn(),
          }
    ))
  })

  it('refreshes prune preview once after a successful cleanup job', async () => {
    mockRunMaintenancePrune.mockResolvedValue({
      job: { id: 'job_prune_done', stack_id: null, action: 'prune', state: 'running' },
    })

    render(<MaintenanceCleanup />)

    fireEvent.click(screen.getByTestId('maintenance-prune'))

    await waitFor(() => {
      expect(mockRunMaintenancePrune).toHaveBeenCalledWith({
        scope: {
          images: true,
          build_cache: true,
          stopped_containers: false,
          volumes: false,
        },
      })
    })

    await waitFor(() => {
      expect(mockRefetch).toHaveBeenCalledTimes(1)
    })
  })

  it('shows prune preview with category counts', () => {
    render(<MaintenanceCleanup />)
    expect(screen.getByText(/Total reclaimable/)).toBeInTheDocument()
    expect(screen.getByText('Unused images')).toBeInTheDocument()
    expect(screen.getByText('Build cache')).toBeInTheDocument()
    expect(screen.getByText('Stopped containers')).toBeInTheDocument()
    expect(screen.getByText('Unused volumes')).toBeInTheDocument()
  })

  it('shows volume names when volume cleanup is selected', () => {
    render(<MaintenanceCleanup />)

    fireEvent.click(screen.getAllByRole('checkbox')[3])

    expect(screen.getByText('Volumes selected for removal')).toBeInTheDocument()
    expect(screen.getByText('external_media')).toBeInTheDocument()
  })

  it('disables prune button when nothing is selected', () => {
    render(<MaintenanceCleanup />)

    // Uncheck defaults: images (index 0) and build_cache (index 1)
    const checkboxes = screen.getAllByRole('checkbox')
    fireEvent.click(checkboxes[0])
    fireEvent.click(checkboxes[1])

    expect(screen.getByTestId('maintenance-prune')).toBeDisabled()
  })

  it('shows error when prune fails', async () => {
    mockRunMaintenancePrune.mockRejectedValue(new Error('Prune failed'))

    render(<MaintenanceCleanup />)
    fireEvent.click(screen.getByTestId('maintenance-prune'))

    expect(await screen.findByText('Prune failed')).toBeInTheDocument()
  })

  it('sends correct scope when volumes checkbox is checked', async () => {
    mockRunMaintenancePrune.mockResolvedValue({
      job: { id: 'job_vol', stack_id: null, action: 'prune', state: 'running' },
    })

    render(<MaintenanceCleanup />)

    // stopped_containers (index 2), volumes (index 3)
    const checkboxes = screen.getAllByRole('checkbox')
    fireEvent.click(checkboxes[2])
    fireEvent.click(checkboxes[3])
    fireEvent.click(screen.getByTestId('maintenance-prune'))

    expect(mockRunMaintenancePrune).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog', { name: 'Run cleanup with volume removal?' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm cleanup' }))

    await waitFor(() => {
      expect(mockRunMaintenancePrune).toHaveBeenCalledWith({
        scope: {
          images: true,
          build_cache: true,
          stopped_containers: true,
          volumes: true,
        },
      })
    })
  })

  it.each([
    ['loading', { data: null, error: null, loading: true, updatedAt: null }],
    ['failed', { data: null, error: new Error('Docker is unavailable'), loading: false, updatedAt: null }],
    ['missing', { data: null, error: null, loading: false, updatedAt: null }],
  ])('does not allow volume cleanup when preview is %s', (_state, apiState) => {
    mockUseApi.mockReturnValue({ ...apiState, refetch: mockRefetch })

    render(<MaintenanceCleanup />)

    fireEvent.click(screen.getAllByRole('checkbox')[3])
    const runCleanup = screen.getByTestId('maintenance-prune')
    expect(runCleanup).toBeDisabled()
    fireEvent.click(runCleanup)

    expect(screen.queryByRole('dialog', { name: 'Run cleanup with volume removal?' })).not.toBeInTheDocument()
    expect(mockRunMaintenancePrune).not.toHaveBeenCalled()
  })

  it('shows a preview error and retries it without allowing cleanup', () => {
    mockUseApi.mockReturnValue({
      data: {
        preview: {
          images: { count: 2, reclaimable_bytes: 1024 },
          build_cache: { count: 1, reclaimable_bytes: 2048 },
          stopped_containers: { count: 0, reclaimable_bytes: 0 },
          volumes: { count: 1, reclaimable_bytes: 0, items: [{ reference: 'external_media', size_bytes: 0, reason: 'unused_external_volume' }] },
          total_reclaimable_bytes: 3072,
        },
      },
      error: new Error('Docker is unavailable'),
      loading: false,
      updatedAt: 1,
      refetch: mockRefetch,
    })

    render(<MaintenanceCleanup />)

    expect(screen.getByRole('alert')).toHaveTextContent('Preview failed: Docker is unavailable')
    expect(screen.getByTestId('maintenance-prune')).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Retry preview' }))
    expect(mockRefetch).toHaveBeenCalledTimes(1)
  })
})
