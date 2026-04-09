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
  const [webhookUrl, setWebhookUrl] = useState('')
  const [jobFailed, setJobFailed] = useState(true)
  const [jobWarnings, setJobWarnings] = useState(true)
  const [maintenanceSucceeded, setMaintenanceSucceeded] = useState(false)

  const [savingNotif, setSavingNotif] = useState(false)
  const [saveResult, setSaveResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  const [savedState, setSavedState] = useState<string>('')
  const currentState = JSON.stringify({ enabled, webhookUrl, jobFailed, jobWarnings, maintenanceSucceeded })
  const isDirty = currentState !== savedState

  useEffect(() => {
    getNotificationSettings()
      .then((settings) => {
        setEnabled(settings.enabled)
        setWebhookUrl(settings.webhook_url)
        setJobFailed(settings.events.job_failed)
        setJobWarnings(settings.events.job_succeeded_with_warnings)
        setMaintenanceSucceeded(settings.events.maintenance_succeeded)
        setSavedState(JSON.stringify({
          enabled: settings.enabled,
          webhookUrl: settings.webhook_url,
          jobFailed: settings.events.job_failed,
          jobWarnings: settings.events.job_succeeded_with_warnings,
          maintenanceSucceeded: settings.events.maintenance_succeeded,
        }))
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const handleSave = useCallback(async () => {
    setSavingNotif(true)
    setSaveResult(null)
    try {
      const result = await updateNotificationSettings({
        enabled,
        webhook_url: webhookUrl,
        events: {
          job_failed: jobFailed,
          job_succeeded_with_warnings: jobWarnings,
          maintenance_succeeded: maintenanceSucceeded,
        },
      })
      setSaveResult({ type: 'success', text: 'Saved' })
      setSavedState(JSON.stringify({
        enabled: result.enabled,
        webhookUrl: result.webhook_url,
        jobFailed: result.events.job_failed,
        jobWarnings: result.events.job_succeeded_with_warnings,
        maintenanceSucceeded: result.events.maintenance_succeeded,
      }))
    } catch (err) {
      setSaveResult({ type: 'error', text: err instanceof Error ? err.message : 'Save failed' })
    } finally {
      setSavingNotif(false)
    }
  }, [enabled, webhookUrl, jobFailed, jobWarnings, maintenanceSucceeded])

  const handleTest = useCallback(async () => {
    setTesting(true)
    setTestResult(null)
    try {
      await sendNotificationTest({
        enabled,
        webhook_url: webhookUrl,
        events: {
          job_failed: jobFailed,
          job_succeeded_with_warnings: jobWarnings,
          maintenance_succeeded: maintenanceSucceeded,
        },
      })
      setTestResult({ type: 'success', text: 'Test sent' })
    } catch (err) {
      setTestResult({ type: 'error', text: err instanceof Error ? err.message : 'Test failed' })
    } finally {
      setTesting(false)
    }
  }, [enabled, webhookUrl, jobFailed, jobWarnings, maintenanceSucceeded])

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
      <p className="mt-1 text-xs text-[var(--muted)]">
        Outgoing webhook notifications. Best-effort delivery, no retries in v1.
      </p>

      <div className="mt-3 max-w-md space-y-3">
        {/* Enable toggle */}
        <label className="flex items-center gap-2 text-sm text-[var(--text)]">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} className="rounded" />
          Enable notifications
        </label>

        {/* Webhook URL */}
        <input
          type="url"
          value={webhookUrl}
          onChange={(e) => setWebhookUrl(e.target.value)}
          placeholder="https://hooks.example.com/stacklab"
          className="w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]"
        />

        {/* Event toggles */}
        <div className="space-y-1.5">
          <label className="flex items-center gap-2 text-xs text-[var(--text)]">
            <input type="checkbox" checked={jobFailed} onChange={(e) => setJobFailed(e.target.checked)} className="rounded" />
            Failed jobs
          </label>
          <label className="flex items-center gap-2 text-xs text-[var(--text)]">
            <input type="checkbox" checked={jobWarnings} onChange={(e) => setJobWarnings(e.target.checked)} className="rounded" />
            Succeeded with warnings
          </label>
          <label className="flex items-center gap-2 text-xs text-[var(--text)]">
            <input type="checkbox" checked={maintenanceSucceeded} onChange={(e) => setMaintenanceSucceeded(e.target.checked)} className="rounded" />
            Maintenance succeeded
          </label>
        </div>

        {/* Feedback */}
        {saveResult && (
          <p className={saveResult.type === 'success' ? 'text-xs text-emerald-400' : 'text-xs text-red-400'}>
            {saveResult.text}
          </p>
        )}
        {testResult && (
          <p className={testResult.type === 'success' ? 'text-xs text-emerald-400' : 'text-xs text-red-400'}>
            {testResult.text}
          </p>
        )}

        {/* Actions */}
        <div className="flex gap-2">
          <button
            onClick={handleSave}
            disabled={savingNotif || !isDirty}
            className="rounded-full border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-4 py-2 text-xs text-[var(--text)] transition hover:bg-[rgba(34,197,94,0.2)] disabled:opacity-40"
          >
            {savingNotif ? 'Saving...' : 'Save'}
          </button>
          <button
            onClick={handleTest}
            disabled={testing || !webhookUrl.trim()}
            className="rounded-full border border-[var(--panel-border)] px-4 py-2 text-xs text-[var(--muted)] transition hover:text-[var(--text)] disabled:opacity-40"
          >
            {testing ? 'Sending...' : 'Send test'}
          </button>
        </div>
      </div>
    </div>
  )
}
