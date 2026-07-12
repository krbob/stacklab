import { useCallback, useState, type FormEvent } from 'react'
import { flushSync } from 'react-dom'
import { StatusMessage } from '@/components/status-message'
import { useAuth } from '@/hooks/use-auth'
import { changePassword } from '@/lib/api-client'
import { useSettingsDraft } from '@/pages/settings/settings-draft-context'

const PASSWORD_MINIMUM_LENGTH = 12
const PASSWORD_MAXIMUM_LENGTH = 256

export function PasswordSettingsSection() {
  const { requireReauthentication } = useAuth()
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [passwordError, setPasswordError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const clearDraft = useSettingsDraft('password', Boolean(currentPassword || newPassword || confirmPassword))

  const handlePasswordChange = useCallback(async (event: FormEvent) => {
    event.preventDefault()
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
      flushSync(() => {
        setCurrentPassword('')
        setNewPassword('')
        setConfirmPassword('')
        setPasswordError(null)
        clearDraft()
      })
      requireReauthentication('password_changed')
    } catch (err) {
      setPasswordError(err instanceof Error ? err.message : 'Failed to change password')
    } finally {
      setSaving(false)
    }
  }, [clearDraft, currentPassword, newPassword, confirmPassword, requireReauthentication])

  return (
    <>
      <h2 className="text-sm font-medium text-[var(--text)]">Change password</h2>
      <form onSubmit={handlePasswordChange} className="mt-3 max-w-md space-y-3">
        <input type="password" autoComplete="current-password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} placeholder="Current password" disabled={saving} className="w-full rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-2.5 text-sm text-[var(--text)] outline-none transition focus:border-[rgba(245,165,36,0.35)] disabled:opacity-50" />
        <input type="password" autoComplete="new-password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} placeholder="New password" disabled={saving} aria-describedby="new-password-requirements" className="w-full rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-2.5 text-sm text-[var(--text)] outline-none transition focus:border-[rgba(245,165,36,0.35)] disabled:opacity-50" />
        <p id="new-password-requirements" className="text-xs text-[var(--muted)]">Use {PASSWORD_MINIMUM_LENGTH}–{PASSWORD_MAXIMUM_LENGTH} characters.</p>
        <input type="password" autoComplete="new-password" value={confirmPassword} onChange={(event) => setConfirmPassword(event.target.value)} placeholder="Confirm new password" disabled={saving} className="w-full rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-2.5 text-sm text-[var(--text)] outline-none transition focus:border-[rgba(245,165,36,0.35)] disabled:opacity-50" />
        {passwordError && <StatusMessage className="text-sm text-[var(--danger)]">{passwordError}</StatusMessage>}
        <button type="submit" disabled={saving || !currentPassword || !newPassword || !confirmPassword} className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(245,165,36,0.2)] disabled:opacity-40">
          {saving ? 'Updating...' : 'Update password'}
        </button>
      </form>
    </>
  )
}
