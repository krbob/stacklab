export function LoginPage() {
  return (
    <main className="flex min-h-screen items-center justify-center px-4 py-10">
      <section className="w-full max-w-xl rounded-[32px] border border-[var(--panel-border)] bg-[var(--panel)] p-8 shadow-[var(--shadow)]">
        <div className="text-xs uppercase tracking-[0.32em] text-[var(--accent)]">Stacklab</div>
        <h1 className="mt-4 text-4xl font-semibold tracking-[-0.05em] text-[var(--text)]">Single-user access gate.</h1>
        <p className="mt-4 max-w-lg text-sm leading-6 text-[var(--muted)]">
          Minimal login scaffold matching the documented single-password auth flow. Final UI will plug into
          <code className="mx-1 rounded bg-[rgba(255,255,255,0.06)] px-2 py-1 font-mono text-[var(--text)]">POST /api/auth/login</code>
          and
          <code className="mx-1 rounded bg-[rgba(255,255,255,0.06)] px-2 py-1 font-mono text-[var(--text)]">GET /api/session</code>.
        </p>

        <form className="mt-8 space-y-4">
          <label className="block">
            <span className="mb-2 block text-sm text-[var(--muted)]">Password</span>
            <input
              type="password"
              placeholder="••••••••"
              className="w-full rounded-2xl border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-3 text-[var(--text)] outline-none transition focus:border-[rgba(79,209,197,0.35)]"
            />
          </label>
          <button
            type="button"
            className="w-full rounded-2xl bg-[linear-gradient(135deg,rgba(79,209,197,0.9),rgba(20,184,166,0.95))] px-4 py-3 text-sm font-medium text-[#042328] transition hover:brightness-105"
          >
            Log in
          </button>
        </form>
      </section>
    </main>
  )
}
