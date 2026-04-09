import { getDockerAdminOverview, getDockerDaemonConfig } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import type { DockerAdminOverviewResponse, DockerDaemonConfigResponse } from '@/lib/api-types'
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
