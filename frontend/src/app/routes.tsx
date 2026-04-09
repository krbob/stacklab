import { lazy, Suspense } from 'react'
import { Routes, Route } from 'react-router-dom'

import { AuthGuard } from '@/components/auth-guard'
import { RootLayout } from '@/app/root-layout'
import { StackLayout } from '@/app/stack-layout'
import { LoginPage } from '@/pages/login-page'
import { NotFoundPage } from '@/pages/not-found-page'
import { StacksPage } from '@/pages/stacks-page'
const CreateStackPage = lazy(() => import('@/pages/create-stack-page').then((m) => ({ default: m.CreateStackPage })))
import { HostPage } from '@/pages/host-page'
const ConfigPage = lazy(() => import('@/pages/config-page').then((m) => ({ default: m.ConfigPage })))
const MaintenancePage = lazy(() => import('@/pages/maintenance-page').then((m) => ({ default: m.MaintenancePage })))
const DockerAdminPage = lazy(() => import('@/pages/docker-admin-page').then((m) => ({ default: m.DockerAdminPage })))
import { GlobalAuditPage } from '@/pages/global-audit-page'
import { SettingsPage } from '@/pages/settings-page'
import { StackOverviewPage } from '@/pages/stack-overview-page'
import { StackAuditPage } from '@/pages/stack-audit-page'

const StackEditorPage = lazy(() => import('@/pages/stack-editor-page').then((m) => ({ default: m.StackEditorPage })))
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
        <Route path="stacks/new" element={<Suspense><CreateStackPage /></Suspense>} />
        <Route path="host" element={<HostPage />} />
        <Route path="config" element={<Suspense><ConfigPage /></Suspense>} />
        <Route path="maintenance" element={<Suspense><MaintenancePage /></Suspense>} />
        <Route path="docker" element={<Suspense><DockerAdminPage /></Suspense>} />
        <Route path="audit" element={<GlobalAuditPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="stacks/:stackId" element={<StackLayout />}>
          <Route index element={<StackOverviewPage />} />
          <Route path="editor" element={<Suspense><StackEditorPage /></Suspense>} />
          <Route path="logs" element={<Suspense><StackLogsPage /></Suspense>} />
          <Route path="stats" element={<Suspense><StackStatsPage /></Suspense>} />
          <Route path="terminal" element={<Suspense><StackTerminalPage /></Suspense>} />
          <Route path="audit" element={<StackAuditPage />} />
        </Route>
      </Route>
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  )
}
