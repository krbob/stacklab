import { useEffect, useState } from 'react'
import { getMeta } from '@/lib/api-client'
import type { MetaResponse } from '@/lib/api-types'
import { PageHeader } from '@/components/page-header'
import { HostSettingsSection } from '@/pages/settings/host-settings-section'
import { NotificationsSection } from '@/pages/settings/notification-settings-section'
import { MaintenanceSchedulesSection } from '@/pages/settings/maintenance-schedules-section'
import { PasswordSettingsSection } from '@/pages/settings/password-settings-section'
import { SettingsCard } from '@/pages/settings/settings-card'
import { StacklabUpdateSection } from '@/pages/settings/stacklab-update-section'

export function SettingsPage() {
  const [meta, setMeta] = useState<MetaResponse | null>(null)

  useEffect(() => {
    getMeta().then(setMeta).catch(() => {})
  }, [])

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
        {meta && (
          <SettingsCard>
            <h2 className="text-sm font-medium text-[var(--text)]">About</h2>
            <div className="mt-3 grid gap-2 text-sm text-[var(--muted)]">
              <div>Stacklab {meta.app.version}</div>
              <div>Docker Engine {meta.docker.engine_version}</div>
              <div>Docker Compose {meta.docker.compose_version}</div>
              <div>Stack root: <code className="font-mono text-[var(--text)]">{meta.environment.stack_root}</code></div>
            </div>
          </SettingsCard>
        )}
      </div>
    </div>
  )
}
