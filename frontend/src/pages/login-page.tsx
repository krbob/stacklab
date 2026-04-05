import { useState, type FormEvent } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuth } from '@/hooks/use-auth'

export function LoginPage() {
  const { status, login } = useAuth()
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  if (status === 'authenticated') {
    return <Navigate to="/stacks" replace />
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!password.trim()) return

    setError(null)
    setLoading(true)
    try {
      await login(password)
    } catch {
      setError('Invalid password')
    } finally {
      setLoading(false)
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center px-4 py-10">
      <section className="w-full max-w-xl rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-8 shadow-[var(--shadow)]">
        <div className="flex items-center gap-3">
          <svg viewBox="0 0 64 64" fill="none" xmlns="http://www.w3.org/2000/svg" className="size-10 shrink-0">
            <rect width="64" height="64" rx="12" fill="#0A0A0B" />
            <rect x="12" y="15" width="40" height="9" rx="4.5" fill="#22C55E" />
            <rect x="16" y="28" width="32" height="9" rx="4.5" fill="#22C55E" opacity="0.5" />
            <rect x="20" y="41" width="24" height="9" rx="4.5" fill="#22C55E" opacity="0.25" />
          </svg>
          <div className="text-xs uppercase tracking-[0.32em] text-[var(--accent)]">Stacklab</div>
        </div>
        <h1 className="mt-4 text-4xl font-semibold tracking-[-0.05em] text-[var(--text)]">Log in</h1>
        <p className="mt-4 max-w-lg text-sm leading-6 text-[var(--muted)]">
          Single-user access. Enter your password to continue.
        </p>

        <form onSubmit={handleSubmit} className="mt-8 space-y-4">
          <label className="block">
            <span className="mb-2 block text-sm text-[var(--muted)]">Password</span>
            <input
              data-testid="login-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
              autoFocus
              disabled={loading}
              className="w-full rounded-2xl border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-3 text-[var(--text)] outline-none transition focus:border-[rgba(34,197,94,0.35)] disabled:opacity-50"
            />
          </label>

          {error && (
            <p className="text-sm text-[var(--danger)]">{error}</p>
          )}

          <button
            data-testid="login-submit"
            type="submit"
            disabled={loading || !password.trim()}
            className="w-full rounded-2xl bg-emerald-500 px-4 py-3 text-sm font-medium text-black transition hover:brightness-105 disabled:opacity-50"
          >
            {loading ? 'Logging in...' : 'Log in'}
          </button>
        </form>
      </section>
    </main>
  )
}
