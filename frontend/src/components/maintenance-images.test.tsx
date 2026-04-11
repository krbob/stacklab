import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MaintenanceImages } from './maintenance-images'
import type { MaintenanceImagesResponse } from '@/lib/api-types'

const mockUseApi = vi.fn()

vi.mock('@/hooks/use-api', () => ({
  useApi: (...args: unknown[]) => mockUseApi(...args),
}))

vi.mock('@/lib/api-client', () => ({
  getMaintenanceImages: vi.fn(),
}))

const imagesData: MaintenanceImagesResponse = {
  items: [
    {
      id: 'sha256:aaa111',
      repository: 'nginx',
      tag: 'alpine',
      reference: 'nginx:alpine',
      size_bytes: 50 * 1024 * 1024,
      created_at: '2026-04-01T00:00:00Z',
      containers_using: 2,
      stacks_using: [{ stack_id: 'demo', service_names: ['web'] }],
      is_dangling: false,
      is_unused: false,
      source: 'stack_managed',
    },
    {
      id: 'sha256:bbb222',
      repository: '<none>',
      tag: '<none>',
      reference: '',
      size_bytes: 10 * 1024 * 1024,
      created_at: '2026-03-15T00:00:00Z',
      containers_using: 0,
      stacks_using: [],
      is_dangling: true,
      is_unused: true,
      source: 'external',
    },
  ],
}

describe('MaintenanceImages', () => {
  const renderComponent = () =>
    render(
      <MemoryRouter>
        <MaintenanceImages />
      </MemoryRouter>,
    )

  beforeEach(() => {
    mockUseApi.mockReset()
    mockUseApi.mockReturnValue({
      data: imagesData,
      error: null,
      loading: false,
      refetch: vi.fn(),
    })
  })

  it('renders image list with summary', () => {
    renderComponent()
    expect(screen.getByText('Images')).toBeInTheDocument()
    expect(screen.getByText(/2 images/)).toBeInTheDocument()
    expect(screen.getByText(/1 unused/)).toBeInTheDocument()
    expect(screen.getByText('nginx:alpine')).toBeInTheDocument()
  })

  it('shows dangling and unused badges', () => {
    renderComponent()
    expect(screen.getByText('dangling')).toBeInTheDocument()
    // "unused" appears as badge and in summary
    expect(screen.getAllByText(/unused/).length).toBeGreaterThanOrEqual(2)
  })

  it('shows stack links for managed images', () => {
    renderComponent()
    expect(screen.getByRole('link', { name: 'demo' })).toHaveAttribute('href', '/stacks/demo')
  })

  it('shows container count', () => {
    renderComponent()
    expect(screen.getByText('2 containers')).toBeInTheDocument()
    expect(screen.getByText('0 containers')).toBeInTheDocument()
  })

  it('shows empty state when no images match', () => {
    mockUseApi.mockReturnValue({
      data: { items: [] },
      error: null,
      loading: false,
      refetch: vi.fn(),
    })
    renderComponent()
    expect(screen.getByText(/No images found/)).toBeInTheDocument()
  })

  it('shows error state', () => {
    mockUseApi.mockReturnValue({
      data: null,
      error: new Error('Docker unavailable'),
      loading: false,
      refetch: vi.fn(),
    })
    renderComponent()
    expect(screen.getByText('Docker unavailable')).toBeInTheDocument()
  })

  it('renders filter buttons', () => {
    renderComponent()
    expect(screen.getByRole('button', { name: 'used' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'unused' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'stack managed' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'external' })).toBeInTheDocument()
  })
})
