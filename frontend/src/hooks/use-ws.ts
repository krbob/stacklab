import { useContext } from 'react'
import { WsContext, type WsContextValue } from '@/contexts/ws-context'

export function useWs(): WsContextValue {
  const ctx = useContext(WsContext)
  if (!ctx) throw new Error('useWs must be used within WsProvider')
  return ctx
}
