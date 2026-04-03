import type { ReactNode } from 'react'

type RoutePlaceholderProps = {
  title: string
  summary: string
  contract: string
  aside?: ReactNode
  children?: ReactNode
}

export function RoutePlaceholder({
  title,
  summary,
  contract,
  aside,
  children,
}: RoutePlaceholderProps) {
  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
      <div className="rounded-[24px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-5">
        <div className="text-xs uppercase tracking-[0.28em] text-[var(--accent)]">Route scaffold</div>
        <h3 className="mt-3 text-2xl font-semibold tracking-[-0.04em] text-[var(--text)]">{title}</h3>
        <p className="mt-3 max-w-3xl text-sm leading-6 text-[var(--muted)]">{summary}</p>

        <div className="mt-6 rounded-2xl border border-[var(--panel-border)] bg-[rgba(0,0,0,0.18)] p-4">
          <div className="text-xs uppercase tracking-[0.28em] text-[var(--accent)]">Contract</div>
          <code className="mt-3 block overflow-x-auto whitespace-pre-wrap font-mono text-sm text-[var(--text)]">
            {contract}
          </code>
        </div>

        {children ? <div className="mt-6">{children}</div> : null}
      </div>

      <aside className="rounded-[24px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-5">
        <div className="text-xs uppercase tracking-[0.28em] text-[var(--accent)]">Build notes</div>
        <ul className="mt-4 space-y-3 text-sm leading-6 text-[var(--muted)]">
          <li>Route shell is already wired to React Router.</li>
          <li>Design placeholders intentionally reflect the IA, not final visuals.</li>
          <li>API and WebSocket contracts are documented in `docs/api/`.</li>
        </ul>
        {aside ? <div className="mt-6">{aside}</div> : null}
      </aside>
    </div>
  )
}
