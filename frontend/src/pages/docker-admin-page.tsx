import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  getDockerAdminOverview,
  getDockerDaemonConfig,
  getDockerRegistryStatus,
  validateDockerDaemonConfig,
  applyDockerDaemonConfig,
  loginDockerRegistry,
  logoutDockerRegistry,
} from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { useJobStream } from '@/hooks/use-job-stream'
import { StepCards } from '@/components/step-cards'
import type {
  DockerAdminOverviewResponse,
  DockerDaemonConfigResponse,
  DockerDaemonValidateResponse,
  DockerRegistryStatusResponse,
} from '@/lib/api-types'
import { cn } from '@/lib/cn'

export function DockerAdminPage() {
  const { data: overview, error: overviewError, loading: overviewLoading, refetch: refetchOverview } = useApi(() => getDockerAdminOverview(), [])
  const { data: daemonConfig, error: configError, loading: configLoading, refetch: refetchConfig } = useApi(() => getDockerDaemonConfig(), [])
  const { data: registryStatus, error: registryError, loading: registryLoading, refetch: refetchRegistry } = useApi(() => getDockerRegistryStatus(), [])

  const handleApplyDone = useCallback(() => {
    refetchOverview()
    refetchConfig()
  }, [refetchOverview, refetchConfig])

  return (
    <div className="flex flex-col gap-4">
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <h2 className="text-3xl font-semibold tracking-[-0.04em] text-[var(--text)]">Docker</h2>

        {overviewLoading && (
          <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {[1, 2, 3].map((i) => (
              <div key={i} className="h-36 animate-pulse rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />
            ))}
          </div>
        )}

        {overviewError && (
          <div className="mt-4 rounded-2xl border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">
            Failed to load Docker overview: {overviewError.message}
          </div>
        )}

        {overview && <OverviewCards overview={overview} />}
      </section>

      {/* Daemon config viewer */}
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <h3 className="text-lg font-medium text-[var(--text)]">daemon.json</h3>

        {configLoading && <div className="mt-3 h-48 animate-pulse rounded-md bg-[rgba(255,255,255,0.02)]" />}
        {configError && <p className="mt-3 text-sm text-red-400">{configError.message}</p>}
        {daemonConfig && <DaemonConfigViewer config={daemonConfig} />}
      </section>

      {/* Managed settings */}
      {overview && (
        <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
          <ManagedSettingsForm
            currentSummary={overview.daemon_config.summary}
            writeCapability={overview.daemon_config.write_capability ?? overview.write_capability}
            onApplyDone={handleApplyDone}
          />
        </section>
      )}

      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <RegistryAuthSection
          status={registryStatus}
          loading={registryLoading}
          error={registryError}
          refetch={refetchRegistry}
        />
      </section>
    </div>
  )
}

