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
          volumes: { count: 0, reclaimable_bytes: 0 },
          total_reclaimable_bytes: 3072,
        },
      },
      error: null,
      loading: false,
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
})
