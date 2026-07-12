import { useEffect, useRef, useState } from 'react'
import { Activity, Container, Ellipsis, FolderCog, FolderKanban, LogOut, Monitor, Settings, Wrench, X } from 'lucide-react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '@/hooks/use-auth'
import { GlobalActivity } from '@/components/global-activity'
import { JobDetailDrawer } from '@/components/job-detail-drawer'
import { Drawer } from '@/components/drawer'
import { HostStrip } from '@/components/host-strip'
import { ActivityProvider } from '@/contexts/activity-context'
import { CommandPalette } from '@/components/command-palette'
import { hasActiveModal } from '@/lib/modal-state'

const links = [
  { to: '/stacks', label: 'Stacks', icon: FolderKanban },
  { to: '/host', label: 'Host', icon: Monitor },
  { to: '/config', label: 'Config', icon: FolderCog },
  { to: '/maintenance', label: 'Maintenance', icon: Wrench },
  { to: '/docker', label: 'Docker', icon: Container },
  { to: '/audit', label: 'Audit', icon: Activity },
  { to: '/settings', label: 'Settings', icon: Settings },
]

function SidebarContent({ onNavigate, onLogout, loggingOut, logoutError }: {
  onNavigate?: () => void
  onLogout: () => void
  loggingOut: boolean
  logoutError: string | null
}) {
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
            <kbd className="ml-auto rounded border border-[rgba(255,255,255,0.1)] px-1 font-mono text-xs text-[var(--dim)]">{index + 1}</kbd>
          </NavLink>
        ))}
      </nav>

      <div className="mt-auto space-y-1">
        <GlobalActivity />
        <button
          onClick={onLogout}
          disabled={loggingOut}
          className="flex w-full items-center gap-3 rounded-lg border border-transparent px-4 py-3 text-sm text-[var(--muted)] transition hover:border-[var(--panel-border)] hover:bg-[rgba(255,255,255,0.03)] hover:text-[var(--text)] disabled:cursor-not-allowed disabled:opacity-50"
        >
          <LogOut className="size-4" />
          <span>{loggingOut ? 'Logging out…' : 'Log out'}</span>
        </button>
        {logoutError && (
          <p className="rounded-md border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-3 py-2 text-xs leading-5 text-[var(--danger)]" role="alert">
            {logoutError}
          </p>
        )}
      </div>
    </>
  )
}