function OverviewCards({ overview }: { overview: DockerAdminOverviewResponse }) {
  const { service, engine, daemon_config } = overview

  return (
    <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
      {/* Service status */}
      <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
        <div className="font-brand text-xs uppercase tracking-wider text-[var(--accent)]">Service</div>
        {service.supported ? (
          <>
            <div className="mt-2 flex items-center gap-2">
              <span className={cn(
                'inline-block size-2 rounded-full',
                service.active_state === 'active' ? 'bg-emerald-400' : 'bg-zinc-500',
              )} />
              <span className="text-lg font-medium text-[var(--text)]">{service.active_state || 'unknown'}</span>
            </div>
            <div className="mt-1 space-y-0.5 font-mono text-xs text-[var(--muted)]">
              <div>Unit: {service.unit_name}</div>
              <div>Load: {service.load_state}</div>
              <div>Sub: {service.sub_state}</div>
              {service.unit_file_state && <div>File: {service.unit_file_state}</div>}
              {service.started_at && <div>Started: {new Date(service.started_at).toLocaleString()}</div>}
            </div>
          </>
        ) : (
          <div className="mt-2">
            <span className="text-sm text-amber-400">Not available</span>
            <p className="mt-1 text-xs text-[var(--muted)]">
              {service.message || 'systemd service status is not supported on this host.'}
            </p>
          </div>
        )}
      </div>

      {/* Engine */}
      <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
        <div className="font-brand text-xs uppercase tracking-wider text-[var(--accent)]">Engine</div>
        {engine.available ? (
          <>
            <div className="mt-2 text-lg font-medium text-[var(--text)]">{engine.version}</div>
            <div className="mt-1 space-y-0.5 font-mono text-xs text-[var(--muted)]">
              <div>API: {engine.api_version}</div>
              <div>Compose: {engine.compose_version}</div>
              <div>Driver: {engine.driver}</div>
              <div>Logging: {engine.logging_driver}</div>
              <div>Cgroup: {engine.cgroup_driver}</div>
              <div>Root: {engine.root_dir}</div>
            </div>
          </>
        ) : (
          <div className="mt-2">
            <span className="text-sm text-red-400">Unavailable</span>
            {engine.message && <p className="mt-1 text-xs text-[var(--muted)]">{engine.message}</p>}
          </div>
        )}
      </div>

      {/* Daemon config summary */}
      <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
        <div className="font-brand text-xs uppercase tracking-wider text-[var(--accent)]">Configuration</div>
        {daemon_config.exists ? (
          <>
            <div className="mt-2 flex items-center gap-2">
              <span className={cn(
                'inline-block size-2 rounded-full',
                daemon_config.valid_json ? 'bg-emerald-400' : 'bg-red-400',
              )} />
              <span className="text-sm text-[var(--text)]">{daemon_config.valid_json ? 'Valid JSON' : 'Invalid JSON'}</span>
            </div>
            <div className="mt-2 space-y-0.5 font-mono text-xs text-[var(--muted)]">
              {daemon_config.summary.dns.length > 0 && (
                <div>DNS: {daemon_config.summary.dns.join(', ')}</div>
              )}
              {daemon_config.summary.registry_mirrors.length > 0 && (
                <div>Mirrors: {daemon_config.summary.registry_mirrors.join(', ')}</div>
              )}
              {daemon_config.summary.log_driver && (
                <div>Log driver: {daemon_config.summary.log_driver}</div>
              )}
              {daemon_config.summary.live_restore != null && (
                <div>Live restore: {daemon_config.summary.live_restore ? 'yes' : 'no'}</div>
              )}
              <div className="mt-1 text-zinc-600">{daemon_config.configured_keys.length} keys configured</div>
            </div>
          </>
        ) : (
          <div className="mt-2">
            <span className="text-sm text-[var(--muted)]">Docker is using defaults</span>
            <p className="mt-1 text-xs text-[var(--muted)]">No <span className="font-mono">{daemon_config.path}</span> file was found.</p>
          </div>
        )}
      </div>
    </div>
  )
}

function DaemonConfigViewer({ config }: { config: DockerDaemonConfigResponse }) {
  if (!config.exists) {
    return (
      <div className="mt-3 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-8 text-center">
        <p className="text-[var(--text)]">Docker is currently using built-in defaults.</p>
        <p className="mt-1 text-xs text-[var(--muted)]">No daemon config file was found at:</p>
        <p className="mt-1 font-mono text-xs text-[var(--muted)]">{config.path}</p>
      </div>
    )
  }

  return (
    <div className="mt-3 space-y-3">
      {/* Metadata */}
      <div className="flex flex-wrap gap-4 text-xs text-[var(--muted)]">
        <span className="font-mono">{config.path}</span>
        {config.permissions && (
          <>
            <span>{config.permissions.owner_name}:{config.permissions.group_name}</span>
            <span>{config.permissions.mode}</span>
          </>
        )}
        {config.modified_at && <span>{new Date(config.modified_at).toLocaleString()}</span>}
      </div>

      {!config.valid_json && config.parse_error && (
        <div className="rounded-md border border-red-400/20 bg-red-400/5 px-4 py-2 text-xs text-red-400">
          Parse error: {config.parse_error}
        </div>
      )}

      {/* Raw JSON */}
      {config.content && (
        <div className="overflow-auto rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.3)] p-4 font-mono text-xs leading-5 text-[var(--text)]">
          <pre className="whitespace-pre-wrap">{config.content}</pre>
        </div>
      )}

      {/* Key summary */}
      {config.configured_keys.length > 0 && (
        <div className="text-xs text-[var(--muted)]">
          Configured keys: <span className="font-mono text-[var(--text)]">{config.configured_keys.join(', ')}</span>
        </div>
      )}
    </div>
  )
}

