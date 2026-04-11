import type { AuditEntry } from '@/lib/api-types'
import { useJobDrawer } from '@/hooks/use-job-drawer'
import { cn } from '@/lib/cn'

const resultColors: Record<string, string> = {
  succeeded: 'text-emerald-400',
  failed: 'text-red-400',
  cancelled: 'text-[var(--muted)]',
  timed_out: 'text-red-400',
}

interface AuditTableProps {
  entries: AuditEntry[]
  showStack?: boolean
  onLoadMore?: () => void
  hasMore?: boolean
  loading?: boolean
}

export function AuditTable({ entries, showStack = false, onLoadMore, hasMore, loading }: AuditTableProps) {
  const { openJob } = useJobDrawer()

  if (entries.length === 0 && !loading) {
    return (
      <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-10 text-center">
        <p className="text-[var(--text)]">No operations recorded yet</p>
        <p className="mt-1 text-sm text-[var(--muted)]">
          Actions like deploy, stop, pull, and remove will appear here.
        </p>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      {entries.map((entry) => {
        const hasJob = Boolean(entry.job_id)

        return (
          <div key={entry.id} data-testid="audit-row">
            <div className="flex min-w-0 items-center gap-3 overflow-hidden rounded border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-4 py-3 text-sm">
              <span className="shrink-0 text-xs text-[var(--muted)]" style={{ width: '140px' }}>
                {new Date(entry.requested_at).toLocaleString()}
              </span>
              {showStack && entry.stack_id && (
                <span className="shrink-0 truncate font-medium text-[var(--text)]" style={{ width: '100px' }}>
                  {entry.stack_id}
                </span>
              )}
              <span className="shrink-0 truncate font-mono text-xs text-[var(--text)]" style={{ width: '130px' }}>
                {entry.action}
              </span>
              <span className={cn('shrink-0 w-20 text-xs', resultColors[entry.result] ?? 'text-[var(--muted)]')}>
                {entry.result === 'succeeded' ? '✓' : '✗'} {entry.result}
              </span>
              <span className="text-xs text-[var(--muted)]">
                {entry.duration_ms != null ? `${(entry.duration_ms / 1000).toFixed(1)}s` : '—'}
              </span>
              {hasJob && (
                <button
                  onClick={() => openJob(entry.job_id!)}
                  className="ml-auto text-xs text-[var(--accent)] hover:underline"
                >
                  View detail
                </button>
              )}
            </div>
          </div>
        )
      })}

      {loading && (
        <div className="h-12 animate-pulse rounded border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />
      )}

      {hasMore && !loading && (
        <button
          data-testid="audit-load-more"
          onClick={onLoadMore}
          className="rounded-full border border-[var(--panel-border)] px-4 py-2 text-sm text-[var(--muted)] hover:text-[var(--text)]"
        >
          Load more
        </button>
      )}
    </div>
  )
}
