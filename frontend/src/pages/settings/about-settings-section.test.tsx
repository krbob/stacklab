import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getMeta } from '@/lib/api-client'
import { AboutSettingsSection } from './about-settings-section'

vi.mock('@/lib/api-client', () => ({
  getMeta: vi.fn(),
}))

const mockGetMeta = vi.mocked(getMeta)
const metaResponse = {
  app: { name: 'Stacklab', version: '0.1.0-dev' },
  environment: { stack_root: '/opt/stacklab', platform: 'linux/amd64' },
  docker: { engine_version: '29.3.1', compose_version: '5.1.1' },
  features: { host_shell: false },
}

describe('AboutSettingsSection', () => {
  beforeEach(() => {
    mockGetMeta.mockReset()
  })

  it('keeps the About card visible while system information is loading', () => {
    mockGetMeta.mockReturnValue(new Promise(() => {}))

    render(<AboutSettingsSection />)

    expect(screen.getByText('About')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Loading system information.')
    expect(screen.getByText('About').closest('section')?.querySelector('[aria-busy]')).toHaveAttribute('aria-busy', 'true')
  })

  it('retries a failed system information request inside the About card', async () => {
    mockGetMeta
      .mockRejectedValueOnce(new Error('metadata unavailable'))
      .mockResolvedValueOnce(metaResponse)

    render(<AboutSettingsSection />)

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Failed to load system information: metadata unavailable',
    )
    fireEvent.click(screen.getByRole('button', { name: 'Retry system information' }))

    expect(await screen.findByText('Stacklab 0.1.0-dev')).toBeInTheDocument()
    expect(screen.getByText('Docker Engine 29.3.1')).toBeInTheDocument()
    expect(screen.getByText('Docker Compose 5.1.1')).toBeInTheDocument()
    expect(mockGetMeta).toHaveBeenCalledTimes(2)
  })
})
