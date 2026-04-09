import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { getMeta, changePassword, getNotificationSettings, updateNotificationSettings, sendNotificationTest } from '@/lib/api-client'
import type { MetaResponse } from '@/lib/api-types'

export function SettingsPage() {
  const [meta, setMeta] = useState<MetaResponse | null>(null)
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [passwordError, setPasswordError] = useState<string | null>(null)
  const [passwordSuccess, setPasswordSuccess] = useState(false)
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
    if (newPassword.length < 4) {
      setPasswordError('Password must be at least 4 characters')
      return
    }

    setSaving(true)
    setPasswordError(null)
    setPasswordSuccess(false)
    try {
      await changePassword(currentPassword, newPassword)
      setPasswordSuccess(true)
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
    } catch (err) {
      setPasswordError(err instanceof Error ? err.message : 'Failed to change password')
    } finally {
      setSaving(false)
    }
  }, [currentPassword, newPassword, confirmPassword])

  return (
    <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <h2 className="text-3xl font-semibold tracking-[-0.04em] text-[var(--text)]">Settings</h2>

      <div className="mt-6 space-y-8">
        {/* Password */}
        <div>
          <h3 className="text-sm font-medium text-[var(--text)]">Change password</h3>
          <form onSubmit={handlePasswordChange} className="mt-3 max-w-md space-y-3">
            <input type="password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} placeholder="Current password" disabled={saving} className="w-full rounded-2xl border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-2.5 text-sm text-[var(--text)] outline-none transition focus:border-[rgba(34,197,94,0.35)] disabled:opacity-50" />
            <input type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} placeholder="New password" disabled={saving} className="w-full rounded-2xl border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-2.5 text-sm text-[var(--text)] outline-none transition focus:border-[rgba(34,197,94,0.35)] disabled:opacity-50" />
            <input type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} placeholder="Confirm new password" disabled={saving} className="w-full rounded-2xl border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-2.5 text-sm text-[var(--text)] outline-none transition focus:border-[rgba(34,197,94,0.35)] disabled:opacity-50" />
            {passwordError && <p className="text-sm text-red-400">{passwordError}</p>}
            {passwordSuccess && <p className="text-sm text-emerald-400">Password updated</p>}
            <button type="submit" disabled={saving || !currentPassword || !newPassword || !confirmPassword} className="rounded-full border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(34,197,94,0.2)] disabled:opacity-40">
              {saving ? 'Updating...' : 'Update password'}
            </button>
          </form>
        </div>

        {/* Notifications */}
        <NotificationsSection />

        {/* About */}
        {meta && (
          <div>
            <h3 className="text-sm font-medium text-[var(--text)]">About</h3>
            <div className="mt-3 grid gap-2 text-sm text-[var(--muted)]">
              <div>Stacklab {meta.app.version}</div>
              <div>Docker Engine {meta.docker.engine_version}</div>
              <div>Docker Compose {meta.docker.compose_version}</div>
              <div>Stack root: <code className="font-mono text-[var(--text)]">{meta.environment.stack_root}</code></div>
            </div>
          </div>
        )}
      </div>
    </section>
  )
}

