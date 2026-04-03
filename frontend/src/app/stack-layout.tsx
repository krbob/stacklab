import { NavLink, Outlet, useParams } from 'react-router-dom'

const tabs = [
  { to: '', label: 'Overview' },
  { to: 'editor', label: 'Editor' },
  { to: 'logs', label: 'Logs' },
  { to: 'stats', label: 'Stats' },
  { to: 'terminal', label: 'Terminal' },
  { to: 'audit', label: 'History' },
]

export function StackLayout() {
  const { stackId = 'stack' } = useParams()

  return (
    <section className="rounded-[28px] border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <div className="flex flex-col gap-5">
        <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
          <div>
            <div className="text-xs uppercase tracking-[0.28em] text-[var(--accent)]">Stack context</div>
            <h2 className="mt-2 text-3xl font-semibold tracking-[-0.04em] text-[var(--text)]">{stackId}</h2>
            <p className="mt-2 max-w-2xl text-sm text-[var(--muted)]">
              Nested layout for stack-scoped views. Tabs and action surfaces are ready to consume capabilities and available actions from the backend.
            </p>
          </div>

          <div className="rounded-full border border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] px-4 py-2 text-sm text-[var(--text)]">
            Placeholder state: running · drifted · idle
          </div>
        </div>

        <nav className="flex flex-wrap gap-2">
          {tabs.map(({ to, label }) => (
            <NavLink
              key={label}
              end={to === ''}
              to={to}
              className={({ isActive }) =>
                [
                  'rounded-full border px-4 py-2 text-sm transition',
                  isActive
                    ? 'border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] text-[var(--text)]'
                    : 'border-[var(--panel-border)] text-[var(--muted)] hover:border-[rgba(79,209,197,0.25)] hover:text-[var(--text)]',
                ].join(' ')
              }
            >
              {label}
            </NavLink>
          ))}
        </nav>

        <Outlet />
      </div>
    </section>
  )
}
