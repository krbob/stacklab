import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { getMeta, changePassword } from '@/lib/api-client'
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
    <section className="rounded-[28px] border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <h2 className="text-3xl font-semibold tracking-[-0.04em] text-[var(--text)]">Settings</h2>

      <div className="mt-6 space-y-8">
        {/* Password */}
        <div>
          <h3 className="text-sm font-medium text-[var(--text)]">Change password</h3>
          <form onSubmit={handlePasswordChange} className="mt-3 max-w-md space-y-3">
            <input
              type="password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              placeholder="Current password"
              disabled={saving}
              className="w-full rounded-2xl border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-2.5 text-sm text-[var(--text)] outline-none transition focus:border-[rgba(79,209,197,0.35)] disabled:opacity-50"
            />
            <input
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              placeholder="New password"
              disabled={saving}
              className="w-full rounded-2xl border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-2.5 text-sm text-[var(--text)] outline-none transition focus:border-[rgba(79,209,197,0.35)] disabled:opacity-50"
            />
            <input
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              placeholder="Confirm new password"
              disabled={saving}
              className="w-full rounded-2xl border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-2.5 text-sm text-[var(--text)] outline-none transition focus:border-[rgba(79,209,197,0.35)] disabled:opacity-50"
            />

            {passwordError && <p className="text-sm text-red-400">{passwordError}</p>}
            {passwordSuccess && <p className="text-sm text-emerald-400">Password updated</p>}

            <button
              type="submit"
              disabled={saving || !currentPassword || !newPassword || !confirmPassword}
              className="rounded-full border border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(79,209,197,0.2)] disabled:opacity-40"
            >
              {saving ? 'Updating...' : 'Update password'}
            </button>
          </form>
        </div>

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
