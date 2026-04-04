import { Activity, FolderCog, FolderKanban, LogOut, Monitor, Settings, Wrench } from 'lucide-react'
import { NavLink, Outlet } from 'react-router-dom'
import { useAuth } from '@/hooks/use-auth'

const links = [
  { to: '/stacks', label: 'Stacks', icon: FolderKanban },
  { to: '/host', label: 'Host', icon: Monitor },
  { to: '/config', label: 'Config', icon: FolderCog },
  { to: '/maintenance', label: 'Maintenance', icon: Wrench },
  { to: '/audit', label: 'Audit', icon: Activity },
  { to: '/settings', label: 'Settings', icon: Settings },
]

export function RootLayout() {
  const { logout } = useAuth()

  return (
    <div className="min-h-screen">
      <div className="mx-auto flex min-h-screen max-w-[1600px] gap-4 px-4 py-4 md:px-6">
        <aside className="hidden w-56 shrink-0 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-4 shadow-[var(--shadow)] lg:flex lg:flex-col">
          <div className="mb-8">
            <div className="text-xs uppercase tracking-[0.32em] text-[var(--accent)]">Stacklab</div>
          </div>

          <nav className="space-y-1">
            {links.map(({ to, label, icon: Icon }) => (
              <NavLink
                key={to}
                to={to}
                className={({ isActive }) =>
                  [
                    'flex items-center gap-3 rounded-2xl border px-4 py-3 text-sm transition',
                    isActive
                      ? 'border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] text-[var(--text)]'
                      : 'border-transparent bg-transparent text-[var(--muted)] hover:border-[var(--panel-border)] hover:bg-[rgba(255,255,255,0.03)] hover:text-[var(--text)]',
                  ].join(' ')
                }
              >
                <Icon className="size-4" />
                <span>{label}</span>
              </NavLink>
            ))}
          </nav>

          <div className="mt-auto">
            <button
              onClick={() => logout()}
              className="flex w-full items-center gap-3 rounded-2xl border border-transparent px-4 py-3 text-sm text-[var(--muted)] transition hover:border-[var(--panel-border)] hover:bg-[rgba(255,255,255,0.03)] hover:text-[var(--text)]"
            >
              <LogOut className="size-4" />
              <span>Log out</span>
            </button>
          </div>
        </aside>

        <main className="flex min-w-0 flex-1 flex-col gap-4">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
