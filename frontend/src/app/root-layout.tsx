import { useEffect, useState } from 'react'
import { Activity, Container, Ellipsis, FolderCog, FolderKanban, LogOut, Monitor, Settings, Wrench, X } from 'lucide-react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useAuth } from '@/hooks/use-auth'
import { GlobalActivity } from '@/components/global-activity'
import { JobDetailDrawer } from '@/components/job-detail-drawer'
import { HostStrip } from '@/components/host-strip'
import { ActivityProvider } from '@/contexts/activity-context'

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
        {links.map(({ to, label, icon: Icon }, index) => (
          <NavLink
            key={to}
            to={to}
            onClick={onNavigate}
            className={({ isActive }) =>
              [
                'flex items-center gap-3 rounded-lg border px-4 py-3 text-sm transition',
                isActive
                  ? 'border-[var(--panel-border)] bg-[rgba(245,165,36,0.10)] text-[var(--text)] shadow-[inset_2px_0_0_var(--accent)]'
                  : 'border-transparent bg-transparent text-[var(--muted)] hover:border-[var(--panel-border)] hover:bg-[rgba(255,255,255,0.03)] hover:text-[var(--text)]',
              ].join(' ')
            }
          >
            <Icon className="size-4" />
            <span>{label}</span>
            <kbd className="ml-auto rounded border border-[rgba(255,255,255,0.1)] px-1 font-mono text-[9px] text-[var(--dim,#6E6757)]">{index + 1}</kbd>
          </NavLink>
        ))}
      </nav>

      <div className="mt-auto space-y-1">
        <GlobalActivity />
        <button
          onClick={() => logout()}
          className="flex w-full items-center gap-3 rounded-lg border border-transparent px-4 py-3 text-sm text-[var(--muted)] transition hover:border-[var(--panel-border)] hover:bg-[rgba(255,255,255,0.03)] hover:text-[var(--text)]"
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
  const navigate = useNavigate()
  const [mobileNavOpen, setMobileNavOpen] = useState(false)

  // Nav hotkeys 1-7 (Z5); skipped while typing.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.metaKey || e.ctrlKey || e.altKey) return
      const target = e.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) return
      const index = Number.parseInt(e.key, 10) - 1
      if (Number.isNaN(index) || index < 0 || index >= links.length) return
      e.preventDefault()
      navigate(links[index].to)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [navigate])

  useEffect(() => {
    if (!mobileNavOpen) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prev
    }
  }, [mobileNavOpen])

  return (
    <ActivityProvider>
    <div className="min-h-screen">
      <header
        className="sticky top-0 z-30 flex items-center justify-center border-b border-[var(--panel-border)] bg-[var(--bg)] px-4 py-3 lg:hidden"
        style={{ paddingTop: 'max(0.75rem, env(safe-area-inset-top))' }}
      >
        <div className="font-brand text-xs uppercase tracking-[0.32em] text-[var(--accent)]">Stacklab</div>
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

      <div className="mx-auto flex min-h-screen max-w-[1600px] gap-4 px-4 py-4 pb-24 md:px-6 lg:pb-4">
        <aside className="hidden w-56 shrink-0 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-4 shadow-[var(--shadow)] lg:flex lg:flex-col">
          <SidebarContent logout={logout} />
        </aside>

        <main className="flex min-w-0 flex-1 flex-col gap-4">
          <HostStrip />
          <Outlet />
        </main>
      </div>

      {/* Mobile bottom navigation: primary sections one thumb away; the rest
          (Config, Docker, Settings) lives behind "More" in the drawer. */}
      <nav
        aria-label="Primary"
        className="fixed inset-x-0 bottom-0 z-30 grid grid-cols-5 border-t border-[var(--panel-border)] bg-[var(--bg)] lg:hidden"
        style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
      >
        {bottomLinks.map(({ to, label, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            className={({ isActive }) =>
              [
                'flex flex-col items-center gap-1 py-2 text-[10px] font-medium transition',
                isActive ? 'text-[var(--accent)]' : 'text-[var(--muted)]',
              ].join(' ')
            }
          >
            <Icon className="size-5" />
            <span>{label}</span>
          </NavLink>
        ))}
        <button
          type="button"
          onClick={() => setMobileNavOpen(true)}
          aria-label="More navigation"
          className="flex flex-col items-center gap-1 py-2 text-[10px] font-medium text-[var(--muted)] transition"
        >
          <Ellipsis className="size-5" />
          <span>More</span>
        </button>
      </nav>

      <JobDetailDrawer />
    </div>
    </ActivityProvider>
  )
}

const bottomLinks = [
  { to: '/stacks', label: 'Stacks', icon: FolderKanban },
  { to: '/host', label: 'Host', icon: Monitor },
  { to: '/maintenance', label: 'Maint', icon: Wrench },
  { to: '/audit', label: 'Audit', icon: Activity },
]
