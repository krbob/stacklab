import { useState, type FormEvent } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuth } from '@/contexts/auth-context'

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
      <section className="w-full max-w-xl rounded-[32px] border border-[var(--panel-border)] bg-[var(--panel)] p-8 shadow-[var(--shadow)]">
        <div className="text-xs uppercase tracking-[0.32em] text-[var(--accent)]">Stacklab</div>
        <h1 className="mt-4 text-4xl font-semibold tracking-[-0.05em] text-[var(--text)]">Log in</h1>
        <p className="mt-4 max-w-lg text-sm leading-6 text-[var(--muted)]">
          Single-user access. Enter your password to continue.
        </p>

        <form onSubmit={handleSubmit} className="mt-8 space-y-4">
          <label className="block">
            <span className="mb-2 block text-sm text-[var(--muted)]">Password</span>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
              autoFocus
              disabled={loading}
              className="w-full rounded-2xl border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-4 py-3 text-[var(--text)] outline-none transition focus:border-[rgba(79,209,197,0.35)] disabled:opacity-50"
            />
          </label>

          {error && (
            <p className="text-sm text-[var(--danger)]">{error}</p>
          )}

          <button
            type="submit"
            disabled={loading || !password.trim()}
            className="w-full rounded-2xl bg-[linear-gradient(135deg,rgba(79,209,197,0.9),rgba(20,184,166,0.95))] px-4 py-3 text-sm font-medium text-[#042328] transition hover:brightness-105 disabled:opacity-50"
          >
            {loading ? 'Logging in...' : 'Log in'}
          </button>
        </form>
      </section>
    </main>
  )
}
