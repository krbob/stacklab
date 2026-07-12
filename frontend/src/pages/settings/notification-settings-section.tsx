import { useCallback, useEffect, useState } from 'react'
import { StatusMessage } from '@/components/status-message'
import { getNotificationSettings, sendNotificationTest, updateNotificationSettings } from '@/lib/api-client'
import { SettingsLoadError } from '@/pages/settings/settings-card'
import { useSettingsDraft } from '@/pages/settings/settings-draft-context'

export function NotificationsSection() {
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
  useSettingsDraft('notifications', !loading && !loadError && Boolean(savedState) && isDirty)

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
