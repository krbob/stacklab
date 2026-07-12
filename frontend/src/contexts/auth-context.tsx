import { createContext, useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'

import * as api from '@/lib/api-client'
import type { SessionResponse } from '@/lib/api-types'

interface AuthState {
  status: 'loading' | 'authenticated' | 'unauthenticated'
  session: SessionResponse | null
}

export interface AuthContextValue extends AuthState {
  login: (password: string) => Promise<void>
  logout: () => Promise<void>
  requireReauthentication: (reason?: 'password_changed' | 'session_expired') => void
}

// eslint-disable-next-line react-refresh/only-export-components
export const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ status: 'loading', session: null })
  const [checkingSession, setCheckingSession] = useState(true)
  const [bootstrapError, setBootstrapError] = useState<Error | null>(null)
  const sessionRequestIDRef = useRef(0)
  const navigate = useNavigate()
  const location = useLocation()

  const checkSession = useCallback(async () => {
    const requestID = sessionRequestIDRef.current + 1
    sessionRequestIDRef.current = requestID
    setCheckingSession(true)
    try {
      const session = await api.getSession()
      if (sessionRequestIDRef.current !== requestID) return
      setBootstrapError(null)
      if (session.authenticated) {
        setState({ status: 'authenticated', session })
      } else {
        setState({ status: 'unauthenticated', session: null })
      }
    } catch (error) {
      if (sessionRequestIDRef.current !== requestID) return
      if (error instanceof api.ApiClientError && error.status === 401) {
        setBootstrapError(null)
        setState({ status: 'unauthenticated', session: null })
      } else {
        setState({ status: 'loading', session: null })
        setBootstrapError(error instanceof Error ? error : new Error('Session verification failed'))
      }
    } finally {
      if (sessionRequestIDRef.current === requestID) setCheckingSession(false)
    }
  }, [])

  useEffect(() => {
    void checkSession()
    return () => {
      sessionRequestIDRef.current += 1
    }
  }, [checkSession])

  const login = useCallback(async (password: string) => {
    await api.login(password)
    const session = await api.getSession()
    setState({ status: 'authenticated', session })
    const from = location.state?.from?.pathname ?? '/stacks'
    navigate(from, { replace: true })
  }, [navigate, location.state])

  const logout = useCallback(async () => {
    try {
      await api.logout()
    } catch (error) {
      if (!(error instanceof api.ApiClientError) || error.status !== 401) throw error
    }
    setState({ status: 'unauthenticated', session: null })
    navigate('/login', { replace: true })
  }, [navigate])

  const requireReauthentication = useCallback((reason?: 'password_changed' | 'session_expired') => {
    setState({ status: 'unauthenticated', session: null })
    navigate('/login', { replace: true, state: reason ? { reason } : undefined })
  }, [navigate])

  if (bootstrapError) {
    return (
      <SessionBootstrapError
        error={bootstrapError}
        retrying={checkingSession}
        onRetry={() => { void checkSession() }}
      />
    )
  }

  if (checkingSession) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <h1 className="sr-only">Loading Stacklab</h1>
        <div className="text-sm text-[var(--muted)]" role="status">Verifying session…</div>
      </div>
    )
  }

  return (
    <AuthContext.Provider value={{ ...state, login, logout, requireReauthentication }}>
      {children}
    </AuthContext.Provider>
  )
}

function SessionBootstrapError({ error, retrying, onRetry }: {
  error: Error
  retrying: boolean
  onRetry: () => void
}) {
  return (
    <main className="flex min-h-screen items-center justify-center px-5">
      <div className="w-full max-w-md rounded-lg border border-[var(--danger)]/20 bg-[var(--panel)] p-6 shadow-[var(--shadow)]" role="alert">
        <h1 className="text-lg font-medium text-[var(--text)]">Unable to verify your session</h1>
        <p className="mt-2 text-sm leading-6 text-[var(--muted)]">
          Stacklab could not confirm whether your session is active. Protected content has not been loaded.
        </p>
        <p className="mt-2 text-xs text-[var(--danger)]">{error.message}</p>
        <button
          type="button"
          onClick={onRetry}
          disabled={retrying}
          className="mt-4 rounded-md border border-[var(--danger)]/30 px-4 py-2 text-sm text-[var(--danger)] hover:bg-[var(--danger)]/10 disabled:opacity-40"
        >
          {retrying ? 'Retrying…' : 'Retry'}
        </button>
      </div>
    </main>
  )
}
