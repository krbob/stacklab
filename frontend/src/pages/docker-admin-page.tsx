import { useCallback, useState } from 'react'
import { getDockerAdminOverview, getDockerDaemonConfig, validateDockerDaemonConfig } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import type { DockerAdminOverviewResponse, DockerDaemonConfigResponse, DockerDaemonValidateResponse } from '@/lib/api-types'
import { cn } from '@/lib/cn'

export function DockerAdminPage() {
  const { data: overview, error: overviewError, loading: overviewLoading } = useApi(() => getDockerAdminOverview(), [])
  const { data: daemonConfig, error: configError, loading: configLoading } = useApi(() => getDockerDaemonConfig(), [])

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
          />
        </section>
      )}
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
            <span className="text-sm text-[var(--muted)]">No daemon.json found</span>
            <p className="mt-1 text-xs text-[var(--muted)]">{daemon_config.path}</p>
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
        <p className="text-[var(--text)]">No daemon.json found</p>
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

function ManagedSettingsForm({ currentSummary, writeCapability }: {
  currentSummary: DockerAdminOverviewResponse['daemon_config']['summary']
  writeCapability: { supported: boolean; reason?: string | null; managed_keys: string[] }
}) {
  const [dns, setDns] = useState(currentSummary.dns.join(', '))
  const [mirrors, setMirrors] = useState(currentSummary.registry_mirrors.join(', '))
  const [insecure, setInsecure] = useState(currentSummary.insecure_registries.join(', '))
  const [liveRestore, setLiveRestore] = useState(currentSummary.live_restore ?? false)

  const [validating, setValidating] = useState(false)
  const [validateResult, setValidateResult] = useState<DockerDaemonValidateResponse | null>(null)
  const [validateError, setValidateError] = useState<string | null>(null)

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
            disabled
            title={writeCapability.supported ? 'Apply changes' : 'Apply is not available yet'}
            className="rounded-md border border-[var(--panel-border)] px-4 py-2 text-xs text-[var(--muted)] opacity-40"
          >
            Apply
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
    </div>
  )
}

function parseCommaSeparatedList(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}
