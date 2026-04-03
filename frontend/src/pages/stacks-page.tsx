import { Link } from 'react-router-dom'

const sampleStacks = [
  { name: 'traefik', state: 'running', detail: '3/3 services · in_sync' },
  { name: 'nextcloud', state: 'running', detail: '2/2 services · drifted' },
  { name: 'monitoring', state: 'partial', detail: '2/3 services · in_sync' },
  { name: 'backup-nightly', state: 'defined', detail: 'never deployed · unknown config state' },
]

export function StacksPage() {
  return (
    <section className="rounded-[28px] border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div>
          <div className="text-xs uppercase tracking-[0.28em] text-[var(--accent)]">Dashboard scaffold</div>
          <h2 className="mt-2 text-3xl font-semibold tracking-[-0.04em] text-[var(--text)]">Stacks</h2>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-[var(--muted)]">
            Placeholder dashboard wired to the documented route structure. Final data source is
            <code className="mx-1 rounded bg-[rgba(255,255,255,0.06)] px-2 py-1 font-mono text-[var(--text)]">GET /api/stacks</code>.
          </p>
        </div>

        <Link
          to="/stacks/new"
          className="rounded-full border border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] px-4 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(79,209,197,0.2)]"
        >
          New stack
        </Link>
      </div>

      <div className="mt-6 grid gap-3">
        {sampleStacks.map((stack) => (
          <Link
            key={stack.name}
            to={`/stacks/${stack.name}`}
            className="flex flex-col gap-3 rounded-[24px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-5 py-4 transition hover:border-[rgba(79,209,197,0.25)] hover:bg-[rgba(255,255,255,0.05)] md:flex-row md:items-center md:justify-between"
          >
            <div>
              <div className="text-lg font-medium text-[var(--text)]">{stack.name}</div>
              <div className="mt-1 text-sm text-[var(--muted)]">{stack.detail}</div>
            </div>
            <div className="rounded-full border border-[rgba(79,209,197,0.22)] px-3 py-1 text-xs uppercase tracking-[0.2em] text-[var(--accent)]">
              {stack.state}
            </div>
          </Link>
        ))}
      </div>
    </section>
  )
}
