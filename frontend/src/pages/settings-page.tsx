import { NavLink, Outlet } from 'react-router-dom'

import { PageHeader } from '@/components/page-header'
import { cn } from '@/lib/cn'
import { AboutSettingsSection } from '@/pages/settings/about-settings-section'
import { HostSettingsSection } from '@/pages/settings/host-settings-section'
import { NotificationsSection } from '@/pages/settings/notification-settings-section'
import { MaintenanceSchedulesSection } from '@/pages/settings/maintenance-schedules-section'
import { PasswordSettingsSection } from '@/pages/settings/password-settings-section'
import { SettingsCard } from '@/pages/settings/settings-card'
import { SettingsDraftProvider } from '@/pages/settings/settings-draft-context'
import { StacklabUpdateSection } from '@/pages/settings/stacklab-update-section'

const settingsSections = [
  { to: '/settings/security', label: 'Security' },
  { to: '/settings/notifications', label: 'Notifications' },
  { to: '/settings/automation', label: 'Automation' },
  { to: '/settings/updates', label: 'Updates' },
  { to: '/settings/about', label: 'About' },
] as const

export function SettingsPage() {
  return (
    <SettingsDraftProvider>
      <div className="flex flex-col gap-4">
        <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
          <PageHeader kicker="System" title="Settings" />

          <nav aria-label="Settings sections" className="mt-5 grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-5">
            {settingsSections.map(({ to, label }) => (
              <NavLink
                key={to}
                to={to}
                className={({ isActive }) => cn(
                  'flex min-h-11 items-center justify-center rounded-md border px-3 py-2 text-center text-sm font-medium transition focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--accent)]',
                  isActive
                    ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                    : 'border-[var(--panel-border)] text-[var(--muted)] hover:border-[rgba(245,165,36,0.25)] hover:text-[var(--text)]',
                )}
              >
                {label}
              </NavLink>
            ))}
          </nav>
        </section>

        <Outlet />
      </div>
    </SettingsDraftProvider>
  )
}

export function SettingsSecurityPage() {
  return (
    <div className="columns-[26rem] gap-4">
      <SettingsCard>
        <PasswordSettingsSection />
      </SettingsCard>
      <SettingsCard>
        <HostSettingsSection />
      </SettingsCard>
    </div>
  )
}

export function SettingsNotificationsPage() {
  return (
    <div className="max-w-3xl">
      <SettingsCard>
        <NotificationsSection />
      </SettingsCard>
    </div>
  )
}

export function SettingsAutomationPage() {
  return (
    <div className="max-w-3xl">
      <SettingsCard>
        <MaintenanceSchedulesSection />
      </SettingsCard>
    </div>
  )
}

export function SettingsUpdatesPage() {
  return (
    <div className="max-w-3xl">
      <SettingsCard>
        <StacklabUpdateSection />
      </SettingsCard>
    </div>
  )
}

export function SettingsAboutPage() {
  return (
    <div className="max-w-3xl">
      <AboutSettingsSection />
    </div>
  )
}
