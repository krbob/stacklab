import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter, RouterProvider } from 'react-router-dom'

import { AuthProvider } from '@/contexts/auth-context'
import { JobDrawerProvider } from '@/contexts/job-drawer-context'
import { AuthenticatedWsProvider } from '@/app/authenticated-ws-provider'
import { AppRoutes } from '@/app/routes'
import '@/index.css'

const router = createBrowserRouter([
  {
    path: '*',
    element: (
      <AuthProvider>
        <JobDrawerProvider>
          <AuthenticatedWsProvider>
            <AppRoutes />
          </AuthenticatedWsProvider>
        </JobDrawerProvider>
      </AuthProvider>
    ),
  },
])

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
)

if ('serviceWorker' in navigator && import.meta.env.PROD) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {})
  })
}
