import { act, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiClientError, getSession, logout } from '@/lib/api-client'
import type { SessionResponse } from '@/lib/api-types'
import { useAuth } from '@/hooks/use-auth'
import { AuthProvider, type AuthContextValue } from './auth-context'

vi.mock('@/lib/api-client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api-client')>()
  return {
    ...actual,
    getSession: vi.fn(),
    login: vi.fn(),
    logout: vi.fn(),
  }
})

const mockGetSession = vi.mocked(getSession)
const mockApiLogout = vi.mocked(logout)
let latestAuth: AuthContextValue | null = null
const authenticatedSession: SessionResponse = {
  authenticated: true,
  user: { id: 'local', display_name: 'Local Operator' },
  features: { host_shell: false },
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })

  return { promise, resolve, reject }
}

function AuthProbe() {
  const auth = useAuth()
  latestAuth = auth
  return <div>Auth child: {auth.status}</div>
}

function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location">{location.pathname}{location.search}</div>
}

function renderProvider() {
  return render(
    <MemoryRouter initialEntries={['/stacks?view=compact']}>
      <LocationProbe />
      <AuthProvider>
        <AuthProbe />
      </AuthProvider>
    </MemoryRouter>,
  )
}

describe('AuthProvider session bootstrap', () => {
  beforeEach(() => {
    mockGetSession.mockReset()
    mockApiLogout.mockReset()
    latestAuth = null
  })

  it('keeps protected children unmounted when session verification fails', async () => {
    mockGetSession.mockRejectedValue(new TypeError('Failed to fetch'))

    renderProvider()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Unable to verify your session')
    expect(alert).toHaveTextContent('Failed to fetch')
    expect(screen.queryByText(/Auth child:/)).not.toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/stacks?view=compact')
  })

  it('retries in place and mounts children only after session verification succeeds', async () => {
    const retry = deferred<SessionResponse>()
    mockGetSession
      .mockRejectedValueOnce(new Error('session endpoint unavailable'))
      .mockReturnValueOnce(retry.promise)

    renderProvider()

    fireEvent.click(await screen.findByRole('button', { name: 'Retry' }))

    expect(screen.getByRole('button', { name: 'Retrying…' })).toBeDisabled()
    expect(mockGetSession).toHaveBeenCalledTimes(2)
    expect(screen.queryByText(/Auth child:/)).not.toBeInTheDocument()

    await act(async () => {
      retry.resolve(authenticatedSession)
      await retry.promise
    })

    expect(await screen.findByText('Auth child: authenticated')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/stacks?view=compact')
  })

  it('treats an explicit 401 as an unauthenticated session', async () => {
    mockGetSession.mockRejectedValue(new ApiClientError(401, 'unauthorized', 'Session expired.'))

    renderProvider()

    expect(await screen.findByText('Auth child: unauthenticated')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/stacks?view=compact')
  })

  it('treats a logout 401 as a completed logout', async () => {
    mockGetSession.mockResolvedValue(authenticatedSession)
    mockApiLogout.mockRejectedValue(new ApiClientError(401, 'unauthorized', 'Session already ended.'))
    renderProvider()
    await screen.findByText('Auth child: authenticated')

    await act(async () => {
      await latestAuth!.logout()
    })

    expect(screen.getByText('Auth child: unauthenticated')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/login')
  })

  it('keeps the authenticated session and route when logout cannot be confirmed', async () => {
    mockGetSession.mockResolvedValue(authenticatedSession)
    mockApiLogout.mockRejectedValue(new TypeError('Failed to fetch'))
    renderProvider()
    await screen.findByText('Auth child: authenticated')

    await expect(latestAuth!.logout()).rejects.toThrow('Failed to fetch')

    expect(screen.getByText('Auth child: authenticated')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/stacks?view=compact')
  })
})
