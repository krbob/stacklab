import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'

import { AuthProvider } from '@/contexts/auth-context'
import { AuthenticatedWsProvider } from '@/app/authenticated-ws-provider'
import { AppRoutes } from '@/app/routes'
import '@/index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <AuthProvider>
        <AuthenticatedWsProvider>
          <AppRoutes />
        </AuthenticatedWsProvider>
      </AuthProvider>
    </BrowserRouter>
  </StrictMode>,
)
