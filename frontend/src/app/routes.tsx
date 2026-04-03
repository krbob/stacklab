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
import { lazy, Suspense } from 'react'
import { StackPlaceholderPage } from '@/pages/stack-placeholder-page'

const StackLogsPage = lazy(() => import('@/pages/stack-logs-page').then((m) => ({ default: m.StackLogsPage })))
const StackStatsPage = lazy(() => import('@/pages/stack-stats-page').then((m) => ({ default: m.StackStatsPage })))
const StackTerminalPage = lazy(() => import('@/pages/stack-terminal-page').then((m) => ({ default: m.StackTerminalPage })))

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
          <Route path="logs" element={<Suspense><StackLogsPage /></Suspense>} />
          <Route path="stats" element={<Suspense><StackStatsPage /></Suspense>} />
          <Route path="terminal" element={<Suspense><StackTerminalPage /></Suspense>} />
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
