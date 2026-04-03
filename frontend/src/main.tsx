import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'

import { AuthProvider } from '@/contexts/auth-context'
import { WsProvider } from '@/contexts/ws-context'
import { AppRoutes } from '@/app/routes'
import '@/index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <AuthProvider>
        <WsProvider>
          <AppRoutes />
        </WsProvider>
      </AuthProvider>
    </BrowserRouter>
  </StrictMode>,
)
