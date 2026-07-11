import { createContext, useContext, useLayoutEffect } from 'react'
import { useLocation } from 'react-router-dom'

export interface StackPageIdentity {
  id: string
  name: string
}

export interface RegisteredStackIdentity extends StackPageIdentity {
  pathname: string
}

export type RegisterStackIdentity = (identity: RegisteredStackIdentity | null) => void

export const StackIdentityContext = createContext<RegisterStackIdentity | null>(null)

export function useStackPageIdentity(identity: StackPageIdentity | null) {
  const registerIdentity = useContext(StackIdentityContext)
  const location = useLocation()
  const id = identity?.id ?? null
  const name = identity?.name ?? null

  useLayoutEffect(() => {
    if (!registerIdentity) return

    registerIdentity(id && name ? { id, name, pathname: location.pathname } : null)
    return () => registerIdentity(null)
  }, [id, location.pathname, name, registerIdentity])
}
