import { AsyncState } from '@/components/async-state'
import { useApi } from '@/hooks/use-api'
import { getMeta } from '@/lib/api-client'
import { SettingsCard } from '@/pages/settings/settings-card'

export function AboutSettingsSection() {
  const { data: meta, error, loading, refetch } = useApi(() => getMeta(), [])
  const loadError = error ? new Error(`Failed to load system information: ${error.message}`) : null

  return (
    <SettingsCard>
      <div aria-busy={loading}>
        <h2 className="text-sm font-medium text-[var(--text)]">About</h2>
        <div className="mt-3">
          <AsyncState
            loading={loading}
            error={loadError}
            hasData={meta !== null}
            isEmpty={false}
            loadingLabel="Loading system information."
            emptyMessage="System information unavailable."
            onRetry={refetch}
            retryLabel="Retry system information"
            loadingFallback={(
              <div className="space-y-2">
                <div className="h-4 w-40 animate-pulse rounded bg-[rgba(255,255,255,0.03)]" />
                <div className="h-4 w-52 animate-pulse rounded bg-[rgba(255,255,255,0.03)]" />
                <div className="h-4 w-44 animate-pulse rounded bg-[rgba(255,255,255,0.03)]" />
              </div>
            )}
          >
            {meta && (
              <div className="grid gap-2 text-sm text-[var(--muted)]">
                <div>Stacklab {meta.app.version}</div>
                <div>Docker Engine {meta.docker.engine_version}</div>
                <div>Docker Compose {meta.docker.compose_version}</div>
                <div>Stack root: <code className="font-mono text-[var(--text)]">{meta.environment.stack_root}</code></div>
              </div>
            )}
          </AsyncState>
        </div>
      </div>
    </SettingsCard>
  )
}
