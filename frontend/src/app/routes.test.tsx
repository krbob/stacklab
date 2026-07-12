import { render, screen } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { AppRoutes } from './routes'

vi.mock('@/components/auth-guard', () => ({
  AuthGuard: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))

vi.mock('@/app/root-layout', async () => {
  const { Outlet } = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { RootLayout: () => <Outlet /> }
})

vi.mock('@/pages/login-page', () => ({ LoginPage: () => <h1>Log in</h1> }))
vi.mock('@/pages/not-found-page', () => ({ NotFoundPage: () => <h1>Page not found</h1> }))
vi.mock('@/pages/stacks-page', () => ({ StacksPage: () => <h1>Stacks</h1> }))
vi.mock('@/pages/create-stack-page', () => ({ CreateStackPage: () => <h1>Create stack</h1> }))
vi.mock('@/pages/host-page', () => ({ HostPage: () => <h1>Host</h1> }))
vi.mock('@/pages/config-page', () => ({ ConfigPage: () => <h1>Config</h1> }))
vi.mock('@/pages/maintenance-page', () => ({ MaintenancePage: () => <h1>Maintenance</h1> }))
vi.mock('@/pages/docker-admin-page', () => ({ DockerAdminPage: () => <h1>Docker</h1> }))
vi.mock('@/pages/global-audit-page', () => ({ GlobalAuditPage: () => <h1>Audit log</h1> }))
vi.mock('@/pages/settings-page', async () => {
  const { Outlet } = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    SettingsPage: () => <><h1>Settings</h1><Outlet /></>,
    SettingsSecurityPage: () => <h2>Security</h2>,
    SettingsNotificationsPage: () => <h2>Notifications</h2>,
    SettingsAutomationPage: () => <h2>Automation</h2>,
    SettingsUpdatesPage: () => <h2>Updates</h2>,
    SettingsAboutPage: () => <h2>About</h2>,
  }
})

describe('AppRoutes page metadata', () => {
  it.each([
    ['/', 'Stacks'],
    ['/stacks', 'Stacks'],
    ['/stacks/new', 'Create stack'],
    ['/host', 'Host'],
    ['/config', 'Config'],
    ['/maintenance', 'Maintenance'],
    ['/docker', 'Docker'],
    ['/audit', 'Audit log'],
    ['/login', 'Log in'],
    ['/missing', 'Page not found'],
    ['/settings/unknown', 'Page not found'],
  ])('wires %s to one h1 and the matching document title', async (path, screenName) => {
    render(
      <MemoryRouter initialEntries={[path]}>
        <AppRoutes />
      </MemoryRouter>,
    )

    expect(await screen.findByRole('heading', { level: 1, name: screenName })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    expect(document.title).toBe(`${screenName} | Stacklab`)
  })

  it('canonicalizes the authenticated root URL to /stacks', async () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <LocationProbe />
        <AppRoutes />
      </MemoryRouter>,
    )

    expect(await screen.findByTestId('route-path')).toHaveTextContent('/stacks')
  })

  it('canonicalizes the settings URL to /settings/security', async () => {
    render(
      <MemoryRouter initialEntries={['/settings']}>
        <LocationProbe />
        <AppRoutes />
      </MemoryRouter>,
    )

    expect(await screen.findByTestId('route-path')).toHaveTextContent('/settings/security')
    expect(screen.getByRole('heading', { level: 1, name: 'Settings' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'Security' })).toBeInTheDocument()
  })
})

function LocationProbe() {
  return <span data-testid="route-path">{useLocation().pathname}</span>
}
