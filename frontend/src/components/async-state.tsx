import type { ReactNode } from 'react'

interface AsyncStateProps {
  loading: boolean
  error: Error | null
  hasData: boolean
  isEmpty: boolean
  loadingLabel: string
  emptyMessage: string
  onRetry: () => void
  loadingFallback: ReactNode
  children: ReactNode
}

export function AsyncState({
  loading,
  error,
  hasData,
  isEmpty,
  loadingLabel,
  emptyMessage,
  onRetry,
  loadingFallback,
  children,
}: AsyncStateProps) {
  if (!hasData) {
    if (error) {
      return <LoadError error={error} onRetry={onRetry} stale={false} />
    }

    return (
      <>
        <span className="sr-only" role="status" aria-live="polite">{loadingLabel}</span>
        <div aria-hidden="true" className="space-y-1">{loadingFallback}</div>
      </>
    )
  }

  return (
    <>
      {loading && (
        <p className="text-xs text-[var(--muted)]" role="status" aria-live="polite">Refreshing…</p>
      )}
      {error && <LoadError error={error} onRetry={onRetry} stale />}
      {isEmpty
        ? <p className="py-6 text-center text-sm text-[var(--muted)]">{emptyMessage}</p>
        : children}
    </>
  )
}

function LoadError({ error, onRetry, stale }: {
  error: Error
  onRetry: () => void
  stale: boolean
}) {
  return (
    <div
      role="alert"
      className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]"
    >
      <div>
        <p>{error.message}</p>
        {stale && (
          <p className="mt-1 text-xs text-[var(--muted)]">Showing the last successfully loaded data.</p>
        )}
      </div>
      <button
        type="button"
        onClick={onRetry}
        className="rounded-md border border-[var(--danger)]/30 px-3 py-1.5 text-xs text-[var(--danger)] hover:bg-[var(--danger)]/10"
      >
        Retry
      </button>
    </div>
  )
}
