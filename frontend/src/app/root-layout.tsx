import { Activity, FolderKanban, Settings } from 'lucide-react'
import { NavLink, Outlet } from 'react-router-dom'

const links = [
  { to: '/stacks', label: 'Stacks', icon: FolderKanban },
  { to: '/audit', label: 'Audit', icon: Activity },
  { to: '/settings', label: 'Settings', icon: Settings },
]

export function RootLayout() {
  return (
    <div className="min-h-screen">
      <div className="mx-auto flex min-h-screen max-w-[1600px] gap-4 px-4 py-4 md:px-6">
        <aside className="hidden w-64 shrink-0 rounded-[28px] border border-[var(--panel-border)] bg-[var(--panel)] p-4 shadow-[var(--shadow)] lg:flex lg:flex-col">
          <div className="mb-8">
            <div className="text-xs uppercase tracking-[0.32em] text-[var(--accent)]">Stacklab</div>
            <h1 className="mt-3 text-3xl font-semibold tracking-[-0.04em] text-[var(--text)]">Control surface for Compose stacks.</h1>
          </div>

          <nav className="space-y-2">
            {links.map(({ to, label, icon: Icon }) => (
              <NavLink
                key={to}
                to={to}
                className={({ isActive }) =>
                  [
                    'flex items-center gap-3 rounded-2xl border px-4 py-3 text-sm transition',
                    isActive
                      ? 'border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] text-[var(--text)]'
                      : 'border-transparent bg-transparent text-[var(--muted)] hover:border-[var(--panel-border)] hover:bg-[rgba(255,255,255,0.03)] hover:text-[var(--text)]',
                  ].join(' ')
                }
              >
                <Icon className="size-4" />
                <span>{label}</span>
              </NavLink>
            ))}
          </nav>

          <div className="mt-auto rounded-2xl border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] p-4 text-sm text-[var(--muted)]">
            Scaffold aligned with IA, REST, WebSocket, and security docs. Ready for feature work.
          </div>
        </aside>

        <div className="flex min-w-0 flex-1 flex-col gap-4">
          <header className="rounded-[28px] border border-[var(--panel-border)] bg-[var(--panel)] px-5 py-4 shadow-[var(--shadow)]">
            <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <div className="text-xs uppercase tracking-[0.28em] text-[var(--accent)]">Preview shell</div>
                <p className="mt-2 max-w-2xl text-sm text-[var(--muted)]">
                  Host-native backend, React/Vite frontend, contract-first API, and a route structure matching the documentation.
                </p>
              </div>

              <div className="flex items-center gap-3">
                <div className="rounded-full border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)]">
                  Desktop first
                </div>
                <NavLink
                  to="/login"
                  className="rounded-full border border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(79,209,197,0.2)]"
                >
                  Login route
                </NavLink>
              </div>
            </div>
          </header>

          <main className="min-w-0 flex-1">
            <Outlet />
          </main>
        </div>
      </div>
    </div>
  )
}
