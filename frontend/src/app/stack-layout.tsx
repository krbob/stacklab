import { NavLink, Outlet, useParams } from 'react-router-dom'

import { getStack } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { AsyncState } from '@/components/async-state'
import { StackBadge } from '@/components/stack-badge'
import { cn } from '@/lib/cn'
import { useStackPageIdentity } from '@/app/stack-page-identity'

interface Tab {
  to: string
  label: string
  capability?: 'can_edit_definition' | 'can_view_logs' | 'can_view_stats' | 'can_open_terminal'
}

const tabs: Tab[] = [
  { to: '', label: 'Overview' },
  { to: 'editor', label: 'Editor', capability: 'can_edit_definition' },
  { to: 'files', label: 'Stack files' },
  { to: 'logs', label: 'Logs', capability: 'can_view_logs' },
  { to: 'stats', label: 'Stats', capability: 'can_view_stats' },
  { to: 'terminal', label: 'Terminal', capability: 'can_open_terminal' },
  { to: 'audit', label: 'History' },
]

export function StackLayout() {
  const { stackId = '' } = useParams()
  const { data, error, loading, refetch } = useApi(() => getStack(stackId), [stackId])
  const stack = data?.stack.id === stackId ? data.stack : null
  const loadError = error
    ? new Error(`Failed to load stack: ${error.message}`)
    : data && !stack
      ? new Error('Failed to load stack: Unknown error')
      : null

  useStackPageIdentity(stack ? { id: stack.id, name: stack.name } : null)

  return (
    <section aria-busy={loading} className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <div className="flex flex-col gap-5">
        <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
          <div>
            <div className="font-brand text-xs uppercase tracking-[0.28em] text-[var(--accent)]">Stack</div>
            <h1 className="mt-2 text-3xl font-semibold tracking-[-0.04em] text-[var(--text)]">
              {stack?.name ?? `Stack ${stackId}`}
            </h1>
          </div>

          {stack && (
            <StackBadge
              displayState={stack.display_state}
              configState={stack.config_state}
              activityState={stack.activity_state}
            />
          )}
        </div>

        <AsyncState
          loading={loading}
          error={loadError}
          hasData={stack !== null}
          isEmpty={false}
          loadingLabel="Loading stack…"
          emptyMessage="Stack unavailable."
          onRetry={refetch}
          loadingFallback={
            <div className="animate-pulse space-y-4">
              <div className="h-4 w-80 max-w-full rounded bg-[rgba(255,255,255,0.03)]" />
              <div className="flex gap-2 overflow-hidden">
                {[1, 2, 3, 4, 5, 6].map((i) => (
                  <div key={i} className="h-9 w-20 shrink-0 rounded-md bg-[rgba(255,255,255,0.03)]" />
                ))}
              </div>
            </div>
          }
        >
          {stack && (
            <>
              <nav
                aria-label="Stack views"
                data-testid="stack-view-tabs"
                className="sticky top-0 z-20 -mx-5 flex snap-x gap-2 overflow-x-auto border-y border-[var(--panel-border)] bg-[var(--panel)] px-5 py-3 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden md:static md:mx-0 md:flex-wrap md:border-0 md:bg-transparent md:p-0"
              >
                {tabs.map(({ to, label, capability }) => {
                  const disabled = capability ? !stack.capabilities[capability] : false

                  if (disabled) {
                    return (
                      <span
                        key={label}
                        title={`${label} is not available for this stack`}
                        className="shrink-0 snap-start cursor-not-allowed whitespace-nowrap rounded-md border border-[var(--panel-border)] px-4 py-2 text-sm text-[var(--muted)]"
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
                          'shrink-0 snap-start whitespace-nowrap rounded-md border px-4 py-2 text-sm transition',
                          isActive
                            ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                            : 'border-[var(--panel-border)] text-[var(--muted)] hover:border-[rgba(245,165,36,0.25)] hover:text-[var(--text)]',
                        )
                      }
                    >
                      {label}
                    </NavLink>
                  )
                })}
              </nav>

              <Outlet context={{ stack, refetch }} />
            </>
          )}
        </AsyncState>
      </div>
    </section>
  )
}
