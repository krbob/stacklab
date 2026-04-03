import { createBrowserRouter } from 'react-router-dom'

import { RootLayout } from '@/app/root-layout'
import { StackLayout } from '@/app/stack-layout'
import { CreateStackPage } from '@/pages/create-stack-page'
import { GlobalAuditPage } from '@/pages/global-audit-page'
import { LoginPage } from '@/pages/login-page'
import { NotFoundPage } from '@/pages/not-found-page'
import { SettingsPage } from '@/pages/settings-page'
import { StackPlaceholderPage } from '@/pages/stack-placeholder-page'
import { StacksPage } from '@/pages/stacks-page'

export const router = createBrowserRouter([
  {
    path: '/login',
    element: <LoginPage />,
  },
  {
    path: '/',
    element: <RootLayout />,
    errorElement: <NotFoundPage />,
    children: [
      {
        index: true,
        element: <StacksPage />,
      },
      {
        path: 'stacks',
        element: <StacksPage />,
      },
      {
        path: 'stacks/new',
        element: <CreateStackPage />,
      },
      {
        path: 'audit',
        element: <GlobalAuditPage />,
      },
      {
        path: 'settings',
        element: <SettingsPage />,
      },
      {
        path: 'stacks/:stackId',
        element: <StackLayout />,
        children: [
          {
            index: true,
            element: (
              <StackPlaceholderPage
                title="Overview"
                summary="Runtime services, container states, health, mounts, and quick actions."
                contract="GET /api/stacks/{stackId}"
              />
            ),
          },
          {
            path: 'editor',
            element: (
              <StackPlaceholderPage
                title="Editor"
                summary="Compose and .env editing, validation, and resolved preview."
                contract="GET/PUT /api/stacks/{stackId}/definition, GET/POST /resolved-config"
              />
            ),
          },
          {
            path: 'logs',
            element: (
              <StackPlaceholderPage
                title="Logs"
                summary="Live log stream with service filters and reconnect-safe buffering."
                contract="WS logs.subscribe"
              />
            ),
          },
          {
            path: 'stats',
            element: (
              <StackPlaceholderPage
                title="Stats"
                summary="Live container and aggregate stack metrics from snapshot frames."
                contract="WS stats.subscribe"
              />
            ),
          },
          {
            path: 'terminal',
            element: (
              <StackPlaceholderPage
                title="Terminal"
                summary="Container exec sessions with optional reattach after reconnect."
                contract="WS terminal.open / terminal.attach"
              />
            ),
          },
          {
            path: 'audit',
            element: (
              <StackPlaceholderPage
                title="History"
                summary="Per-stack audit entries with link-out to retained job detail."
                contract="GET /api/stacks/{stackId}/audit"
              />
            ),
          },
        ],
      },
    ],
  },
])
