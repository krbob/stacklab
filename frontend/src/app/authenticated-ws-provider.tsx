import { WsProvider } from '@/contexts/ws-context'
import { useAuth } from '@/hooks/use-auth'

export function AuthenticatedWsProvider({ children }: { children: React.ReactNode }) {
  const { status } = useAuth()
  return (
    <WsProvider authenticated={status === 'authenticated'}>
      {children}
    </WsProvider>
  )
}
