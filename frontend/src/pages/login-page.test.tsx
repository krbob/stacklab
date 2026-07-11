import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiClientError } from '@/lib/api-client'
import { useAuth } from '@/hooks/use-auth'
import { LoginPage } from './login-page'

vi.mock('@/hooks/use-auth', () => ({
  useAuth: vi.fn(),
}))

const mockUseAuth = vi.mocked(useAuth)
const mockLogin = vi.fn()

function renderLogin() {
  return render(
    <MemoryRouter>
      <LoginPage />
    </MemoryRouter>,
  )
}

describe('LoginPage', () => {
  beforeEach(() => {
    mockLogin.mockReset()
    mockUseAuth.mockReturnValue({
      status: 'unauthenticated',
      session: null,
      login: mockLogin,
      logout: vi.fn(),
      requireReauthentication: vi.fn(),
    })
  })

  it('shows invalid password only for authentication failures', async () => {
    mockLogin.mockRejectedValue(new ApiClientError(401, 'unauthorized', 'Invalid password.'))

    renderLogin()
    fireEvent.change(screen.getByTestId('login-password'), { target: { value: 'wrong' } })
    fireEvent.click(screen.getByTestId('login-submit'))

    expect(await screen.findByText('Invalid password')).toBeInTheDocument()
  })

  it('shows a connection error for network failures', async () => {
    mockLogin.mockRejectedValue(new TypeError('Failed to fetch'))

    renderLogin()
    fireEvent.change(screen.getByTestId('login-password'), { target: { value: 'secret' } })
    fireEvent.click(screen.getByTestId('login-submit'))

    expect(await screen.findByText('Unable to reach Stacklab. Check your connection and try again.')).toBeInTheDocument()
  })
})
