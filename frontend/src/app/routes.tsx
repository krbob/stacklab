import { Routes, Route } from 'react-router-dom'

import { AuthGuard } from '@/components/auth-guard'
import { RootLayout } from '@/app/root-layout'
import { StackLayout } from '@/app/stack-layout'
import { LoginPage } from '@/pages/login-page'
import { NotFoundPage } from '@/pages/not-found-page'
import { StacksPage } from '@/pages/stacks-page'
import { CreateStackPage } from '@/pages/create-stack-page'
import { GlobalAuditPage } from '@/pages/global-audit-page'
import { SettingsPage } from '@/pages/settings-page'
import { StackOverviewPage } from '@/pages/stack-overview-page'
import { StackPlaceholderPage } from '@/pages/stack-placeholder-page'

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/"
        element={
          <AuthGuard>
            <RootLayout />
          </AuthGuard>
        }
      >
        <Route index element={<StacksPage />} />
        <Route path="stacks" element={<StacksPage />} />
        <Route path="stacks/new" element={<CreateStackPage />} />
        <Route path="audit" element={<GlobalAuditPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="stacks/:stackId" element={<StackLayout />}>
          <Route index element={<StackOverviewPage />} />
          <Route
            path="editor"
            element={
              <StackPlaceholderPage
                title="Editor"
                summary="Compose and .env editing, validation, and resolved preview."
                contract="GET/PUT /api/stacks/{stackId}/definition, GET/POST /resolved-config"
              />
            }
          />
          <Route
            path="logs"
            element={
              <StackPlaceholderPage
                title="Logs"
                summary="Live log stream with service filters and reconnect-safe buffering."
                contract="WS logs.subscribe"
              />
            }
          />
          <Route
            path="stats"
            element={
              <StackPlaceholderPage
                title="Stats"
                summary="Live container and aggregate stack metrics from snapshot frames."
                contract="WS stats.subscribe"
              />
            }
          />
          <Route
            path="terminal"
            element={
              <StackPlaceholderPage
                title="Terminal"
                summary="Container exec sessions with optional reattach after reconnect."
                contract="WS terminal.open / terminal.attach"
              />
            }
          />
          <Route
            path="audit"
            element={
              <StackPlaceholderPage
                title="History"
                summary="Per-stack audit entries with link-out to retained job detail."
                contract="GET /api/stacks/{stackId}/audit"
              />
            }
          />
        </Route>
      </Route>
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  )
}
