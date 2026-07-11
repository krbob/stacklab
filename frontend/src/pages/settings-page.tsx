import { useCallback, useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react'
import { getMeta, changePassword, getNotificationSettings, updateNotificationSettings, sendNotificationTest, getMaintenanceSchedules, updateMaintenanceSchedules, getHostSettings, updateHostSettings, getStacks, getStack, getStacklabUpdateOverview, applyStacklabUpdate } from '@/lib/api-client'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import type { MetaResponse, MaintenanceSchedulesResponse, ScheduleFrequency, ScheduleWeekday, StackListItem, StacklabUpdateOverviewResponse } from '@/lib/api-types'
import { cn } from '@/lib/cn'
import { PageHeader } from '@/components/page-header'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { useAuth } from '@/hooks/use-auth'
import { StatusMessage } from '@/components/status-message'

const PASSWORD_MINIMUM_LENGTH = 12
const PASSWORD_MAXIMUM_LENGTH = 256

export function SettingsPage() {
  const { requireReauthentication } = useAuth()
  const [meta, setMeta] = useState<MetaResponse | null>(null)
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [passwordError, setPasswordError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    getMeta().then(setMeta).catch(() => {})
  }, [])

  const handlePasswordChange = useCallback(async (e: FormEvent) => {
    e.preventDefault()
    if (newPassword !== confirmPassword) {
      setPasswordError('Passwords do not match')
      return
    }
    const newPasswordLength = Array.from(newPassword).length
    if (newPasswordLength < PASSWORD_MINIMUM_LENGTH || newPasswordLength > PASSWORD_MAXIMUM_LENGTH) {
      setPasswordError(`Password must contain between ${PASSWORD_MINIMUM_LENGTH} and ${PASSWORD_MAXIMUM_LENGTH} characters`)
      return
    }

    setSaving(true)
    setPasswordError(null)
    try {
      await changePassword(currentPassword, newPassword)
      requireReauthentication('password_changed')
    } catch (err) {
      setPasswordError(err instanceof Error ? err.message : 'Failed to change password')
    } finally {
      setSaving(false)
    }
  }, [currentPassword, newPassword, confirmPassword, requireReauthentication])

  return (
    <div className="flex flex-col gap-4">
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <PageHeader kicker="System" title="Settings" />
      </section>

      {/* Staggered card grid: every section owns its save button and status */}
      <div className="columns-[26rem] gap-4">
        {/* Password */}
        <SettingsCard>
          <h2 className="text-sm font-medium text-[var(--text)]">Change password</h2>
          <form onSubmit={handlePasswordChange} className="mt-3 max-w-md space-y-3">
            <input type="password" autoComplete="current-password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} placeholder="Current password" disabled={saving} className="w-full rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-2.5 text-sm text-[var(--text)] outline-none transition focus:border-[rgba(245,165,36,0.35)] disabled:opacity-50" />
            <input type="password" autoComplete="new-password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} placeholder="New password" disabled={saving} aria-describedby="new-password-requirements" className="w-full rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-2.5 text-sm text-[var(--text)] outline-none transition focus:border-[rgba(245,165,36,0.35)] disabled:opacity-50" />
            <p id="new-password-requirements" className="text-xs text-[var(--muted)]">Use {PASSWORD_MINIMUM_LENGTH}–{PASSWORD_MAXIMUM_LENGTH} characters.</p>
            <input type="password" autoComplete="new-password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} placeholder="Confirm new password" disabled={saving} className="w-full rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-2.5 text-sm text-[var(--text)] outline-none transition focus:border-[rgba(245,165,36,0.35)] disabled:opacity-50" />
            {passwordError && <StatusMessage className="text-sm text-[var(--danger)]">{passwordError}</StatusMessage>}
            <button type="submit" disabled={saving || !currentPassword || !newPassword || !confirmPassword} className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(245,165,36,0.2)] disabled:opacity-40">
              {saving ? 'Updating...' : 'Update password'}
            </button>
          </form>
        </SettingsCard>

        {/* Notifications */}
        <SettingsCard>
          <NotificationsSection />
        </SettingsCard>

        {/* Maintenance Schedules */}
        <SettingsCard>
          <SchedulesSection />
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

function SettingsCard({ children }: { children: ReactNode }) {
  // Wrapper padding instead of margin: bottom margins bleed across CSS
  // multi-column breaks and misalign column tops.
  return (
    <div className="break-inside-avoid pb-4">
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        {children}
      </section>
    </div>
  )
}

function SettingsLoadError({ title, message, onRetry }: { title: string; message: string; onRetry: () => void }) {
  return (
    <div>
      <h2 className="text-sm font-medium text-[var(--text)]">{title}</h2>
      <p className="mt-2 text-xs text-[var(--danger)]">{message}</p>
      <button type="button" onClick={onRetry} className="mt-3 rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]">
        Retry
      </button>
    </div>
  )
}

function HostSettingsSection() {
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [publicIPLookupEnabled, setPublicIPLookupEnabled] = useState(false)
  const [savedState, setSavedState] = useState('')
  const [savingHost, setSavingHost] = useState(false)
  const [saveResult, setSaveResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  const currentState = JSON.stringify({ publicIPLookupEnabled })
  const isDirty = currentState !== savedState

  const loadSettings = useCallback(() => {
    setLoading(true)
    setLoadError(null)
    setSaveResult(null)
    getHostSettings()
      .then((settings) => {
        setPublicIPLookupEnabled(settings.public_ip_lookup_enabled)
        setSavedState(JSON.stringify({ publicIPLookupEnabled: settings.public_ip_lookup_enabled }))
      })
      .catch((err) => {
        setSavedState('')
        setLoadError(err instanceof Error ? err.message : 'Failed to load host observability settings')
      })
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => { loadSettings() }, [loadSettings])

  const handleSave = useCallback(async () => {
    if (loadError) return
    setSavingHost(true)
    setSaveResult(null)
    try {
      const saved = await updateHostSettings({ public_ip_lookup_enabled: publicIPLookupEnabled })
      setPublicIPLookupEnabled(saved.public_ip_lookup_enabled)
      setSavedState(JSON.stringify({ publicIPLookupEnabled: saved.public_ip_lookup_enabled }))
      setSaveResult({ type: 'success', text: 'Saved' })
    } catch (err) {
      setSaveResult({ type: 'error', text: err instanceof Error ? err.message : 'Save failed' })
    } finally {
      setSavingHost(false)
    }
  }, [loadError, publicIPLookupEnabled])

  if (loadError) {
    return <SettingsLoadError title="Host observability" message={loadError} onRetry={loadSettings} />
  }

  return (
    <div aria-busy={loading || savingHost}>
      <div className="mb-3 flex items-center justify-between gap-3">
        <h2 className="text-sm font-medium text-[var(--text)]">Host observability</h2>
        {loading && <span className="text-xs text-[var(--muted)]" role="status" aria-live="polite">Loading...</span>}
      </div>
      <div className="space-y-3">
        <label className="flex items-start gap-3 text-sm text-[var(--text)]">
          <input
            type="checkbox"
            checked={publicIPLookupEnabled}
            onChange={(event) => setPublicIPLookupEnabled(event.target.checked)}
            disabled={loading || savingHost}
            className="mt-1"
            aria-label="Enable public IP lookup"
          />
          <span className="min-w-0">
            <span className="block">Public IP lookup</span>
            <span className="mt-1 block text-xs text-[var(--muted)]">Uses an external lookup service while the Host page is active. The value stays masked until revealed.</span>
          </span>
        </label>
        <div className="flex items-center gap-3">
          <button type="button" onClick={handleSave} disabled={loading || savingHost || !isDirty} className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(245,165,36,0.2)] disabled:opacity-40">
            {savingHost ? 'Saving...' : 'Save host settings'}
          </button>
          {saveResult && <StatusMessage className={cn('text-xs', saveResult.type === 'success' ? 'text-[var(--ok)]' : 'text-[var(--danger)]')}>{saveResult.text}</StatusMessage>}
        </div>
      </div>
    </div>
  )
}

function NotificationsSection() {
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [enabled, setEnabled] = useState(false)

  // Webhook
  const [webhookEnabled, setWebhookEnabled] = useState(false)
  const [webhookUrl, setWebhookUrl] = useState('')

  // Telegram
  const [telegramEnabled, setTelegramEnabled] = useState(false)
  const [telegramBotToken, setTelegramBotToken] = useState('')
  const [telegramChatId, setTelegramChatId] = useState('')
  const [showBotToken, setShowBotToken] = useState(false)
  const [botTokenConfigured, setBotTokenConfigured] = useState(false)

  // Events
  const [jobFailed, setJobFailed] = useState(true)
  const [jobWarnings, setJobWarnings] = useState(true)
  const [maintenanceSucceeded, setMaintenanceSucceeded] = useState(false)
  const [recoveryFailed, setRecoveryFailed] = useState(false)
  const [serviceError, setServiceError] = useState(false)
  const [runtimeHealthDegraded, setRuntimeHealthDegraded] = useState(false)
  const [runtimeLogErrorBurst, setRuntimeLogErrorBurst] = useState(false)

  const [savingNotif, setSavingNotif] = useState(false)
  const [saveResult, setSaveResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [webhookTestResult, setWebhookTestResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [telegramTestResult, setTelegramTestResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [testingWebhook, setTestingWebhook] = useState(false)
  const [testingTelegram, setTestingTelegram] = useState(false)

  const [savedState, setSavedState] = useState('')

  const currentState = JSON.stringify({ enabled, webhookEnabled, webhookUrl, telegramEnabled, telegramBotToken, telegramChatId, jobFailed, jobWarnings, maintenanceSucceeded, recoveryFailed, serviceError, runtimeHealthDegraded, runtimeLogErrorBurst })
  const isDirty = currentState !== savedState

  const loadSettings = useCallback(() => {
    setLoading(true)
    setLoadError(null)
    setSaveResult(null)
    setWebhookTestResult(null)
    setTelegramTestResult(null)
    getNotificationSettings()
      .then((s) => {
        setEnabled(s.enabled)
        setWebhookEnabled(s.channels?.webhook.enabled ?? s.enabled)
        setWebhookUrl(s.channels?.webhook.url ?? s.webhook_url)
        setTelegramEnabled(s.channels?.telegram.enabled ?? false)
        setTelegramChatId(s.channels?.telegram.chat_id ?? '')
        setBotTokenConfigured(s.channels?.telegram.bot_token_configured ?? false)
        setJobFailed(s.events.job_failed)
        setJobWarnings(s.events.job_succeeded_with_warnings)
        setMaintenanceSucceeded(s.events.maintenance_succeeded)
        setRecoveryFailed(s.events.post_update_recovery_failed ?? false)
        setServiceError(s.events.stacklab_service_error ?? false)
        setRuntimeHealthDegraded(s.events.runtime_health_degraded ?? false)
        setRuntimeLogErrorBurst(s.events.runtime_log_error_burst ?? false)
        const state = JSON.stringify({
          enabled: s.enabled,
          webhookEnabled: s.channels?.webhook.enabled ?? s.enabled,
          webhookUrl: s.channels?.webhook.url ?? s.webhook_url,
          telegramEnabled: s.channels?.telegram.enabled ?? false,
          telegramBotToken: '',
          telegramChatId: s.channels?.telegram.chat_id ?? '',
          jobFailed: s.events.job_failed,
          jobWarnings: s.events.job_succeeded_with_warnings,
          maintenanceSucceeded: s.events.maintenance_succeeded,
          recoveryFailed: s.events.post_update_recovery_failed ?? false,
          serviceError: s.events.stacklab_service_error ?? false,
          runtimeHealthDegraded: s.events.runtime_health_degraded ?? false,
          runtimeLogErrorBurst: s.events.runtime_log_error_burst ?? false,
        })
        setSavedState(state)
      })
      .catch((err) => {
        setSavedState('')
        setLoadError(err instanceof Error ? err.message : 'Failed to load notification settings')
      })
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => { loadSettings() }, [loadSettings])

  const buildRequest = useCallback(() => ({
    enabled,
    webhook_url: webhookUrl,
    events: {
      job_failed: jobFailed,
      job_succeeded_with_warnings: jobWarnings,
      maintenance_succeeded: maintenanceSucceeded,
      post_update_recovery_failed: recoveryFailed,
      stacklab_service_error: serviceError,
      runtime_health_degraded: runtimeHealthDegraded,
      runtime_log_error_burst: runtimeLogErrorBurst,
    },
    channels: {
      webhook: { enabled: webhookEnabled, url: webhookUrl },
      telegram: { enabled: telegramEnabled, bot_token: telegramBotToken, chat_id: telegramChatId },
    },
  }), [enabled, webhookEnabled, webhookUrl, telegramEnabled, telegramBotToken, telegramChatId, jobFailed, jobWarnings, maintenanceSucceeded, recoveryFailed, serviceError, runtimeHealthDegraded, runtimeLogErrorBurst])

  const handleSave = useCallback(async () => {
    if (loadError) return
    setSavingNotif(true)
    setSaveResult(null)
    try {
      await updateNotificationSettings(buildRequest())
      setSaveResult({ type: 'success', text: 'Saved' })
      setSavedState(currentState)
    } catch (err) {
      setSaveResult({ type: 'error', text: err instanceof Error ? err.message : 'Save failed' })
    } finally {
      setSavingNotif(false)
    }
  }, [buildRequest, currentState, loadError])

  const handleTestWebhook = useCallback(async () => {
    if (loadError) return
    setTestingWebhook(true)
    setWebhookTestResult(null)
    try {
      await sendNotificationTest({ ...buildRequest(), channel: 'webhook' })
      setWebhookTestResult({ type: 'success', text: 'Webhook test sent' })
    } catch (err) {
      setWebhookTestResult({ type: 'error', text: err instanceof Error ? err.message : 'Webhook test failed' })
    } finally {
      setTestingWebhook(false)
    }
  }, [buildRequest, loadError])

  const handleTestTelegram = useCallback(async () => {
    if (loadError) return
    setTestingTelegram(true)
    setTelegramTestResult(null)
    try {
      await sendNotificationTest({ ...buildRequest(), channel: 'telegram' })
      setTelegramTestResult({ type: 'success', text: 'Telegram test sent' })
    } catch (err) {
      setTelegramTestResult({ type: 'error', text: err instanceof Error ? err.message : 'Telegram test failed' })
    } finally {
      setTestingTelegram(false)
    }
  }, [buildRequest, loadError])

  if (loading) {
    return (
      <div aria-busy="true">
        <h2 className="text-sm font-medium text-[var(--text)]">Notifications</h2>
        <div className="mt-3 h-20 animate-pulse rounded-md bg-[rgba(255,255,255,0.03)]"><span className="sr-only" role="status" aria-live="polite">Loading notification settings...</span></div>
      </div>
    )
  }

  if (loadError) {
    return <SettingsLoadError title="Notifications" message={loadError} onRetry={loadSettings} />
  }

  return (
    <div>
      <h2 className="text-sm font-medium text-[var(--text)]">Notifications</h2>
      <p className="mt-1 text-xs text-[var(--muted)]">Outgoing notifications. Best-effort delivery, no retries.</p>

      <div className="mt-3 max-w-lg space-y-4">
        <label className="flex items-center gap-2 text-sm text-[var(--text)]">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} className="rounded" />
          Enable notifications
        </label>

        {/* Webhook card */}
        <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4 space-y-3">
          <label className="flex items-center gap-2 text-xs font-medium text-[var(--text)]">
            <input type="checkbox" checked={webhookEnabled} onChange={(e) => setWebhookEnabled(e.target.checked)} className="rounded" />
            Webhook
          </label>
          <input type="url" value={webhookUrl} onChange={(e) => setWebhookUrl(e.target.value)} placeholder="https://hooks.example.com/stacklab" className="w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]" />
          {webhookTestResult && <StatusMessage className={webhookTestResult.type === 'success' ? 'text-xs text-[var(--ok)]' : 'text-xs text-[var(--danger)]'}>{webhookTestResult.text}</StatusMessage>}
          <button onClick={handleTestWebhook} disabled={testingWebhook || !webhookUrl.trim()} className="rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)] disabled:opacity-40">
            {testingWebhook ? 'Sending...' : 'Send test'}
          </button>
        </div>

        {/* Telegram card */}
        <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4 space-y-3">
          <label className="flex items-center gap-2 text-xs font-medium text-[var(--text)]">
            <input type="checkbox" checked={telegramEnabled} onChange={(e) => setTelegramEnabled(e.target.checked)} className="rounded" />
            Telegram
          </label>
          <div className="space-y-2">
            <div>
              <label className="mb-1 block text-xs text-[var(--muted)]">Bot token {botTokenConfigured && !telegramBotToken && <span className="text-[var(--ok)]">(configured)</span>}</label>
              <div className="flex gap-2">
                <input
                  type={showBotToken ? 'text' : 'password'}
                  value={telegramBotToken}
                  onChange={(e) => setTelegramBotToken(e.target.value)}
                  placeholder={botTokenConfigured ? '(leave empty to keep current)' : '123456:ABC-DEF1234'}
                  className="min-w-0 flex-1 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]"
                />
                <button onClick={() => setShowBotToken(!showBotToken)} aria-pressed={showBotToken} className="rounded-md border border-[var(--panel-border)] px-2 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]">
                  {showBotToken ? 'Hide' : 'Show'}
                </button>
              </div>
            </div>
            <div>
              <label className="mb-1 block text-xs text-[var(--muted)]">Chat ID</label>
              <input type="text" value={telegramChatId} onChange={(e) => setTelegramChatId(e.target.value)} placeholder="-1001234567890" className="w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]" />
            </div>
          </div>
          {telegramTestResult && <StatusMessage className={telegramTestResult.type === 'success' ? 'text-xs text-[var(--ok)]' : 'text-xs text-[var(--danger)]'}>{telegramTestResult.text}</StatusMessage>}
          <button onClick={handleTestTelegram} disabled={testingTelegram || (!telegramBotToken && !botTokenConfigured) || !telegramChatId.trim()} className="rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)] disabled:opacity-40">
            {testingTelegram ? 'Sending...' : 'Send test'}
          </button>
        </div>

        {/* Events */}
        <div className="space-y-3">
          <div>
            <div className="mb-1 text-xs font-medium uppercase tracking-wider text-[var(--muted)]">Jobs</div>
            <div className="space-y-1.5">
              <label className="flex items-center gap-2 text-xs text-[var(--text)]">
                <input type="checkbox" checked={jobFailed} onChange={(e) => setJobFailed(e.target.checked)} className="rounded" />
                Failed jobs
              </label>
              <label className="flex items-center gap-2 text-xs text-[var(--text)]">
                <input type="checkbox" checked={jobWarnings} onChange={(e) => setJobWarnings(e.target.checked)} className="rounded" />
                Succeeded with warnings
              </label>
            </div>
          </div>
          <div>
            <div className="mb-1 text-xs font-medium uppercase tracking-wider text-[var(--muted)]">Maintenance</div>
            <div className="space-y-1.5">
              <label className="flex items-center gap-2 text-xs text-[var(--text)]">
                <input type="checkbox" checked={maintenanceSucceeded} onChange={(e) => setMaintenanceSucceeded(e.target.checked)} className="rounded" />
                Maintenance succeeded
              </label>
              <label className="flex items-center gap-2 text-xs text-[var(--text)]">
                <input type="checkbox" checked={recoveryFailed} onChange={(e) => setRecoveryFailed(e.target.checked)} className="rounded" />
                Update finished but stack did not recover
              </label>
            </div>
          </div>
          <div>
            <div className="mb-1 text-xs font-medium uppercase tracking-wider text-[var(--muted)]">Runtime</div>
            <div className="space-y-1.5">
              <label className="flex items-center gap-2 text-xs text-[var(--text)]">
                <input type="checkbox" checked={runtimeHealthDegraded} onChange={(e) => setRuntimeHealthDegraded(e.target.checked)} className="rounded" />
                A stack becomes unhealthy or enters a restart loop
              </label>
              <label className="flex items-center gap-2 text-xs text-[var(--text)]">
                <input type="checkbox" checked={runtimeLogErrorBurst} onChange={(e) => setRuntimeLogErrorBurst(e.target.checked)} className="rounded" />
                A stack starts logging repeated errors
              </label>
            </div>
          </div>
          <div>
            <div className="mb-1 text-xs font-medium uppercase tracking-wider text-[var(--muted)]">Stacklab</div>
            <div className="space-y-1.5">
              <label className="flex items-center gap-2 text-xs text-[var(--text)]">
                <input type="checkbox" checked={serviceError} onChange={(e) => setServiceError(e.target.checked)} className="rounded" />
                Stacklab itself starts logging errors
              </label>
            </div>
          </div>
        </div>

        {/* Save feedback */}
        {saveResult && <StatusMessage className={saveResult.type === 'success' ? 'text-xs text-[var(--ok)]' : 'text-xs text-[var(--danger)]'}>{saveResult.text}</StatusMessage>}

        <button onClick={handleSave} disabled={savingNotif || !isDirty} className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-4 py-2 text-xs text-[var(--text)] transition hover:bg-[rgba(245,165,36,0.2)] disabled:opacity-40">
          {savingNotif ? 'Saving...' : 'Save'}
        </button>
      </div>
    </div>
  )
}

const ALL_WEEKDAYS: ScheduleWeekday[] = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun']
const WEEKDAY_LABELS: Record<ScheduleWeekday, string> = { mon: 'Mon', tue: 'Tue', wed: 'Wed', thu: 'Thu', fri: 'Fri', sat: 'Sat', sun: 'Sun' }

function describeSchedule(frequency: ScheduleFrequency, time: string, weekdays: ScheduleWeekday[]): string {
  if (frequency === 'daily') return `daily at ${time}`
  const days = weekdays.length > 0 ? weekdays.map((day) => WEEKDAY_LABELS[day]).join(', ') : 'no selected days'
  return `weekly on ${days} at ${time}`
}

function cleanedExcludedServices(excluded: Record<string, string[]>): Record<string, string[]> | undefined {
  const result: Record<string, string[]> = {}
  for (const [stackId, services] of Object.entries(excluded)) {
    const unique = Array.from(new Set(services.filter(Boolean))).sort()
    if (unique.length > 0) {
      result[stackId] = unique
    }
  }
  return Object.keys(result).length > 0 ? result : undefined
}

function filteredExcludedServices(excluded: Record<string, string[]>, stackIds: string[]): Record<string, string[]> | undefined {
  const allowed = new Set(stackIds)
  const filtered: Record<string, string[]> = {}
  for (const [stackId, services] of Object.entries(excluded)) {
    if (allowed.has(stackId)) {
      filtered[stackId] = services
    }
  }
  return cleanedExcludedServices(filtered)
}

function hasExcludedServices(excluded: Record<string, string[]>): boolean {
  return Object.values(excluded).some((services) => services.length > 0)
}

function SchedulesSection() {
  const { openJob } = useJobDrawer()
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [data, setData] = useState<MaintenanceSchedulesResponse | null>(null)
  const [stackOptions, setStackOptions] = useState<StackListItem[]>([])
  const [serviceOptions, setServiceOptions] = useState<Record<string, string[]>>({})
  const [serviceLoading, setServiceLoading] = useState<Record<string, boolean>>({})
  const [expandedServiceStacks, setExpandedServiceStacks] = useState<Set<string>>(new Set())
  const [savingSchedules, setSavingSchedules] = useState(false)
  const [saveResult, setSaveResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [confirmVolumeCleanup, setConfirmVolumeCleanup] = useState(false)

  // Update policy
  const [updateEnabled, setUpdateEnabled] = useState(false)
  const [updateFreq, setUpdateFreq] = useState<ScheduleFrequency>('weekly')
  const [updateTime, setUpdateTime] = useState('03:30')
  const [updateWeekdays, setUpdateWeekdays] = useState<ScheduleWeekday[]>(['sat'])
  const [updateTargetMode, setUpdateTargetMode] = useState<'all' | 'selected'>('all')
  const [updateTargetStacks, setUpdateTargetStacks] = useState<string[]>([])
  const [updateExcludedServices, setUpdateExcludedServices] = useState<Record<string, string[]>>({})
  const [updatePull, setUpdatePull] = useState(true)
  const [updateBuild, setUpdateBuild] = useState(true)
  const [updateOrphans, setUpdateOrphans] = useState(true)
  const [updatePrune, setUpdatePrune] = useState(false)
  const [updatePruneVol, setUpdatePruneVol] = useState(false)

  // Prune policy
  const [pruneEnabled, setPruneEnabled] = useState(false)
  const [pruneFreq, setPruneFreq] = useState<ScheduleFrequency>('weekly')
  const [pruneTime, setPruneTime] = useState('04:30')
  const [pruneWeekdays, setPruneWeekdays] = useState<ScheduleWeekday[]>(['sun'])
  const [pruneImages, setPruneImages] = useState(true)
  const [pruneBuildCache, setPruneBuildCache] = useState(true)
  const [pruneStopped, setPruneStopped] = useState(true)
  const [pruneVolumes, setPruneVolumes] = useState(false)

  const loadSchedules = useCallback(() => {
    setLoading(true)
    setLoadError(null)
    setSaveResult(null)
    Promise.allSettled([getMaintenanceSchedules(), getStacks()])
      .then(([schedulesResult, stacksResult]) => {
        if (schedulesResult.status === 'fulfilled') {
          const s = schedulesResult.value
          setData(s)
          setUpdateEnabled(s.update.enabled)
          setUpdateFreq(s.update.frequency)
          setUpdateTime(s.update.time)
          setUpdateWeekdays(s.update.weekdays ?? ['sat'])
          setUpdateTargetMode(s.update.target.mode)
          setUpdateTargetStacks(s.update.target.stack_ids ?? [])
          setUpdateExcludedServices(s.update.target.excluded_services ?? {})
          setUpdatePull(s.update.options.pull_images)
          setUpdateBuild(s.update.options.build_images)
          setUpdateOrphans(s.update.options.remove_orphans)
          setUpdatePrune(s.update.options.prune_after)
          setUpdatePruneVol(s.update.options.include_volumes)
          setPruneEnabled(s.prune.enabled)
          setPruneFreq(s.prune.frequency)
          setPruneTime(s.prune.time)
          setPruneWeekdays(s.prune.weekdays ?? ['sun'])
          setPruneImages(s.prune.scope.images)
          setPruneBuildCache(s.prune.scope.build_cache)
          setPruneStopped(s.prune.scope.stopped_containers)
          setPruneVolumes(s.prune.scope.volumes)
        } else {
          setData(null)
          setLoadError(schedulesResult.reason instanceof Error ? schedulesResult.reason.message : 'Failed to load maintenance schedules')
        }
        if (stacksResult.status === 'fulfilled') {
          setStackOptions(stacksResult.value.items)
        } else {
          setStackOptions([])
          setLoadError(stacksResult.reason instanceof Error ? stacksResult.reason.message : 'Failed to load stack list')
        }
      })
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => { loadSchedules() }, [loadSchedules])

  const visibleUpdateStackIds = useMemo(() => (
    updateTargetMode === 'selected'
      ? updateTargetStacks
      : stackOptions.map((stack) => stack.id)
  ), [stackOptions, updateTargetMode, updateTargetStacks])
  const hasVisibleExcludedServices = useMemo(() => (
    hasExcludedServices(filteredExcludedServices(updateExcludedServices, visibleUpdateStackIds) ?? {})
  ), [updateExcludedServices, visibleUpdateStackIds])
  const volumeCleanupSummary = useMemo(() => {
    const summary: string[] = []
    if (updateEnabled && updatePrune && updatePruneVol) {
      const target = updateTargetMode === 'all' ? 'all stacks' : `${updateTargetStacks.length} selected stack(s)`
      summary.push(`After stack update: ${describeSchedule(updateFreq, updateTime, updateWeekdays)}; ${target}`)
    }
    if (pruneEnabled && pruneVolumes) {
      summary.push(`Scheduled cleanup: ${describeSchedule(pruneFreq, pruneTime, pruneWeekdays)}`)
    }
    if (summary.length > 0) summary.push('Scope: unused Docker volumes and their data')
    return summary
  }, [pruneEnabled, pruneFreq, pruneTime, pruneVolumes, pruneWeekdays, updateEnabled, updateFreq, updatePrune, updatePruneVol, updateTargetMode, updateTargetStacks.length, updateTime, updateWeekdays])

  const ensureServicesLoaded = useCallback((stackId: string) => {
    if (serviceOptions[stackId] || serviceLoading[stackId]) return
    setServiceLoading((current) => ({ ...current, [stackId]: true }))
    getStack(stackId)
      .then((detail) => {
        setServiceOptions((current) => ({
          ...current,
          [detail.stack.id]: detail.stack.services.map((service) => service.name).sort(),
        }))
      })
      .catch(() => {
        setServiceOptions((current) => ({ ...current, [stackId]: [] }))
      })
      .finally(() => {
        setServiceLoading((current) => {
          const next = { ...current }
          delete next[stackId]
          return next
        })
      })
  }, [serviceLoading, serviceOptions])

  useEffect(() => {
    const visible = new Set(visibleUpdateStackIds)
    const stackIds = Object.keys(updateExcludedServices).filter((stackId) => visible.has(stackId))
    if (stackIds.length === 0) return
    setExpandedServiceStacks((current) => new Set([...current, ...stackIds]))
    for (const stackId of stackIds) {
      ensureServicesLoaded(stackId)
    }
  }, [ensureServicesLoaded, updateExcludedServices, visibleUpdateStackIds])

  const toggleServiceStack = useCallback((stackId: string) => {
    const willExpand = !expandedServiceStacks.has(stackId)
    setExpandedServiceStacks((current) => {
      const next = new Set(current)
      if (next.has(stackId)) {
        next.delete(stackId)
      } else {
        next.add(stackId)
      }
      return next
    })
    if (willExpand) {
      ensureServicesLoaded(stackId)
    }
  }, [ensureServicesLoaded, expandedServiceStacks])

  const toggleExcludedService = useCallback((stackId: string, serviceName: string, excluded: boolean) => {
    setUpdateExcludedServices((current) => {
      const existing = current[stackId] ?? []
      const nextForStack = excluded
        ? Array.from(new Set([...existing, serviceName])).sort()
        : existing.filter((item) => item !== serviceName)
      const next = { ...current }
      if (nextForStack.length > 0) {
        next[stackId] = nextForStack
      } else {
        delete next[stackId]
      }
      return next
    })
    if (excluded) {
      setUpdateOrphans(false)
    }
  }, [])

  const saveSchedules = useCallback(async () => {
    setSavingSchedules(true)
    setSaveResult(null)
    try {
      const result = await updateMaintenanceSchedules({
        update: {
          enabled: updateEnabled,
          frequency: updateFreq,
          time: updateTime,
          weekdays: updateFreq === 'weekly' ? updateWeekdays : undefined,
          target: {
            mode: updateTargetMode,
            stack_ids: updateTargetMode === 'selected' ? updateTargetStacks : undefined,
            excluded_services: filteredExcludedServices(updateExcludedServices, visibleUpdateStackIds),
          },
          options: {
            pull_images: updatePull,
            build_images: updateBuild,
            remove_orphans: updateOrphans,
            prune_after: updatePrune,
            include_volumes: updatePrune ? updatePruneVol : false,
          },
        },
        prune: {
          enabled: pruneEnabled,
          frequency: pruneFreq,
          time: pruneTime,
          weekdays: pruneFreq === 'weekly' ? pruneWeekdays : undefined,
          scope: {
            images: pruneImages,
            build_cache: pruneBuildCache,
            stopped_containers: pruneStopped,
            volumes: pruneVolumes,
          },
        },
      })
      setData(result)
      setSaveResult({ type: 'success', text: 'Saved' })
    } catch (err) {
      setSaveResult({ type: 'error', text: err instanceof Error ? err.message : 'Save failed' })
    } finally {
      setSavingSchedules(false)
    }
  }, [updateEnabled, updateFreq, updateTime, updateWeekdays, updateTargetMode, updateTargetStacks, visibleUpdateStackIds, updateExcludedServices, updatePull, updateBuild, updateOrphans, updatePrune, updatePruneVol, pruneEnabled, pruneFreq, pruneTime, pruneWeekdays, pruneImages, pruneBuildCache, pruneStopped, pruneVolumes])

  const handleSave = useCallback(() => {
    if (loadError) return
    if (updateTargetMode === 'selected' && updateTargetStacks.length === 0) {
      setSaveResult({ type: 'error', text: 'Select at least one stack for scheduled updates' })
      return
    }
    if (volumeCleanupSummary.length > 0) {
      setConfirmVolumeCleanup(true)
      return
    }
    void saveSchedules()
  }, [loadError, saveSchedules, updateTargetMode, updateTargetStacks.length, volumeCleanupSummary.length])

  if (loading) {
    return (
      <div aria-busy="true">
        <h2 className="text-sm font-medium text-[var(--text)]">Maintenance schedules</h2>
        <div className="mt-3 h-24 animate-pulse rounded-md bg-[rgba(255,255,255,0.03)]"><span className="sr-only" role="status" aria-live="polite">Loading maintenance schedules...</span></div>
      </div>
    )
  }

  if (loadError) {
    return <SettingsLoadError title="Maintenance schedules" message={loadError} onRetry={loadSchedules} />
  }

  return (
    <div>
      <h2 className="text-sm font-medium text-[var(--text)]">Maintenance schedules</h2>
      <p className="mt-1 text-xs text-[var(--muted)]">Runs in host local time. Reuses the same workflows as manual maintenance.</p>

      <div className="mt-3 max-w-lg space-y-4">
        {/* Update schedule card */}
        <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4 space-y-3">
          <label className="flex items-center gap-2 text-xs font-medium text-[var(--text)]">
            <input type="checkbox" checked={updateEnabled} onChange={(e) => setUpdateEnabled(e.target.checked)} className="rounded" />
            Scheduled stack update
          </label>

          <div className="flex flex-wrap items-center gap-2">
            <FrequencyToggle value={updateFreq} onChange={setUpdateFreq} />
            <input type="time" value={updateTime} onChange={(e) => setUpdateTime(e.target.value)} className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-2 py-1 font-mono text-xs text-[var(--text)] outline-none" />
          </div>

          {updateFreq === 'weekly' && (
            <WeekdayPicker value={updateWeekdays} onChange={setUpdateWeekdays} />
          )}

          <div className="space-y-1">
            <label className="flex items-center gap-2 text-xs text-[var(--text)]">
              <input type="radio" checked={updateTargetMode === 'all'} onChange={() => setUpdateTargetMode('all')} className="accent-[var(--accent)]" />
              All stacks
            </label>
            <label className="flex items-center gap-2 text-xs text-[var(--text)]">
              <input type="radio" checked={updateTargetMode === 'selected'} onChange={() => setUpdateTargetMode('selected')} className="accent-[var(--accent)]" />
              Selected stacks
            </label>
          </div>

          {updateTargetMode === 'selected' && (
            <div className="space-y-2 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-3">
              {stackOptions.length > 0 ? (
                <div className="space-y-1.5">
                  {stackOptions.map((stack) => (
                    <label key={stack.id} className="flex items-center justify-between gap-3 text-xs text-[var(--text)]">
                      <span className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          aria-label={stack.id}
                          checked={updateTargetStacks.includes(stack.id)}
                          onChange={(e) => setUpdateTargetStacks((current) => (
                            e.target.checked
                              ? [...current, stack.id]
                              : current.filter((id) => id !== stack.id)
                          ))}
                          className="rounded"
                        />
                        <span className="font-mono">{stack.id}</span>
                      </span>
                      <span className="text-[var(--muted)]">{stack.display_state}</span>
                    </label>
                  ))}
                </div>
              ) : (
                <p className="text-xs text-[var(--muted)]">No stacks available.</p>
              )}
            </div>
          )}

          {visibleUpdateStackIds.length > 0 && (
            <div className="space-y-2 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-3">
              <div className="text-xs font-medium text-[var(--text)]">Skip services</div>
              <div className="space-y-2">
                {visibleUpdateStackIds.map((stackId) => {
                  const expanded = expandedServiceStacks.has(stackId)
                  const services = serviceOptions[stackId] ?? []
                  return (
                    <div key={stackId} className="space-y-1">
                      <button
                        type="button"
                        aria-label={`${expanded ? 'Hide' : 'Show'} services for ${stackId}`}
                        onClick={() => toggleServiceStack(stackId)}
                        className="flex w-full items-center justify-between gap-2 rounded border border-[var(--panel-border)] px-2 py-1.5 text-left text-xs text-[var(--text)]"
                      >
                        <span className="font-mono text-xs uppercase tracking-wide text-[var(--muted)]">{stackId}</span>
                        <span className="text-xs text-[var(--muted)]">{expanded ? 'Hide' : 'Show'}</span>
                      </button>
                      {expanded && (
                        <div className="flex flex-wrap gap-2 pt-1">
                          {serviceLoading[stackId] ? (
                            <span className="text-xs text-[var(--muted)]">Loading...</span>
                          ) : services.length > 0 ? (
                            services.map((serviceName) => (
                              <label key={serviceName} className="flex items-center gap-1.5 rounded border border-[var(--panel-border)] px-2 py-1 text-xs text-[var(--text)]">
                                <input
                                  type="checkbox"
                                  checked={(updateExcludedServices[stackId] ?? []).includes(serviceName)}
                                  onChange={(e) => toggleExcludedService(stackId, serviceName, e.target.checked)}
                                  className="rounded accent-[var(--accent)]"
                                />
                                <span className="font-mono">{serviceName}</span>
                              </label>
                            ))
                          ) : (
                            <span className="text-xs text-[var(--muted)]">No services.</span>
                          )}
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </div>
          )}

          <div className="space-y-1 text-xs text-[var(--muted)]">
            <label className="flex items-center gap-2"><input type="checkbox" checked={updatePull} onChange={(e) => setUpdatePull(e.target.checked)} className="rounded" />Pull images</label>
            <label className="flex items-center gap-2"><input type="checkbox" checked={updateBuild} onChange={(e) => setUpdateBuild(e.target.checked)} className="rounded" />Build images</label>
            <label className="flex items-center gap-2"><input type="checkbox" checked={updateOrphans} disabled={hasVisibleExcludedServices} onChange={(e) => setUpdateOrphans(e.target.checked)} className="rounded" />Remove orphans</label>
            <label className="flex items-center gap-2 text-[var(--warning)]"><input type="checkbox" checked={updatePrune} onChange={(e) => { setUpdatePrune(e.target.checked); if (!e.target.checked) setUpdatePruneVol(false) }} className="rounded" />Prune after update</label>
            {updatePrune && <label className="ml-5 flex items-center gap-2 text-[var(--danger)]"><input type="checkbox" checked={updatePruneVol} onChange={(e) => setUpdatePruneVol(e.target.checked)} className="rounded" />Include volumes</label>}
          </div>

          <ScheduleStatusFooter status={data?.update.status} onOpenJob={openJob} />
        </div>

        {/* Prune schedule card */}
        <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4 space-y-3">
          <label className="flex items-center gap-2 text-xs font-medium text-[var(--text)]">
            <input type="checkbox" checked={pruneEnabled} onChange={(e) => setPruneEnabled(e.target.checked)} className="rounded" />
            Scheduled cleanup
          </label>

          <div className="flex flex-wrap items-center gap-2">
            <FrequencyToggle value={pruneFreq} onChange={setPruneFreq} />
            <input type="time" value={pruneTime} onChange={(e) => setPruneTime(e.target.value)} className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-2 py-1 font-mono text-xs text-[var(--text)] outline-none" />
          </div>

          {pruneFreq === 'weekly' && (
            <WeekdayPicker value={pruneWeekdays} onChange={setPruneWeekdays} />
          )}

          <div className="space-y-1 text-xs text-[var(--muted)]">
            <label className="flex items-center gap-2"><input type="checkbox" checked={pruneImages} onChange={(e) => setPruneImages(e.target.checked)} className="rounded" />Unused images</label>
            <label className="flex items-center gap-2"><input type="checkbox" checked={pruneBuildCache} onChange={(e) => setPruneBuildCache(e.target.checked)} className="rounded" />Build cache</label>
            <label className="flex items-center gap-2 text-[var(--warning)]"><input type="checkbox" checked={pruneStopped} onChange={(e) => setPruneStopped(e.target.checked)} className="rounded" />Stopped containers</label>
            <label className="flex items-center gap-2 text-[var(--danger)]"><input type="checkbox" checked={pruneVolumes} onChange={(e) => setPruneVolumes(e.target.checked)} className="rounded" />Unused volumes</label>
          </div>

          <ScheduleStatusFooter status={data?.prune.status} onOpenJob={openJob} />
        </div>

        {saveResult && <StatusMessage className={saveResult.type === 'success' ? 'text-xs text-[var(--ok)]' : 'text-xs text-[var(--danger)]'}>{saveResult.text}</StatusMessage>}

        <button onClick={handleSave} disabled={savingSchedules} className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-4 py-2 text-xs text-[var(--text)] transition hover:bg-[rgba(245,165,36,0.2)] disabled:opacity-40">
          {savingSchedules ? 'Saving...' : 'Save schedules'}
        </button>
      </div>

      {confirmVolumeCleanup && (
        <ConfirmDialog
          title="Enable scheduled volume deletion?"
          message="Unused Docker volumes can contain persistent application data. This cleanup will run automatically without another confirmation."
          items={volumeCleanupSummary}
          confirmLabel="Save volume cleanup"
          onConfirm={() => {
            setConfirmVolumeCleanup(false)
            void saveSchedules()
          }}
          onCancel={() => setConfirmVolumeCleanup(false)}
        />
      )}
    </div>
  )
}

function FrequencyToggle({ value, onChange }: { value: ScheduleFrequency; onChange: (v: ScheduleFrequency) => void }) {
  return (
    <div className="flex gap-1">
      {(['daily', 'weekly'] as const).map((f) => (
        <button key={f} onClick={() => onChange(f)} className={cn('rounded-md border px-2.5 py-1 text-xs transition', value === f ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}>
          {f === 'daily' ? 'Daily' : 'Weekly'}
        </button>
      ))}
    </div>
  )
}

function WeekdayPicker({ value, onChange }: { value: ScheduleWeekday[]; onChange: (v: ScheduleWeekday[]) => void }) {
  return (
    <div className="flex flex-wrap gap-1">
      {ALL_WEEKDAYS.map((d) => (
        <button
          key={d}
          onClick={() => onChange(value.includes(d) ? value.filter((w) => w !== d) : [...value, d])}
          className={cn('rounded-md border px-2 py-1 text-xs transition', value.includes(d) ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}
        >
          {WEEKDAY_LABELS[d]}
        </button>
      ))}
    </div>
  )
}

function ScheduleStatusFooter({ status, onOpenJob }: { status?: MaintenanceSchedulesResponse['update']['status']; onOpenJob: (id: string) => void }) {
  if (!status) return null

  const resultColors: Record<string, string> = { succeeded: 'text-[var(--ok)]', failed: 'text-[var(--danger)]', skipped: 'text-[var(--warning)]', running: 'text-[var(--run)]' }

  return (
    <div className="border-t border-[var(--panel-border)] pt-2 font-mono text-xs text-[var(--muted)]">
      {status.next_run_at && <div>Next: {new Date(status.next_run_at).toLocaleString()}</div>}
      {status.last_result && (
        <div className="flex items-center gap-2">
          <span>Last: <span className={resultColors[status.last_result] ?? ''}>{status.last_result}</span></span>
          {status.last_scheduled_for && <span>{new Date(status.last_scheduled_for).toLocaleString()}</span>}
          {status.last_job_id && (
            <button onClick={() => onOpenJob(status.last_job_id!)} className="text-[var(--accent)] hover:underline">View job</button>
          )}
        </div>
      )}
      {status.last_message && <div className="text-[var(--warning)]">{status.last_message}</div>}
    </div>
  )
}

function StacklabUpdateSection() {
  const { openJob } = useJobDrawer()
  const [loading, setLoading] = useState(true)
  const [overview, setOverview] = useState<StacklabUpdateOverviewResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [applying, setApplying] = useState(false)
  const [applyError, setApplyError] = useState<string | null>(null)
  const [confirmUpdate, setConfirmUpdate] = useState(false)

  const loadOverview = useCallback(async () => {
    try {
      const data = await getStacklabUpdateOverview()
      setOverview(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load update status')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadOverview() }, [loadOverview])

  const handleApply = useCallback(async () => {
    if (!overview) return
    setApplying(true)
    setApplyError(null)
    try {
      const result = await applyStacklabUpdate({
        expected_candidate_version: overview.package.candidate_version,
        refresh_package_index: true,
      })
      if (result.job?.id) {
        openJob(result.job.id)
      }
      // Refresh overview after triggering
      setTimeout(loadOverview, 2000)
    } catch (err) {
      setApplyError(err instanceof Error ? err.message : 'Update failed')
    } finally {
      setApplying(false)
    }
  }, [overview, openJob, loadOverview])

  if (loading) {
    return (
      <div aria-busy="true">
        <h2 className="text-sm font-medium text-[var(--text)]">Stacklab update</h2>
        <div className="mt-3 h-20 animate-pulse rounded-md bg-[rgba(255,255,255,0.03)]"><span className="sr-only" role="status" aria-live="polite">Loading update status...</span></div>
      </div>
    )
  }

  if (error) {
    return (
      <div>
        <h2 className="text-sm font-medium text-[var(--text)]">Stacklab update</h2>
        <p className="mt-2 text-xs text-[var(--danger)]">{error}</p>
      </div>
    )
  }

  if (!overview) return null

  const { package: pkg, write_capability: cap, runtime } = overview
  const runtimeRunning = Boolean(runtime?.job_id && !runtime.finished_at && runtime.result !== 'succeeded' && runtime.result !== 'failed')
  const isRunning = runtimeRunning || applying

  return (
    <div>
      <h2 className="text-sm font-medium text-[var(--text)]">Stacklab update</h2>

      <div className="mt-3 max-w-lg rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4 space-y-3">
        {/* Version info */}
        <div className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 font-mono text-xs">
          <span className="text-[var(--muted)]">Current</span>
          <span className="min-w-0 break-all text-[var(--text)]">{overview.current_version}</span>
          <span className="text-[var(--muted)]">Install</span>
          <span className="min-w-0 break-all text-[var(--text)]">{overview.install_mode}</span>
          {pkg.installed_version && (
            <>
              <span className="text-[var(--muted)]">Package</span>
              <span className="min-w-0 break-all text-[var(--text)]">{pkg.installed_version}</span>
            </>
          )}
          {pkg.candidate_version && pkg.candidate_version !== pkg.installed_version && (
            <>
              <span className="text-[var(--muted)]">Candidate</span>
              <span className="min-w-0 break-all text-[var(--ok)]">{pkg.candidate_version}</span>
            </>
          )}
          {pkg.configured_channel && (
            <>
              <span className="text-[var(--muted)]">Channel</span>
              <span className="min-w-0 break-all text-[var(--text)]">{pkg.configured_channel}</span>
            </>
          )}
        </div>

        {/* Update available badge */}
        {pkg.update_available && (
          <div className="flex min-w-0 items-center gap-2 text-xs">
            <span className="inline-block size-2 shrink-0 rounded-full bg-[var(--ok)]" />
            <span className="min-w-0 break-all text-[var(--ok)]">Update available: {pkg.candidate_version}</span>
          </div>
        )}
        {pkg.supported && !pkg.update_available && (
          <p className="text-xs text-[var(--muted)]">Stacklab is already up to date.</p>
        )}

        {/* Unsupported state */}
        {!pkg.supported && (
          <p className="text-xs text-[var(--warning)]">{pkg.message ?? 'Self-update is only available for APT installs.'}</p>
        )}

        {/* Write capability warning */}
        {pkg.supported && !cap.supported && (
          <p className="text-xs text-[var(--warning)]">{cap.reason ?? 'Self-update helper is not configured.'}</p>
        )}

        {/* Runtime status */}
        {runtime && (runtime.result || runtimeRunning) && (
          <div className="border-t border-[var(--panel-border)] pt-2 font-mono text-xs text-[var(--muted)]">
            <div className="flex items-center gap-2">
              <span>Last: <span className={runtime.result === 'succeeded' ? 'text-[var(--ok)]' : runtime.result === 'failed' ? 'text-[var(--danger)]' : 'text-[var(--run)]'}>{runtime.result || 'running'}</span></span>
              {runtime.finished_at && <span>{new Date(runtime.finished_at).toLocaleString()}</span>}
              {runtime.job_id && (
                <button onClick={() => openJob(runtime.job_id!)} className="text-[var(--accent)] hover:underline">View job</button>
              )}
            </div>
            {runtime.message && <div className="text-[var(--warning)]">{runtime.message}</div>}
          </div>
        )}

        {applyError && <p className="text-xs text-[var(--danger)]">{applyError}</p>}

        {/* Action */}
        {pkg.supported && cap.supported && (
          <button
            onClick={() => setConfirmUpdate(true)}
            disabled={isRunning || !pkg.update_available}
            className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-4 py-2 text-xs text-[var(--text)] transition hover:bg-[rgba(245,165,36,0.2)] disabled:opacity-40"
          >
            {isRunning ? 'Updating...' : 'Update Stacklab'}
          </button>
        )}
      </div>

      {confirmUpdate && (
        <ConfirmDialog
          title="Update Stacklab?"
          message="This installs the selected Stacklab package and restarts the Stacklab service. The UI may disconnect briefly."
          items={[
            `current: ${overview.current_version}`,
            `candidate: ${pkg.candidate_version || 'unknown'}`,
            `channel: ${pkg.configured_channel || 'unknown'}`,
          ]}
          confirmLabel="Update Stacklab"
          confirmingLabel="Updating..."
          confirming={applying}
          onCancel={() => setConfirmUpdate(false)}
          onConfirm={() => {
            void handleApply().then(() => setConfirmUpdate(false))
          }}
        />
      )}
    </div>
  )
}
