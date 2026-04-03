import { Link } from 'react-router-dom'

export function NotFoundPage() {
  return (
    <main className="flex min-h-screen items-center justify-center px-4 py-10">
      <section className="w-full max-w-xl rounded-[32px] border border-[var(--panel-border)] bg-[var(--panel)] p-8 text-center shadow-[var(--shadow)]">
        <div className="text-xs uppercase tracking-[0.32em] text-[var(--danger)]">404</div>
        <h1 className="mt-4 text-4xl font-semibold tracking-[-0.05em] text-[var(--text)]">Page not found</h1>
        <p className="mt-4 text-sm leading-6 text-[var(--muted)]">
          The page you're looking for doesn't exist or has been moved.
        </p>
        <Link
          to="/stacks"
          className="mt-8 inline-flex rounded-full border border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(79,209,197,0.2)]"
        >
          Back to stacks
        </Link>
      </section>
    </main>
  )
}