function ManagedSettingsForm({ currentSummary, writeCapability, onApplyDone }: {
  currentSummary: DockerAdminOverviewResponse['daemon_config']['summary']
  writeCapability: { supported: boolean; reason?: string | null; managed_keys: string[] }
  onApplyDone: () => void
}) {
  const [dns, setDns] = useState(currentSummary.dns.join(', '))
  const [mirrors, setMirrors] = useState(currentSummary.registry_mirrors.join(', '))
  const [insecure, setInsecure] = useState(currentSummary.insecure_registries.join(', '))
  const [liveRestore, setLiveRestore] = useState(currentSummary.live_restore ?? false)

  const [validating, setValidating] = useState(false)
  const [validateResult, setValidateResult] = useState<DockerDaemonValidateResponse | null>(null)
  const [validateError, setValidateError] = useState<string | null>(null)

  const [applyJobId, setApplyJobId] = useState<string | null>(null)
  const [applying, setApplying] = useState(false)
  const [applyError, setApplyError] = useState<string | null>(null)

  const { events: applyEvents, state: applyJobState } = useJobStream({ jobId: applyJobId })
  const applyTerminal = applyJobState === 'succeeded' || applyJobState === 'failed' || applyJobState === 'cancelled' || applyJobState === 'timed_out'
  const applyInProgress = Boolean(applyJobId) && !applyTerminal

  const applyBackupPath = useMemo(
    () =>
      applyEvents.find(
        (event) => event.event === 'job_log' && event.message === 'Created Docker daemon config backup.' && event.data,
      )?.data ?? null,
    [applyEvents],
  )
  const applyRollbackAttempted = useMemo(
    () =>
      applyEvents.some(
        (event) =>
          event.event === 'job_warning' && /rollback/i.test(event.message),
      ),
    [applyEvents],
  )

  useEffect(() => {
    setValidateResult(null)
    setValidateError(null)
  }, [dns, mirrors, insecure, liveRestore])

  // Auto-refresh on apply completion
  useEffect(() => {
    if (applyTerminal && applyJobId) {
      onApplyDone()
    }
  }, [applyTerminal, applyJobId, onApplyDone])

  const handleValidate = useCallback(async () => {
    setValidating(true)
    setValidateError(null)
    setValidateResult(null)
    try {
      const dnsList = parseCommaSeparatedList(dns)
      const mirrorsList = parseCommaSeparatedList(mirrors)
      const insecureList = parseCommaSeparatedList(insecure)
      const removeKeys: string[] = []
      if (dnsList.length === 0) removeKeys.push('dns')
      if (mirrorsList.length === 0) removeKeys.push('registry_mirrors')
      if (insecureList.length === 0) removeKeys.push('insecure_registries')

      const result = await validateDockerDaemonConfig({
        settings: {
          dns: dnsList,
          registry_mirrors: mirrorsList,
          insecure_registries: insecureList,
          live_restore: liveRestore,
        },
        remove_keys: removeKeys,
      })
      setValidateResult(result)
    } catch (err) {
      setValidateError(err instanceof Error ? err.message : 'Validation failed')
    } finally {
      setValidating(false)
    }
  }, [dns, mirrors, insecure, liveRestore])

  const handleApply = useCallback(async () => {
    setApplying(true)
    setApplyError(null)
    setApplyJobId(null)
    try {
      const dnsList = parseCommaSeparatedList(dns)
      const mirrorsList = parseCommaSeparatedList(mirrors)
      const insecureList = parseCommaSeparatedList(insecure)
      const removeKeys: string[] = []
      if (dnsList.length === 0) removeKeys.push('dns')
      if (mirrorsList.length === 0) removeKeys.push('registry_mirrors')
      if (insecureList.length === 0) removeKeys.push('insecure_registries')

      const result = await applyDockerDaemonConfig({
        settings: {
          dns: dnsList,
          registry_mirrors: mirrorsList,
          insecure_registries: insecureList,
          live_restore: liveRestore,
        },
        remove_keys: removeKeys,
      })
      setApplyJobId(result.job.id)
      setValidateResult(null) // Clear preview since we're applying
    } catch (err) {
      setApplyError(err instanceof Error ? err.message : 'Apply failed')
    } finally {
      setApplying(false)
    }
  }, [dns, mirrors, insecure, liveRestore])

  const canApply = writeCapability.supported && Boolean(validateResult) && !applying && !applyInProgress

  return (
    <div>
      <h3 className="text-lg font-medium text-[var(--text)]">Managed settings</h3>

      {/* Preview mode banner */}
      {!writeCapability.supported && (
        <div className="mt-2 rounded-md border border-amber-400/20 bg-amber-400/5 px-4 py-2 text-xs text-amber-400">
          Preview mode — changes can be validated but not applied yet.
          {writeCapability.reason && <span className="ml-1 text-[var(--muted)]">{writeCapability.reason}</span>}
        </div>
      )}

      {/* Form */}
      <div className="mt-4 max-w-lg space-y-3">
        <label className="block">
          <span className="mb-1 block text-xs text-[var(--muted)]">DNS servers (comma-separated)</span>
          <input
            type="text"
            value={dns}
            onChange={(e) => setDns(e.target.value)}
            placeholder="192.168.1.2, 8.8.8.8"
            className="w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]"
          />
        </label>

        <label className="block">
          <span className="mb-1 block text-xs text-[var(--muted)]">Registry mirrors (comma-separated)</span>
          <input
            type="text"
            value={mirrors}
            onChange={(e) => setMirrors(e.target.value)}
            placeholder="https://mirror.local"
            className="w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]"
          />
        </label>

        <label className="block">
          <span className="mb-1 block text-xs text-[var(--muted)]">Insecure registries (comma-separated)</span>
          <input
            type="text"
            value={insecure}
            onChange={(e) => setInsecure(e.target.value)}
            placeholder="registry.local:5000"
            className="w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]"
          />
        </label>

        <label className="flex items-center gap-2 text-xs text-[var(--text)]">
          <input type="checkbox" checked={liveRestore} onChange={(e) => setLiveRestore(e.target.checked)} className="rounded" />
          Enable live restore
        </label>

        <div className="flex gap-2 pt-2">
          <button
            onClick={handleValidate}
            disabled={validating}
            className="rounded-md border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-4 py-2 text-xs text-[var(--text)] transition hover:bg-[rgba(34,197,94,0.2)] disabled:opacity-40"
          >
            {validating ? 'Validating...' : 'Validate'}
          </button>

          <button
            onClick={handleApply}
            disabled={!canApply}
            title={!writeCapability.supported ? 'Apply is not available yet' : !validateResult ? 'Validate first' : 'Apply changes and restart Docker'}
            className={cn(
              'rounded-md border px-4 py-2 text-xs transition',
              canApply
                ? 'border-red-400/30 bg-red-400/10 text-red-400 hover:bg-red-400/20'
                : 'border-[var(--panel-border)] text-[var(--muted)] opacity-40',
            )}
          >
            {applying ? 'Applying...' : 'Apply & Restart'}
          </button>
        </div>
      </div>

      {/* Validate error */}
      {validateError && (
        <div className="mt-3 rounded-md border border-red-400/20 bg-red-400/5 px-4 py-2 text-xs text-red-400">
          {validateError}
        </div>
      )}

      {/* Validate result */}
      {validateResult && (
        <div className="mt-4 space-y-3">
          <div className="flex items-center gap-2 text-xs">
            <span className={cn('inline-block size-2 rounded-full', validateResult.warnings.length > 0 ? 'bg-amber-400' : 'bg-emerald-400')} />
            <span className="text-[var(--text)]">
              Validation {validateResult.warnings.length > 0 ? 'passed with warnings' : 'passed'}
            </span>
            {validateResult.requires_restart && (
              <span className="text-amber-400">· Requires Docker restart</span>
            )}
          </div>

          {validateResult.changed_keys.length > 0 && (
            <div className="text-xs text-[var(--muted)]">
              Changed keys: <span className="font-mono text-[var(--text)]">{validateResult.changed_keys.join(', ')}</span>
            </div>
          )}

          {validateResult.warnings.map((w, i) => (
            <div key={i} className="text-xs text-amber-400">{w}</div>
          ))}

          {/* Preview content */}
          <div>
            <div className="mb-1 text-xs text-[var(--muted)]">Resulting daemon.json:</div>
            <div className="overflow-auto rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.3)] p-4 font-mono text-xs leading-5 text-[var(--text)]">
              <pre className="whitespace-pre-wrap">{validateResult.preview.content}</pre>
            </div>
          </div>
        </div>
      )}

      {/* Apply error */}
      {applyError && (
        <div className="mt-3 rounded-md border border-red-400/20 bg-red-400/5 px-4 py-2 text-xs text-red-400">
          {applyError}
        </div>
      )}

      {/* Apply progress */}
      {applyJobId && (
        <div className="mt-4 space-y-3">
          <h4 className="text-sm font-medium text-[var(--text)]">Apply progress</h4>

          <div className="flex items-center gap-2 text-xs">
            {!applyTerminal && <span className="inline-block size-2 animate-pulse rounded-full bg-sky-400" />}
            <span className={cn(
              'font-medium',
              applyJobState === 'running' ? 'text-sky-400' :
              applyJobState === 'succeeded' ? 'text-emerald-400' :
              applyJobState === 'failed' ? 'text-red-400' :
              'text-[var(--muted)]',
            )}>
              {applyJobState === 'running' ? 'Applying...' :
               applyJobState === 'succeeded' ? 'Applied successfully' :
               applyJobState === 'failed' ? 'Apply failed' :
               applyJobState ?? 'Starting'}
            </span>
          </div>

          <StepCards events={applyEvents} />

          {/* Result card */}
          {applyJobState === 'succeeded' && (
            <div className="rounded-md border border-emerald-400/20 bg-emerald-400/5 px-4 py-3 text-xs text-emerald-400">
              Docker configuration applied and Docker restarted successfully.
              {applyBackupPath && (
                <div className="mt-1 text-[var(--muted)]">
                  Backup: <span className="font-mono text-emerald-300">{applyBackupPath}</span>
                </div>
              )}
            </div>
          )}
          {applyJobState === 'failed' && (
            <div className="rounded-md border border-amber-400/20 bg-amber-400/5 px-4 py-3 text-xs text-amber-400">
              {applyRollbackAttempted
                ? 'Apply failed. A rollback was attempted; the previous configuration should be restored.'
                : 'Apply failed. Check the step details above before retrying.'}
              {applyBackupPath && (
                <div className="mt-1 text-[var(--muted)]">
                  Backup: <span className="font-mono text-amber-300">{applyBackupPath}</span>
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function parseCommaSeparatedList(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function RegistryAuthSection({
  status,
  loading,
  error,
  refetch,
}: {
  status: DockerRegistryStatusResponse | null
  loading: boolean
  error: Error | null
  refetch: () => void
}) {
  const [registry, setRegistry] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [activeJobId, setActiveJobId] = useState<string | null>(null)
  const [activeAction, setActiveAction] = useState<'login' | 'logout' | null>(null)

  const { events, state } = useJobStream({ jobId: activeJobId })
  const terminal = state === 'succeeded' || state === 'failed' || state === 'cancelled' || state === 'timed_out'

  useEffect(() => {
    if (!terminal || !activeJobId) return
    refetch()
    if (state === 'succeeded' && activeAction === 'login') {
      setPassword('')
    }
  }, [terminal, activeJobId, activeAction, state, refetch])

  const handleLogin = useCallback(async () => {
    setSubmitting(true)
    setSubmitError(null)
    setActiveJobId(null)
    setActiveAction('login')
    try {
      const result = await loginDockerRegistry({ registry, username, password })
      setActiveJobId(result.job.id)
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : 'Login failed')
      setActiveAction(null)
    } finally {
      setSubmitting(false)
    }
  }, [registry, username, password])

  const handleLogout = useCallback(async (targetRegistry: string) => {
    setSubmitting(true)
    setSubmitError(null)
    setActiveJobId(null)
    setActiveAction('logout')
    try {
      const result = await logoutDockerRegistry({ registry: targetRegistry })
      setActiveJobId(result.job.id)
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : 'Logout failed')
      setActiveAction(null)
    } finally {
      setSubmitting(false)
    }
  }, [])

  const latestLogLine = useMemo(() => {
    for (let i = events.length - 1; i >= 0; i -= 1) {
      const event = events[i]
      if (event.event === 'job_error') return event.message
      if (event.event === 'job_log' && event.message) return event.message + (event.data ? ` ${event.data}` : '')
    }
    return null
  }, [events])

  const actionInProgress = Boolean(activeJobId) && !terminal
  const canSubmitLogin = registry.trim().length > 0 && username.trim().length > 0 && password.length > 0 && !submitting && !actionInProgress

  return (
    <div>
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="text-lg font-medium text-[var(--text)]">Registry auth</h3>
          <p className="mt-1 text-xs text-[var(--muted)]">
            Authenticate Docker pulls and builds against private registries using Stacklab&apos;s effective Docker client config.
          </p>
        </div>
        <button
          onClick={refetch}
          className="rounded-md border border-[var(--panel-border)] px-3 py-1.5 text-xs text-[var(--text)] transition hover:bg-[rgba(255,255,255,0.05)]"
        >
          Refresh
        </button>
      </div>

      {loading && <div className="mt-3 h-32 animate-pulse rounded-md bg-[rgba(255,255,255,0.02)]" />}

      {error && (
        <div className="mt-3 rounded-md border border-red-400/20 bg-red-400/5 px-4 py-2 text-xs text-red-400">
          Failed to load Docker registry auth status: {error.message}
        </div>
      )}

      {status && (
        <div className="mt-4 space-y-4">
          <div className="text-xs text-[var(--muted)]">
            Docker config: <span className="font-mono text-[var(--text)]">{status.docker_config_path}</span>
          </div>

          {!status.exists && (
            <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-4 py-3 text-xs text-[var(--muted)]">
              No Docker client config exists yet. The first successful login will create it.
            </div>
          )}

          {status.exists && !status.valid_json && status.parse_error && (
            <div className="rounded-md border border-red-400/20 bg-red-400/5 px-4 py-3 text-xs text-red-400">
              Docker config is invalid JSON: {status.parse_error}
            </div>
          )}

          <div className="space-y-2">
            <div className="text-xs uppercase tracking-wider text-[var(--accent)]">Configured registries</div>
            {status.items.length === 0 ? (
              <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-4 py-6 text-sm text-[var(--muted)]">
                No registry credentials configured.
              </div>
            ) : (
              <div className="space-y-2">
                {status.items.map((item) => (
                  <div
                    key={item.registry}
                    className="flex flex-col gap-3 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-3 md:flex-row md:items-center md:justify-between"
                  >
                    <div className="min-w-0">
                      <div className="font-mono text-sm text-[var(--text)]">{item.registry}</div>
                      <div className="mt-1 text-xs text-[var(--muted)]">
                        {item.username ? `Username: ${item.username}` : 'Username unavailable'} · Source: {item.source}
                      </div>
                      {item.last_error && (
                        <div className="mt-1 text-xs text-amber-400">{item.last_error}</div>
                      )}
                    </div>
                    <button
                      onClick={() => handleLogout(item.registry)}
                      disabled={submitting || actionInProgress}
                      className="rounded-md border border-red-400/30 px-3 py-1.5 text-xs text-red-400 transition hover:bg-red-400/10 disabled:opacity-40"
                    >
                      Logout
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="space-y-3 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
            <div className="text-xs uppercase tracking-wider text-[var(--accent)]">Login</div>

            <div className="grid gap-3 md:grid-cols-2">
              <label className="block">
                <span className="mb-1 block text-xs text-[var(--muted)]">Registry</span>
                <input
                  type="text"
                  value={registry}
                  onChange={(e) => setRegistry(e.target.value)}
                  placeholder="ghcr.io"
                  className="w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]"
                />
              </label>

              <label className="block">
                <span className="mb-1 block text-xs text-[var(--muted)]">Username</span>
                <input
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="bob"
                  className="w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]"
                />
              </label>
            </div>

            <label className="block">
              <span className="mb-1 block text-xs text-[var(--muted)]">Password or token</span>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="Token or password"
                className="w-full rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)]"
              />
            </label>

            <div className="flex items-center gap-2">
              <button
                onClick={handleLogin}
                disabled={!canSubmitLogin}
                className="rounded-md border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-4 py-2 text-xs text-[var(--text)] transition hover:bg-[rgba(34,197,94,0.2)] disabled:opacity-40"
              >
                {submitting && activeAction === 'login' ? 'Logging in...' : 'Login'}
              </button>
              <span className="text-xs text-[var(--muted)]">Uses the same Docker config path as Stacklab&apos;s Compose operations.</span>
            </div>
          </div>

          {submitError && (
            <div className="rounded-md border border-red-400/20 bg-red-400/5 px-4 py-2 text-xs text-red-400">
              {submitError}
            </div>
          )}

          {activeJobId && (
            <div className="space-y-3">
              <div className="flex items-center gap-2 text-xs">
                {!terminal && <span className="inline-block size-2 animate-pulse rounded-full bg-sky-400" />}
                <span className={cn(
                  'font-medium',
                  state === 'running' ? 'text-sky-400' :
                  state === 'succeeded' ? 'text-emerald-400' :
                  state === 'failed' ? 'text-red-400' :
                  'text-[var(--muted)]',
                )}>
                  {state === 'running'
                    ? activeAction === 'logout' ? 'Logging out...' : 'Logging in...'
                    : state === 'succeeded'
                      ? activeAction === 'logout' ? 'Logged out' : 'Login succeeded'
                      : state === 'failed'
                        ? activeAction === 'logout' ? 'Logout failed' : 'Login failed'
                        : state ?? 'Starting'}
                </span>
              </div>

              <StepCards events={events} />

              {terminal && latestLogLine && (
                <div className={cn(
                  'rounded-md border px-4 py-2 text-xs',
                  state === 'succeeded'
                    ? 'border-emerald-400/20 bg-emerald-400/5 text-emerald-400'
                    : 'border-red-400/20 bg-red-400/5 text-red-400',
                )}>
                  {latestLogLine}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
