import { PageHeader } from '@/components/page-header'
import { AboutSettingsSection } from '@/pages/settings/about-settings-section'
import { HostSettingsSection } from '@/pages/settings/host-settings-section'
import { NotificationsSection } from '@/pages/settings/notification-settings-section'
import { MaintenanceSchedulesSection } from '@/pages/settings/maintenance-schedules-section'
import { PasswordSettingsSection } from '@/pages/settings/password-settings-section'
import { SettingsCard } from '@/pages/settings/settings-card'
import { StacklabUpdateSection } from '@/pages/settings/stacklab-update-section'

export function SettingsPage() {
  return (
    <div className="flex flex-col gap-4">
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <PageHeader kicker="System" title="Settings" />
      </section>

      {/* Staggered card grid: every section owns its save button and status */}
      <div className="columns-[26rem] gap-4">
        {/* Password */}
        <SettingsCard>
          <PasswordSettingsSection />
        </SettingsCard>

        {/* Notifications */}
        <SettingsCard>
          <NotificationsSection />
        </SettingsCard>

        {/* Maintenance Schedules */}
        <SettingsCard>
          <MaintenanceSchedulesSection />
        </SettingsCard>

        {/* Stacklab Update */}
        <SettingsCard>
          <StacklabUpdateSection />
        </SettingsCard>

        {/* Host */}
        <SettingsCard>
          <HostSettingsSection />
        </SettingsCard>

        {/* About */}
        <AboutSettingsSection />
      </div>
    </div>
  )
}
