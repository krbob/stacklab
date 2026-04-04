import { Link } from 'react-router-dom'

import { getStacks } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { StackBadge } from '@/components/stack-badge'

export function StacksPage() {
  const { data, error, loading } = useApi(() => getStacks(), [])

  return (
    <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div>
          <div className="text-xs uppercase tracking-[0.28em] text-[var(--accent)]">Dashboard</div>
          <h2 className="mt-2 text-3xl font-semibold tracking-[-0.04em] text-[var(--text)]">Stacks</h2>
        </div>

        <Link
          to="/stacks/new"
          className="rounded-full border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(34,197,94,0.2)]"
        >
          New stack
        </Link>
      </div>

      {data?.summary && (
        <div className="mt-4 flex flex-wrap gap-4 text-sm text-[var(--muted)]">
          <span>{data.summary.stack_count} stacks</span>
          <span className="text-emerald-400">{data.summary.running_count} running</span>
          {data.summary.stopped_count > 0 && (
            <span>{data.summary.stopped_count} stopped</span>
          )}
          {data.summary.error_count > 0 && (
            <span className="text-red-400">{data.summary.error_count} error</span>
          )}
          <span>
            {data.summary.container_count.running}/{data.summary.container_count.total} containers
          </span>
        </div>
      )}

      <div className="mt-6 grid gap-3">
        {loading && (
          <>
            {[1, 2, 3].map((i) => (
              <div key={i} className="h-20 animate-pulse rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)]" />
            ))}
          </>
        )}

        {error && (
          <div className="rounded-md border border-red-400/20 bg-red-400/5 p-5">
            <p className="text-sm text-red-400">Failed to load stacks: {error.message}</p>
          </div>
        )}

        {data?.items.map((stack) => (
          <Link
            key={stack.id}
            data-testid={`stack-card-${stack.id}`}
            to={`/stacks/${stack.id}`}
            className="flex flex-col gap-3 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-5 py-4 transition hover:border-[rgba(34,197,94,0.25)] hover:bg-[rgba(255,255,255,0.05)] md:flex-row md:items-center md:justify-between"
          >
            <div className="flex flex-col gap-1">
              <div className="text-lg font-medium text-[var(--text)]">{stack.name}</div>
              <div className="text-sm text-[var(--muted)]">
                {stack.service_count.running}/{stack.service_count.defined} services
                {stack.last_action && (
                  <> · last: {stack.last_action.action} ({stack.last_action.result})</>
                )}
              </div>
            </div>

            <StackBadge
              displayState={stack.display_state}
              configState={stack.config_state}
              activityState={stack.activity_state}
            />
          </Link>
        ))}

        {data && data.items.length === 0 && (
          <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-10 text-center">
            <p className="text-lg text-[var(--text)]">No stacks found</p>
            <p className="mt-2 text-sm text-[var(--muted)]">
              No compose.yaml files detected in /opt/stacklab/stacks/
            </p>
            <Link
              to="/stacks/new"
              className="mt-4 inline-block rounded-full border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(34,197,94,0.2)]"
            >
              Create your first stack
            </Link>
          </div>
        )}
      </div>
    </section>
  )
}
