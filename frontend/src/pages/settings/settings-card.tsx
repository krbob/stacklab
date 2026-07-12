import type { ReactNode } from 'react'

export function SettingsCard({ children }: { children: ReactNode }) {
  // Wrapper padding instead of margin: bottom margins bleed across CSS
  // multi-column breaks and misalign column tops.
  return (
    <div className="break-inside-avoid pb-4">
      <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        {children}
      </section>
    </div>
  )
}

export function SettingsLoadError({ title, message, onRetry }: { title: string; message: string; onRetry: () => void }) {
  return (
    <div>
      <h2 className="text-sm font-medium text-[var(--text)]">{title}</h2>
      <p className="mt-2 text-xs text-[var(--danger)]">{message}</p>
      <button type="button" onClick={onRetry} className="mt-3 rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]">
        Retry
      </button>
    </div>
  )
}
