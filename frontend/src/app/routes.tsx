import { lazy, Suspense } from 'react'
import { Navigate, Routes, Route } from 'react-router-dom'

import { AuthGuard } from '@/components/auth-guard'
import { RootLayout } from '@/app/root-layout'
import { StackLayout } from '@/app/stack-layout'
import { PageMetadataProvider } from '@/app/page-metadata'
import { LoginPage } from '@/pages/login-page'
import { NotFoundPage } from '@/pages/not-found-page'
import { StacksPage } from '@/pages/stacks-page'
const CreateStackPage = lazy(() => import('@/pages/create-stack-page').then((m) => ({ default: m.CreateStackPage })))
import { HostPage } from '@/pages/host-page'
const ConfigPage = lazy(() => import('@/pages/config-page').then((m) => ({ default: m.ConfigPage })))
const MaintenancePage = lazy(() => import('@/pages/maintenance-page').then((m) => ({ default: m.MaintenancePage })))
const DockerAdminPage = lazy(() => import('@/pages/docker-admin-page').then((m) => ({ default: m.DockerAdminPage })))
import { GlobalAuditPage } from '@/pages/global-audit-page'
import {
  SettingsAboutPage,
  SettingsAutomationPage,
  SettingsNotificationsPage,
  SettingsPage,
  SettingsSecurityPage,
  SettingsUpdatesPage,
} from '@/pages/settings-page'
import { StackOverviewPage } from '@/pages/stack-overview-page'
import { StackAuditPage } from '@/pages/stack-audit-page'

const StackEditorPage = lazy(() => import('@/pages/stack-editor-page').then((m) => ({ default: m.StackEditorPage })))
const StackLogsPage = lazy(() => import('@/pages/stack-logs-page').then((m) => ({ default: m.StackLogsPage })))
const StackStatsPage = lazy(() => import('@/pages/stack-stats-page').then((m) => ({ default: m.StackStatsPage })))
const StackFilesPage = lazy(() => import('@/pages/stack-files-page').then((m) => ({ default: m.StackFilesPage })))
const StackTerminalPage = lazy(() => import('@/pages/stack-terminal-page').then((m) => ({ default: m.StackTerminalPage })))

export function AppRoutes() {
  return (
    <PageMetadataProvider>
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
          <Route index element={<Navigate to="/stacks" replace />} />
          <Route path="stacks" element={<StacksPage />} />
          <Route path="stacks/new" element={<Suspense fallback={<PageLoading title="Create stack" />}><CreateStackPage /></Suspense>} />
          <Route path="host" element={<HostPage />} />
          <Route path="config" element={<Suspense fallback={<PageLoading title="Config" />}><ConfigPage /></Suspense>} />
          <Route path="maintenance" element={<Suspense fallback={<PageLoading title="Maintenance" />}><MaintenancePage /></Suspense>} />
          <Route path="docker" element={<Suspense fallback={<PageLoading title="Docker" />}><DockerAdminPage /></Suspense>} />
          <Route path="audit" element={<GlobalAuditPage />} />
          <Route path="settings" element={<SettingsPage />}>
            <Route index element={<Navigate to="security" replace />} />
            <Route path="security" element={<SettingsSecurityPage />} />
            <Route path="notifications" element={<SettingsNotificationsPage />} />
            <Route path="automation" element={<SettingsAutomationPage />} />
            <Route path="updates" element={<SettingsUpdatesPage />} />
            <Route path="about" element={<SettingsAboutPage />} />
          </Route>
          <Route path="stacks/:stackId" element={<StackLayout />}>
            <Route index element={<StackOverviewPage />} />
            <Route path="editor" element={<Suspense fallback={<StackViewLoading title="Editor" />}><StackEditorPage /></Suspense>} />
            <Route path="files" element={<Suspense fallback={<StackViewLoading title="Files" />}><StackFilesPage /></Suspense>} />
            <Route path="logs" element={<Suspense fallback={<StackViewLoading title="Logs" />}><StackLogsPage /></Suspense>} />
            <Route path="stats" element={<Suspense fallback={<StackViewLoading title="Stats" />}><StackStatsPage /></Suspense>} />
            <Route path="terminal" element={<Suspense fallback={<StackViewLoading title="Terminal" />}><StackTerminalPage /></Suspense>} />
            <Route path="audit" element={<StackAuditPage />} />
          </Route>
        </Route>
        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </PageMetadataProvider>
  )
}

function PageLoading({ title }: { title: string }) {
  return (
    <section aria-busy="true" className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <h1 className="text-2xl font-semibold tracking-[-0.04em] text-[var(--text)]">{title}</h1>
      <p className="mt-2 text-sm text-[var(--muted)]" role="status">Loading…</p>
    </section>
  )
}

function StackViewLoading({ title }: { title: string }) {
  return <p aria-busy="true" className="text-sm text-[var(--muted)]" role="status">Loading {title.toLowerCase()}…</p>
}
