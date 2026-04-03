import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'

import * as api from '@/lib/api-client'
import type { SessionResponse } from '@/lib/api-types'

interface AuthState {
  status: 'loading' | 'authenticated' | 'unauthenticated'
  session: SessionResponse | null
}

interface AuthContextValue extends AuthState {
  login: (password: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ status: 'loading', session: null })
  const navigate = useNavigate()
  const location = useLocation()

  useEffect(() => {
    let cancelled = false

    api.getSession()
      .then((session) => {
        if (cancelled) return
        if (session.authenticated) {
          setState({ status: 'authenticated', session })
        } else {
          setState({ status: 'unauthenticated', session: null })
        }
      })
      .catch(() => {
        if (cancelled) return
        setState({ status: 'unauthenticated', session: null })
      })

    return () => { cancelled = true }
  }, [])

  const login = useCallback(async (password: string) => {
    await api.login(password)
    const session = await api.getSession()
    setState({ status: 'authenticated', session })
    const from = location.state?.from?.pathname ?? '/stacks'
    navigate(from, { replace: true })
  }, [navigate, location.state])

  const logout = useCallback(async () => {
    await api.logout()
    setState({ status: 'unauthenticated', session: null })
    navigate('/login', { replace: true })
  }, [navigate])

  return (
    <AuthContext.Provider value={{ ...state, login, logout }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
