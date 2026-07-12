import { useCallback, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { CircleCheck, CircleDashed, CircleX, Container, RefreshCw, Server, Wifi } from 'lucide-react'
import { getDockerAdminOverview, getReadiness } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { useWs } from '@/hooks/use-ws'
import type { DockerAdminOverviewResponse } from '@/lib/api-types'
import { cn } from '@/lib/cn'

type HealthState = 'checking' | 'healthy' | 'degraded' | 'unavailable'

const stateLabels: Record<HealthState, string> = {
  checking: 'Checking',
  healthy: 'Healthy',
  degraded: 'Degraded',
  unavailable: 'Unavailable',
}

export function SystemHealthCenter() {
  const [backendLastSuccessAt, setBackendLastSuccessAt] = useState<number | null>(null)
  const [dockerLastSuccessAt, setDockerLastSuccessAt] = useState<number | null>(null)
  const { connected, lastConnectedAt, reconnect } = useWs()

  const loadBackend = useCallback(async (signal?: AbortSignal) => {
    const result = await getReadiness(signal)
    if (!signal?.aborted && result.status === 'ok') setBackendLastSuccessAt(Date.now())
    return result
  }, [])
  const loadDocker = useCallback(async (signal?: AbortSignal) => {
    const result = await getDockerAdminOverview(signal)
    if (!signal?.aborted && result.engine.available) setDockerLastSuccessAt(Date.now())
    return result
  }, [])

  const backend = useApi(loadBackend, [loadBackend])
  const docker = useApi(loadDocker, [loadDocker])

  const backendState: HealthState = backend.data
    ? backend.data.status === 'ok' ? 'healthy' : 'degraded'
    : backend.error ? 'unavailable' : 'checking'
  const dockerState = getDockerHealthState(docker.data, docker.error)
  const websocketState: HealthState = connected
    ? 'healthy'
    : lastConnectedAt
      ? 'degraded'
      : 'checking'
  const needsAttention = [backendState, dockerState, websocketState].some((state) => state === 'degraded' || state === 'unavailable')
  const checking = !needsAttention && [backendState, dockerState, websocketState].some((state) => state === 'checking')
  const allHealthy = backendState === 'healthy' && dockerState === 'healthy' && websocketState === 'healthy'

  return (
    <section aria-labelledby="system-health-heading" className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 id="system-health-heading" className="text-lg font-medium text-[var(--text)]">System health</h2>
          <p className="mt-1 text-xs text-[var(--muted)]">Backend readiness, Docker access, and this browser's realtime connection.</p>
        </div>
        <button
          type="button"
          onClick={() => {
            backend.refetch()
            docker.refetch()
            if (!connected) reconnect()
          }}
          disabled={backend.loading || docker.loading}
          className="inline-flex items-center gap-2 rounded-md border border-[var(--panel-border)] px-3 py-1.5 text-xs text-[var(--muted)] hover:text-[var(--text)] disabled:cursor-not-allowed disabled:opacity-50"
        >
          <RefreshCw className={cn('size-3.5', (backend.loading || docker.loading) && 'animate-spin')} aria-hidden="true" />
          Check all
        </button>
      </div>

      <p className={cn('mt-4 text-sm', allHealthy ? 'text-[var(--ok)]' : needsAttention ? 'text-[var(--warning)]' : 'text-[var(--muted)]')} role="status" aria-live="polite">
        {allHealthy
          ? 'All core connections are healthy.'
          : needsAttention
            ? 'Some core connections need attention.'
            : checking
              ? 'Checking core connections…'
              : 'Core connection status is unavailable.'}
      </p>

      <div className="mt-4 grid gap-3 xl:grid-cols-3">
        <HealthCard
          title="Backend"
          icon={<Server className="size-4" aria-hidden="true" />}
          state={backendState}
          loading={backend.loading}
          error={backend.error}
          hasData={backend.data !== null}
          lastSuccessAt={backendLastSuccessAt}
          lastSuccessLabel="Last ready check"
          retryLabel="Retry backend readiness"
          onRetry={backend.refetch}
          diagnostics={<Link to="/host#stacklab-logs" className="text-[var(--accent)] hover:underline">View Stacklab logs</Link>}
        >
          {backend.data ? (
            <dl className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-4 gap-y-1">
              {Object.entries(backend.data.checks).sort(([left], [right]) => left.localeCompare(right)).map(([name, check]) => (
                <HealthDetail key={name} label={formatCheckName(name)} value={check.status === 'ok' ? 'Ready' : check.message || 'Unavailable'} unhealthy={check.status !== 'ok'} />
              ))}
              <HealthDetail label="Version" value={backend.data.version} />
            </dl>
          ) : (
            <p>{backend.error ? 'Backend readiness could not be checked.' : 'Waiting for the readiness response.'}</p>
          )}
        </HealthCard>

        <HealthCard
          title="Docker"
          icon={<Container className="size-4" aria-hidden="true" />}
          state={dockerState}
          loading={docker.loading}
          error={docker.error}
          hasData={docker.data !== null}
          lastSuccessAt={dockerLastSuccessAt}
          lastSuccessLabel="Last successful engine check"
          retryLabel="Retry Docker health"
          onRetry={docker.refetch}
          diagnostics={<Link to="/docker" className="text-[var(--accent)] hover:underline">Open Docker diagnostics</Link>}
        >
          {docker.data ? (
            <dl className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-4 gap-y-1">
              <HealthDetail
                label="Engine"
                value={docker.data.engine.available ? docker.data.engine.version || 'Available' : docker.data.engine.message || 'Unavailable'}
                unhealthy={!docker.data.engine.available}
              />
              <HealthDetail
                label="Service"
                value={docker.data.service.supported
                  ? [docker.data.service.active_state, docker.data.service.sub_state].filter(Boolean).join(' / ') || 'Unknown'
                  : 'Manager status not available'}
                unhealthy={docker.data.service.supported && docker.data.service.active_state !== 'active'}
              />
              {docker.data.engine.compose_version && <HealthDetail label="Compose" value={docker.data.engine.compose_version} />}
            </dl>
          ) : (
            <p>{docker.error ? 'Docker availability could not be checked.' : 'Waiting for the Docker overview.'}</p>
          )}
        </HealthCard>

        <HealthCard
          title="WebSocket"
          icon={<Wifi className="size-4" aria-hidden="true" />}
          state={websocketState}
          stateLabel={connected ? 'Connected' : lastConnectedAt ? 'Reconnecting' : 'Connecting'}
          loading={false}
          error={null}
          hasData={connected || lastConnectedAt !== null}
          lastSuccessAt={lastConnectedAt}
          lastSuccessLabel="Last connected"
          retryLabel="Reconnect WebSocket"
          onRetry={reconnect}
          hideRetry={connected}
          diagnostics={<Link to="/audit" className="text-[var(--accent)] hover:underline">Open audit log</Link>}
        >
          <p>
            {connected
              ? 'This browser is receiving realtime job and activity updates.'
              : 'Live streams are reconnecting automatically with backoff.'}
          </p>
        </HealthCard>
      </div>
    </section>
  )
}

function getDockerHealthState(data: DockerAdminOverviewResponse | null, error: Error | null): HealthState {
  if (!data) return error ? 'unavailable' : 'checking'
  if (!data.engine.available) return 'unavailable'
  if (data.service.supported && data.service.active_state !== 'active') return 'degraded'
  return 'healthy'
}

function HealthCard({
  title,
  icon,
  state,
  stateLabel,
  loading,
  error,
  hasData,
  lastSuccessAt,
  lastSuccessLabel,
  retryLabel,
  onRetry,
  hideRetry = false,
  diagnostics,
  children,
}: {
  title: string
  icon: ReactNode
  state: HealthState
  stateLabel?: string
  loading: boolean
  error: Error | null
  hasData: boolean
  lastSuccessAt: number | null
  lastSuccessLabel: string
  retryLabel: string
  onRetry: () => void
  hideRetry?: boolean
  diagnostics: ReactNode
  children: ReactNode
}) {
  return (
    <article aria-label={`${title} health`} aria-busy={loading} className="flex min-h-56 flex-col rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-4">
      <div className="flex items-center justify-between gap-3">
        <h3 className="flex items-center gap-2 text-sm font-medium text-[var(--text)]">{icon}{title}</h3>
        <HealthBadge state={state} label={stateLabel} />
      </div>

      {loading && hasData && <p className="mt-3 text-xs text-[var(--muted)]" role="status">Refreshing…</p>}
      {error && (
        <div className="mt-3 rounded-md border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-3 py-2 text-xs text-[var(--danger)]" role="alert">
          {error.message}
          {hasData && <span className="mt-1 block text-[var(--muted)]">Showing the last successfully loaded state.</span>}
        </div>
      )}

      <div className="mt-3 text-xs leading-5 text-[var(--muted)]">{children}</div>

      <div className="mt-auto pt-4 text-xs">
        <div className="text-[var(--muted)]">
          <span>{lastSuccessLabel}: </span>
          {lastSuccessAt ? (
            <time dateTime={new Date(lastSuccessAt).toISOString()} title={new Date(lastSuccessAt).toISOString()} className="text-[var(--text)]">
              {new Date(lastSuccessAt).toLocaleString()}
            </time>
          ) : (
            <span className="text-[var(--text)]">No successful check in this session</span>
          )}
        </div>
        <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
          {diagnostics}
          {!hideRetry && (
            <button
              type="button"
              onClick={onRetry}
              disabled={loading}
              className="rounded border border-[var(--panel-border)] px-2 py-1 text-[var(--muted)] hover:text-[var(--text)] disabled:cursor-not-allowed disabled:opacity-50"
            >
              {retryLabel}
            </button>
          )}
        </div>
      </div>
    </article>
  )
}

function HealthBadge({ state, label }: { state: HealthState; label?: string }) {
  const Icon = state === 'healthy' ? CircleCheck : state === 'checking' ? CircleDashed : CircleX
  return (
    <span className={cn(
      'inline-flex items-center gap-1.5 rounded-full border px-2 py-1 text-xs font-medium',
      state === 'healthy' && 'border-[var(--ok)]/25 bg-[var(--ok)]/10 text-[var(--ok)]',
      state === 'checking' && 'border-[var(--panel-border)] text-[var(--muted)]',
      state === 'degraded' && 'border-[var(--warning)]/25 bg-[var(--warning)]/10 text-[var(--warning)]',
      state === 'unavailable' && 'border-[var(--danger)]/25 bg-[var(--danger)]/10 text-[var(--danger)]',
    )}>
      <Icon className={cn('size-3.5', state === 'checking' && 'animate-spin')} aria-hidden="true" />
      {label ?? stateLabels[state]}
    </span>
  )
}

function HealthDetail({ label, value, unhealthy = false }: { label: string; value: string; unhealthy?: boolean }) {
  return (
    <>
      <dt>{label}</dt>
      <dd className={cn('break-all text-right font-mono text-[var(--text)]', unhealthy && 'text-[var(--danger)]')}>{value}</dd>
    </>
  )
}

function formatCheckName(value: string): string {
  return value.replaceAll('_', ' ').replace(/^./, (letter) => letter.toUpperCase())
}
