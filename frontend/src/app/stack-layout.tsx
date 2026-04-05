import { NavLink, Outlet, useParams } from 'react-router-dom'

import { getStack } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { StackBadge } from '@/components/stack-badge'
import { cn } from '@/lib/cn'

interface Tab {
  to: string
  label: string
  capability?: 'can_edit_definition' | 'can_view_logs' | 'can_view_stats' | 'can_open_terminal'
}

const tabs: Tab[] = [
  { to: '', label: 'Overview' },
  { to: 'editor', label: 'Editor', capability: 'can_edit_definition' },
  { to: 'logs', label: 'Logs', capability: 'can_view_logs' },
  { to: 'stats', label: 'Stats', capability: 'can_view_stats' },
  { to: 'terminal', label: 'Terminal', capability: 'can_open_terminal' },
  { to: 'audit', label: 'History' },
]

export function StackLayout() {
  const { stackId = '' } = useParams()
  const { data, error, loading, refetch } = useApi(() => getStack(stackId), [stackId])

  if (loading) {
    return (
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <div className="animate-pulse space-y-4">
          <div className="h-8 w-48 rounded bg-[rgba(255,255,255,0.05)]" />
          <div className="h-4 w-80 rounded bg-[rgba(255,255,255,0.03)]" />
          <div className="flex gap-2">
            {[1, 2, 3, 4, 5, 6].map((i) => (
              <div key={i} className="h-9 w-20 rounded-full bg-[rgba(255,255,255,0.03)]" />
            ))}
          </div>
        </div>
      </section>
    )
  }

  if (error || !data) {
    return (
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        <p className="text-sm text-red-400">
          Failed to load stack: {error?.message ?? 'Unknown error'}
        </p>
      </section>
    )
  }

  const stack = data.stack

  return (
    <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <div className="flex flex-col gap-5">
        <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
          <div>
            <div className="font-brand text-xs uppercase tracking-[0.28em] text-[var(--accent)]">Stack</div>
            <h2 className="mt-2 text-3xl font-semibold tracking-[-0.04em] text-[var(--text)]">{stack.name}</h2>
          </div>

          <StackBadge
            displayState={stack.display_state}
            configState={stack.config_state}
            activityState={stack.activity_state}
          />
        </div>

        <nav className="flex flex-wrap gap-2">
          {tabs.map(({ to, label, capability }) => {
            const disabled = capability ? !stack.capabilities[capability] : false

            if (disabled) {
              return (
                <span
                  key={label}
                  title={`${label} is not available for this stack`}
                  className="cursor-not-allowed rounded-full border border-[var(--panel-border)] px-4 py-2 text-sm text-zinc-600"
                >
                  {label}
                </span>
              )
            }

            return (
              <NavLink
                key={label}
                end={to === ''}
                to={to}
                className={({ isActive }) =>
                  cn(
                    'rounded-full border px-4 py-2 text-sm transition',
                    isActive
                      ? 'border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] text-[var(--text)]'
                      : 'border-[var(--panel-border)] text-[var(--muted)] hover:border-[rgba(34,197,94,0.25)] hover:text-[var(--text)]',
                  )
                }
              >
                {label}
              </NavLink>
            )
          })}
        </nav>

        <Outlet context={{ stack, refetch }} />
      </div>
    </section>
  )
}
