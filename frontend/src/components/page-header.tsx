import type { ReactNode } from 'react'

interface PageHeaderProps {
  kicker: string
  title: string
  meta?: ReactNode
  actions?: ReactNode
}

// Shared page header (design v2): kicker · title · contextual actions.
// Every top-level screen uses this so hierarchy and sizing stay identical.
export function PageHeader({ kicker, title, meta, actions }: PageHeaderProps) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-3">
      <div className="min-w-0">
        <div className="font-brand text-xs uppercase tracking-[0.28em] text-[var(--accent)]">{kicker}</div>
        <h2 className="mt-1 text-2xl font-semibold tracking-[-0.04em] text-[var(--text)]">{title}</h2>
        {meta && <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-[var(--muted)]">{meta}</div>}
      </div>
      {actions && <div className="flex max-w-full flex-wrap items-center gap-2">{actions}</div>}
    </header>
  )
}
