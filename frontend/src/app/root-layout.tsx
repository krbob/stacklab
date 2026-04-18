import { useEffect, useState } from 'react'
import { Activity, Container, FolderCog, FolderKanban, LogOut, Menu, Monitor, Settings, Wrench, X } from 'lucide-react'
import { NavLink, Outlet } from 'react-router-dom'
import { useAuth } from '@/hooks/use-auth'
import { GlobalActivity } from '@/components/global-activity'
import { JobDetailDrawer } from '@/components/job-detail-drawer'

const links = [
  { to: '/stacks', label: 'Stacks', icon: FolderKanban },
  { to: '/host', label: 'Host', icon: Monitor },
  { to: '/config', label: 'Config', icon: FolderCog },
  { to: '/maintenance', label: 'Maintenance', icon: Wrench },
  { to: '/docker', label: 'Docker', icon: Container },
  { to: '/audit', label: 'Audit', icon: Activity },
  { to: '/settings', label: 'Settings', icon: Settings },
]

function SidebarContent({ onNavigate, logout }: { onNavigate?: () => void; logout: () => void }) {
  return (
    <>
      <div className="mb-8">
        <div className="font-brand text-xs uppercase tracking-[0.32em] text-[var(--accent)]">Stacklab</div>
      </div>

      <nav className="space-y-1">
        {links.map(({ to, label, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            onClick={onNavigate}
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

      <div className="mt-auto space-y-1">
        <GlobalActivity />
        <button
          onClick={() => logout()}
          className="flex w-full items-center gap-3 rounded-2xl border border-transparent px-4 py-3 text-sm text-[var(--muted)] transition hover:border-[var(--panel-border)] hover:bg-[rgba(255,255,255,0.03)] hover:text-[var(--text)]"
        >
          <LogOut className="size-4" />
          <span>Log out</span>
        </button>
      </div>
    </>
  )
}

export function RootLayout() {
  const { logout } = useAuth()
  const [mobileNavOpen, setMobileNavOpen] = useState(false)

  useEffect(() => {
    if (!mobileNavOpen) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prev
    }
  }, [mobileNavOpen])

  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-30 flex items-center justify-between gap-3 border-b border-[var(--panel-border)] bg-[var(--bg)]/95 px-4 py-3 backdrop-blur lg:hidden">
        <button
          type="button"
          onClick={() => setMobileNavOpen(true)}
          aria-label="Open navigation"
          className="flex size-11 items-center justify-center rounded-2xl border border-[var(--panel-border)] text-[var(--text)]"
        >
          <Menu className="size-5" />
        </button>
        <div className="font-brand text-xs uppercase tracking-[0.32em] text-[var(--accent)]">Stacklab</div>
        <div className="size-11" aria-hidden />
      </header>

      {mobileNavOpen && (
        <>
          <div
            className="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm lg:hidden"
            onClick={() => setMobileNavOpen(false)}
            aria-hidden
          />
          <aside
            role="dialog"
            aria-modal="true"
            aria-label="Navigation"
            className="fixed inset-y-0 left-0 z-50 flex w-72 max-w-[85vw] flex-col border-r border-[var(--panel-border)] bg-[var(--panel)] p-4 shadow-xl lg:hidden"
            style={{
              paddingTop: 'max(1rem, env(safe-area-inset-top))',
              paddingBottom: 'max(1rem, env(safe-area-inset-bottom))',
              paddingLeft: 'max(1rem, env(safe-area-inset-left))',
            }}
          >
            <button
              type="button"
              onClick={() => setMobileNavOpen(false)}
              aria-label="Close navigation"
              className="absolute right-3 top-3 flex size-10 items-center justify-center rounded-lg text-[var(--muted)] hover:text-[var(--text)]"
            >
              <X className="size-5" />
            </button>
            <SidebarContent onNavigate={() => setMobileNavOpen(false)} logout={logout} />
          </aside>
        </>
      )}

      <div className="mx-auto flex min-h-screen max-w-[1600px] gap-4 px-4 py-4 md:px-6">
        <aside className="hidden w-56 shrink-0 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-4 shadow-[var(--shadow)] lg:flex lg:flex-col">
          <SidebarContent logout={logout} />
        </aside>

        <main className="flex min-w-0 flex-1 flex-col gap-4">
          <Outlet />
        </main>
      </div>

      <JobDetailDrawer />
    </div>
  )
}
