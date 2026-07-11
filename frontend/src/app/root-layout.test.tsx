import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { RootLayout } from './root-layout'
import { useAuth } from '@/hooks/use-auth'

const mockGetActiveJobs = vi.fn()
const mockGetMeta = vi.fn()
const mockGetStacks = vi.fn()
const mockGetJob = vi.fn()
const mockGetJobEvents = vi.fn()
const mockCancelJob = vi.fn()

vi.mock('@/hooks/use-auth', () => ({
  useAuth: vi.fn(),
}))

vi.mock('@/lib/api-client', () => ({
  cancelJob: (...args: unknown[]) => mockCancelJob(...args),
  getActiveJobs: (...args: unknown[]) => mockGetActiveJobs(...args),
  getJob: (...args: unknown[]) => mockGetJob(...args),
  getJobEvents: (...args: unknown[]) => mockGetJobEvents(...args),
  getMeta: (...args: unknown[]) => mockGetMeta(...args),
  getStacks: (...args: unknown[]) => mockGetStacks(...args),
}))

const mockUseAuth = vi.mocked(useAuth)

function LocationProbe({ modal = false }: { modal?: boolean }) {
  const location = useLocation()
  return (
    <div>
      <span data-testid="path">{location.pathname}</span>
      {modal && (
        <div role="dialog" aria-modal="true" aria-label="Confirm action">
          Confirm action
        </div>
      )}
    </div>
  )
}

function renderRoot({ modal = false } = {}) {
  return render(
    <MemoryRouter initialEntries={['/stacks']}>
      <Routes>
        <Route path="/" element={<RootLayout />}>
          <Route path="stacks" element={<LocationProbe modal={modal} />} />
          <Route path="host" element={<LocationProbe />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

describe('RootLayout keyboard navigation', () => {
  beforeEach(() => {
    Object.defineProperty(HTMLElement.prototype, 'scrollTo', {
      configurable: true,
      value: vi.fn(),
    })
    Object.defineProperty(window, 'scrollTo', {
      configurable: true,
      value: vi.fn(),
    })
    mockUseAuth.mockReturnValue({
      status: 'authenticated',
      session: null,
      login: vi.fn(),
      logout: vi.fn(),
      requireReauthentication: vi.fn(),
    })
    mockGetActiveJobs.mockResolvedValue({
      items: [],
      summary: { active_count: 0, running_count: 0, queued_count: 0, cancel_requested_count: 0 },
    })
    mockGetJob.mockReset()
    mockGetJobEvents.mockReset()
    mockCancelJob.mockReset()
    mockGetMeta.mockResolvedValue({
      app: { name: 'Stacklab', version: 'test' },
      environment: { stack_root: '/srv/stacklab', platform: 'linux' },
      docker: { engine_version: '29.0.0', compose_version: '2.40.0' },
      features: { host_shell: true },
    })
    mockGetStacks.mockResolvedValue({ items: [], summary: { stack_count: 0 } })
  })

  it('navigates with number hotkeys when no modal is open', async () => {
    renderRoot()

    fireEvent.keyDown(window, { key: '2' })

    await waitFor(() => {
      expect(screen.getByTestId('path')).toHaveTextContent('/host')
    })
  })

  it('does not navigate with number hotkeys while a modal is open', () => {
    renderRoot({ modal: true })

    fireEvent.keyDown(window, { key: '2' })

    expect(screen.getByTestId('path')).toHaveTextContent('/stacks')
  })
})
