import { StatusMessage } from '@/components/status-message'
import { cn } from '@/lib/cn'
import { SettingsLoadError } from '@/pages/settings/settings-card'
import { useSettingsDraft } from '@/pages/settings/settings-draft-context'
import { useHostSettings } from '@/pages/settings/use-host-settings'

export function HostSettingsSection() {
  const {
    loading,
    loadError,
    publicIPLookupEnabled,
    setPublicIPLookupEnabled,
    savingHost,
    saveResult,
    isDirty,
    loadSettings,
    handleSave,
  } = useHostSettings()
  useSettingsDraft('host', !loading && !loadError && isDirty)

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