export function RootLayout() {
  const { logout } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const scrollRef = useRef<HTMLDivElement>(null)
  const mobileNavCloseRef = useRef<HTMLButtonElement>(null)
  const logoutPendingRef = useRef(false)
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const [loggingOut, setLoggingOut] = useState(false)
  const [logoutError, setLogoutError] = useState<string | null>(null)
  const moreActive = morePaths.some((path) => location.pathname === path || location.pathname.startsWith(`${path}/`))

  // Nav hotkeys 1-7 (Z5); skipped while typing.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.metaKey || e.ctrlKey || e.altKey) return
      const target = e.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) return
      if (hasActiveModal()) return
      const index = Number.parseInt(e.key, 10) - 1
      if (Number.isNaN(index) || index < 0 || index >= links.length) return
      e.preventDefault()
      navigate(links[index].to)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [navigate])

  // Reset scroll on navigation. On mobile the app-shell scrolls inside
  // scrollRef; on desktop the document scrolls. Reset both so opening a stack
  // never lands mid-page (the list's scroll position must not carry over).
  useEffect(() => {
    scrollRef.current?.scrollTo(0, 0)
    window.scrollTo(0, 0)
  }, [location.pathname])

  async function handleLogout() {
    if (logoutPendingRef.current) return
    logoutPendingRef.current = true
    setLoggingOut(true)
    setLogoutError(null)
    try {
      await logout()
      setMobileNavOpen(false)
    } catch {
      setLogoutError('Logout could not be confirmed. Your session may still be active. Try again.')
    } finally {
      logoutPendingRef.current = false
      setLoggingOut(false)
    }
  }

  return (
    <ActivityProvider>
    {/* App-shell: on mobile a fixed-height flex column (header · scroll · nav)
        so the bars never overlap content and no content escapes above/below
        them. 100dvh tracks Safari's dynamic toolbars, so the shell always fits
        the visible viewport. On desktop it collapses back to normal document
        flow (the mobile bars are hidden anyway). */}
    <div className="flex h-[100dvh] flex-col overflow-hidden lg:block lg:h-auto lg:min-h-screen lg:overflow-visible">
      <header
        className="grid shrink-0 grid-cols-[1fr_auto_1fr] items-center border-b border-[var(--panel-border)] bg-[var(--bg)] px-4 py-3 lg:hidden"
        style={{ paddingTop: 'max(0.75rem, env(safe-area-inset-top))' }}
      >
        <span aria-hidden />
        <div className="font-brand text-xs uppercase tracking-[0.32em] text-[var(--accent)]">Stacklab</div>
        <div className="justify-self-end">
          <GlobalActivity variant="compact" />
        </div>
      </header>

      {mobileNavOpen && (
        <Drawer
          id="mobile-navigation"
          label="Navigation"
          onClose={() => setMobileNavOpen(false)}
          initialFocusRef={mobileNavCloseRef}
          side="left"
          panelClassName="w-72 max-w-[85vw] p-4 shadow-xl lg:hidden"
          backdropClassName="bg-black/60 backdrop-blur-sm lg:hidden"
          style={{
            paddingTop: 'max(1rem, env(safe-area-inset-top))',
            paddingBottom: 'max(1rem, env(safe-area-inset-bottom))',
            paddingLeft: 'max(1rem, env(safe-area-inset-left))',
          }}
        >
          <button
            ref={mobileNavCloseRef}
            type="button"
            onClick={() => setMobileNavOpen(false)}
            aria-label="Close navigation"
            className="absolute right-3 top-3 flex size-10 items-center justify-center rounded-lg text-[var(--muted)] hover:text-[var(--text)]"
          >
            <X className="size-5" />
          </button>
          <SidebarContent
            onNavigate={() => setMobileNavOpen(false)}
            onLogout={() => { void handleLogout() }}
            loggingOut={loggingOut}
            logoutError={logoutError}
          />
        </Drawer>
      )}

      {/* Scroll region: the only scrollable element on mobile (flex-1 + min-h-0
          so it can shrink and scroll between the fixed bars). On desktop it
          reverts to a normal block and the document scrolls as before. */}
      <div
        ref={scrollRef}
        className="min-h-0 flex-1 overflow-y-auto overscroll-contain lg:flex-none lg:overflow-visible"
      >
        <div className="mx-auto flex max-w-[1600px] gap-4 px-4 py-4 md:px-6 lg:min-h-screen">
          <aside className="hidden w-56 shrink-0 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-4 shadow-[var(--shadow)] lg:flex lg:flex-col">
            <SidebarContent
              onLogout={() => { void handleLogout() }}
              loggingOut={loggingOut}
              logoutError={logoutError}
            />
          </aside>

          <main className="flex min-w-0 flex-1 flex-col gap-4">
            <HostStrip />
            <Outlet />
          </main>
        </div>
      </div>

      {/* Mobile bottom navigation: primary sections one thumb away; the rest
          (Config, Docker, Settings) lives behind "More" in the drawer. Last
          flex child of the shell — never overlaps content, so no clearance
          padding is needed on the scroll region. */}
      <nav
        aria-label="Primary"
        className="grid shrink-0 grid-cols-5 border-t border-[var(--panel-border)] bg-[var(--bg)] lg:hidden"
        style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
      >
        {bottomLinks.map(({ to, label, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            className={({ isActive }) =>
              [
                'flex flex-col items-center gap-1 py-2 text-xs font-medium transition',
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
          onClick={(event) => {
            event.currentTarget.focus()
            setMobileNavOpen(true)
          }}
          aria-label="More navigation"
          aria-controls="mobile-navigation"
          aria-expanded={mobileNavOpen}
          aria-pressed={moreActive}
          className={[
            'flex flex-col items-center gap-1 py-2 text-xs font-medium transition',
            mobileNavOpen || moreActive ? 'text-[var(--accent)]' : 'text-[var(--muted)]',
          ].join(' ')}
        >
          <Ellipsis className="size-5" />
          <span>More</span>
        </button>
      </nav>

      <JobDetailDrawer />
      <CommandPalette />
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

const morePaths = ['/config', '/docker', '/settings']
