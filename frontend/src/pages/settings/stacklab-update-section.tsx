import { useCallback, useEffect, useState } from 'react'
import { applyStacklabUpdate, getStacklabUpdateOverview } from '@/lib/api-client'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import type { StacklabUpdateOverviewResponse } from '@/lib/api-types'
import { ConfirmDialog } from '@/components/confirm-dialog'

export function StacklabUpdateSection() {
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
