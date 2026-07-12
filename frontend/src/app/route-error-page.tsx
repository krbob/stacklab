import { useEffect, useRef } from 'react'
import { useLocation } from 'react-router-dom'

export function RouteErrorPage() {
  const location = useLocation()
  const headingRef = useRef<HTMLHeadingElement>(null)
  const retryHref = `${location.pathname}${location.search}${location.hash}`

  useEffect(() => {
    document.title = 'Application error | Stacklab'
    headingRef.current?.focus()
  }, [])

  return (
    <main className="flex min-h-[100dvh] items-center justify-center bg-[var(--bg)] px-4 py-10">
      <section
        role="alert"
        aria-labelledby="route-error-title"
        className="w-full max-w-xl rounded-lg border border-[var(--danger)]/30 bg-[var(--panel)] p-6 shadow-[var(--shadow)]"
      >
        <div className="font-brand text-xs uppercase tracking-[0.28em] text-[var(--accent)]">Stacklab</div>
        <h1
          ref={headingRef}
          id="route-error-title"
          tabIndex={-1}
          className="mt-3 text-2xl font-semibold tracking-[-0.04em] text-[var(--text)] outline-none"
        >
          This view could not be displayed
        </h1>
        <p className="mt-2 text-sm text-[var(--muted)]">
          An unexpected application error occurred. Retry the page, or return to the stack list.
        </p>

        <div className="mt-5 flex flex-wrap gap-2">
          <a
            href={retryHref}
            className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(245,165,36,0.2)]"
          >
            Retry
          </a>
          <a
            href="/stacks"
            className="rounded-md border border-[var(--panel-border)] px-4 py-2 text-sm text-[var(--muted)] transition hover:text-[var(--text)]"
          >
            Back to stacks
          </a>
        </div>

      </section>
    </main>
  )
}
