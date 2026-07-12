import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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
const mockLogout = vi.fn()

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

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })

  return { promise, resolve, reject }
}

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

function renderRoot({ modal = false, initialEntry = '/stacks' } = {}) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/" element={<RootLayout />}>
          <Route path="stacks" element={<LocationProbe modal={modal} />} />
          <Route path="host" element={<LocationProbe />} />
          <Route path="settings/*" element={<LocationProbe />} />
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
      logout: mockLogout,
      requireReauthentication: vi.fn(),
    })
    mockLogout.mockReset()
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

  it('keeps Settings active on child routes and uses its canonical security entry', async () => {
    renderRoot({ initialEntry: '/settings/notifications' })

    const settingsLink = screen.getByRole('link', { name: /Settings/ })
    expect(settingsLink).toHaveAttribute('aria-current', 'page')
    expect(settingsLink).toHaveAttribute('href', '/settings/security')

    fireEvent.keyDown(window, { key: '7' })

    await waitFor(() => {
      expect(screen.getByTestId('path')).toHaveTextContent('/settings/security')
    })
  })

  it('does not navigate with number hotkeys while a modal is open', () => {
    renderRoot({ modal: true })

    fireEvent.keyDown(window, { key: '2' })

    expect(screen.getByTestId('path')).toHaveTextContent('/stacks')
  })

  it('exposes an accessible mobile drawer and restores focus on Escape', () => {
    document.body.style.overflow = 'auto'
    renderRoot({ initialEntry: '/settings' })

    const more = screen.getByRole('button', { name: 'More navigation' })
    expect(more).toHaveAttribute('aria-pressed', 'true')
    expect(more).toHaveAttribute('aria-expanded', 'false')
    expect(more).toHaveClass('text-[var(--accent)]')

    fireEvent.click(more)

    expect(more).toHaveAttribute('aria-expanded', 'true')
    const navigation = screen.getByRole('dialog', { name: 'Navigation' })
    expect(navigation).toHaveAttribute('id', 'mobile-navigation')
    expect(navigation).toHaveAttribute('aria-modal', 'true')
    expect(more).toHaveAttribute('aria-controls', navigation.id)
    expect(within(navigation).getByRole('button', { name: 'Close navigation' })).toHaveFocus()
    expect(document.body).toHaveStyle({ overflow: 'hidden' })

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(screen.queryByRole('dialog', { name: 'Navigation' })).not.toBeInTheDocument()
    expect(more).toHaveAttribute('aria-expanded', 'false')
    expect(more).toHaveFocus()
    expect(document.body).toHaveStyle({ overflow: 'auto' })
    document.body.style.overflow = ''
  })

  it('closes mobile navigation after following a link', async () => {
    renderRoot({ initialEntry: '/settings' })

    const more = screen.getByRole('button', { name: 'More navigation' })
    fireEvent.click(more)
    const navigation = screen.getByRole('dialog', { name: 'Navigation' })

    fireEvent.click(within(navigation).getByRole('link', { name: /Host/ }))

    await waitFor(() => expect(screen.getByTestId('path')).toHaveTextContent('/host'))
    expect(screen.queryByRole('dialog', { name: 'Navigation' })).not.toBeInTheDocument()
    expect(more).toHaveAttribute('aria-expanded', 'false')
  })

  it('disables logout while the request is pending and prevents duplicate calls', async () => {
    const pendingLogout = deferred<void>()
    mockLogout.mockReturnValue(pendingLogout.promise)
    renderRoot()

    fireEvent.click(screen.getByRole('button', { name: 'Log out' }))

    const pendingButton = screen.getByRole('button', { name: 'Logging out…' })
    expect(pendingButton).toBeDisabled()
    fireEvent.click(pendingButton)
    expect(mockLogout).toHaveBeenCalledTimes(1)

    await act(async () => {
      pendingLogout.resolve(undefined)
      await pendingLogout.promise
    })

    expect(screen.getByRole('button', { name: 'Log out' })).toBeEnabled()
  })

  it('keeps mobile navigation open when logout fails and allows a retry', async () => {
    mockLogout
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockResolvedValueOnce(undefined)
    renderRoot()

    fireEvent.click(screen.getByRole('button', { name: 'More navigation' }))
    const navigation = screen.getByRole('dialog', { name: 'Navigation' })
    fireEvent.click(within(navigation).getByRole('button', { name: 'Log out' }))

    const alert = await within(navigation).findByRole('alert')
    expect(alert).toHaveTextContent(
      'Logout could not be confirmed. Your session may still be active. Try again.',
    )
    expect(screen.getByTestId('path')).toHaveTextContent('/stacks')
    expect(within(navigation).getByRole('button', { name: 'Log out' })).toBeEnabled()

    fireEvent.click(within(navigation).getByRole('button', { name: 'Log out' }))

    await waitFor(() => expect(mockLogout).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Navigation' })).not.toBeInTheDocument())
  })
})
