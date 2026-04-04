import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { AuthGuard } from './auth-guard'

vi.mock('@/hooks/use-auth', () => ({
  useAuth: vi.fn(),
}))

import { useAuth } from '@/hooks/use-auth'
const mockUseAuth = vi.mocked(useAuth)

function renderWithGuard(initialPath = '/protected') {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/login" element={<div>Login page</div>} />
        <Route
          path="/protected"
          element={
            <AuthGuard>
              <div>Protected content</div>
            </AuthGuard>
          }
        />
      </Routes>
    </MemoryRouter>,
  )
}

describe('AuthGuard', () => {
  it('shows loading state while auth is loading', () => {
    mockUseAuth.mockReturnValue({
      status: 'loading',
      session: null,
      login: vi.fn(),
      logout: vi.fn(),
    })

    renderWithGuard()
    expect(screen.getByText('Loading...')).toBeInTheDocument()
    expect(screen.queryByText('Protected content')).not.toBeInTheDocument()
  })

  it('redirects to login when unauthenticated', () => {
    mockUseAuth.mockReturnValue({
      status: 'unauthenticated',
      session: null,
      login: vi.fn(),
      logout: vi.fn(),
    })

    renderWithGuard()
    expect(screen.getByText('Login page')).toBeInTheDocument()
    expect(screen.queryByText('Protected content')).not.toBeInTheDocument()
  })

  it('renders children when authenticated', () => {
    mockUseAuth.mockReturnValue({
      status: 'authenticated',
      session: {
        authenticated: true,
        user: { id: 'local', display_name: 'Local Operator' },
        features: { host_shell: false },
      },
      login: vi.fn(),
      logout: vi.fn(),
    })

    renderWithGuard()
    expect(screen.getByText('Protected content')).toBeInTheDocument()
    expect(screen.queryByText('Login page')).not.toBeInTheDocument()
  })
})