function NotificationsSection() {
  const [loading, setLoading] = useState(true)
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

  const [savingNotif, setSavingNotif] = useState(false)
  const [saveResult, setSaveResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [webhookTestResult, setWebhookTestResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [telegramTestResult, setTelegramTestResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [testingWebhook, setTestingWebhook] = useState(false)
  const [testingTelegram, setTestingTelegram] = useState(false)

  const [savedState, setSavedState] = useState('')

  const currentState = JSON.stringify({ enabled, webhookEnabled, webhookUrl, telegramEnabled, telegramBotToken, telegramChatId, jobFailed, jobWarnings, maintenanceSucceeded, recoveryFailed })
  const isDirty = currentState !== savedState

  useEffect(() => {
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
        })
        setSavedState(state)
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const buildRequest = useCallback(() => ({
    enabled,
    webhook_url: webhookUrl,
    events: {
      job_failed: jobFailed,
      job_succeeded_with_warnings: jobWarnings,
      maintenance_succeeded: maintenanceSucceeded,
      post_update_recovery_failed: recoveryFailed,
    },
    channels: {
      webhook: { enabled: webhookEnabled, url: webhookUrl },
      telegram: { enabled: telegramEnabled, bot_token: telegramBotToken, chat_id: telegramChatId },
    },
  }), [enabled, webhookEnabled, webhookUrl, telegramEnabled, telegramBotToken, telegramChatId, jobFailed, jobWarnings, maintenanceSucceeded, recoveryFailed])

  const handleSave = useCallback(async () => {
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
  }, [buildRequest, currentState])

  const handleTestWebhook = useCallback(async () => {
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
  }, [buildRequest])

  const handleTestTelegram = useCallback(async () => {
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
  }, [buildRequest])

  if (loading) {
    return (
      <div>
        <h3 className="text-sm font-medium text-[var(--text)]">Notifications</h3>
        <div className="mt-3 h-20 animate-pulse rounded-md bg-[rgba(255,255,255,0.03)]" />
      </div>
    )
  }

  return (
    <div>
      <h3 className="text-sm font-medium text-[var(--text)]">Notifications</h3>
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
          <input type="url" value={webhookUrl} onChange={(e) => setWebhookUrl(e.target.value)} placeholder="https://hooks.example.com/stacklab" className="w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]" />
          {webhookTestResult && <p className={webhookTestResult.type === 'success' ? 'text-xs text-emerald-400' : 'text-xs text-red-400'}>{webhookTestResult.text}</p>}
          <button onClick={handleTestWebhook} disabled={testingWebhook || !webhookUrl.trim()} className="rounded-full border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)] disabled:opacity-40">
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
              <label className="mb-1 block text-[10px] text-[var(--muted)]">Bot token {botTokenConfigured && !telegramBotToken && <span className="text-emerald-400">(configured)</span>}</label>
              <div className="flex gap-2">
                <input
                  type={showBotToken ? 'text' : 'password'}
                  value={telegramBotToken}
                  onChange={(e) => setTelegramBotToken(e.target.value)}
                  placeholder={botTokenConfigured ? '(leave empty to keep current)' : '123456:ABC-DEF1234'}
                  className="flex-1 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]"
                />
                <button onClick={() => setShowBotToken(!showBotToken)} className="rounded-md border border-[var(--panel-border)] px-2 py-1 text-[10px] text-[var(--muted)] hover:text-[var(--text)]">
                  {showBotToken ? 'Hide' : 'Show'}
                </button>
              </div>
            </div>
            <div>
              <label className="mb-1 block text-[10px] text-[var(--muted)]">Chat ID</label>
              <input type="text" value={telegramChatId} onChange={(e) => setTelegramChatId(e.target.value)} placeholder="-1001234567890" className="w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]" />
            </div>
          </div>
          {telegramTestResult && <p className={telegramTestResult.type === 'success' ? 'text-xs text-emerald-400' : 'text-xs text-red-400'}>{telegramTestResult.text}</p>}
          <button onClick={handleTestTelegram} disabled={testingTelegram || (!telegramBotToken && !botTokenConfigured) || !telegramChatId.trim()} className="rounded-full border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)] disabled:opacity-40">
            {testingTelegram ? 'Sending...' : 'Send test'}
          </button>
        </div>

        {/* Events */}
        <div className="space-y-3">
          <div>
            <div className="mb-1 text-[10px] font-medium uppercase tracking-wider text-[var(--muted)]">Jobs</div>
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
            <div className="mb-1 text-[10px] font-medium uppercase tracking-wider text-[var(--muted)]">Maintenance</div>
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
        </div>

        {/* Save feedback */}
        {saveResult && <p className={saveResult.type === 'success' ? 'text-xs text-emerald-400' : 'text-xs text-red-400'}>{saveResult.text}</p>}

        <button onClick={handleSave} disabled={savingNotif || !isDirty} className="rounded-full border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-4 py-2 text-xs text-[var(--text)] transition hover:bg-[rgba(34,197,94,0.2)] disabled:opacity-40">
          {savingNotif ? 'Saving...' : 'Save'}
        </button>
      </div>
    </div>
  )
}
