import { useState } from 'react'
import type { AuditEntry } from '@/lib/api-types'
import { ProgressPanel } from '@/components/progress-panel'
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
  const [expandedJobId, setExpandedJobId] = useState<string | null>(null)

  if (entries.length === 0 && !loading) {
    return (
      <div className="rounded-[20px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-10 text-center">
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
        const isFailed = entry.result === 'failed' || entry.result === 'timed_out'

        return (
          <div key={entry.id}>
            <div className="flex min-w-0 items-center gap-3 overflow-hidden rounded-[16px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-4 py-3 text-sm">
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
              {isFailed && (
                <button
                  onClick={() => setExpandedJobId(expandedJobId === entry.id ? null : entry.id)}
                  className="ml-auto text-xs text-[var(--accent)] hover:underline"
                >
                  {expandedJobId === entry.id ? 'Hide log' : 'View log'}
                </button>
              )}
            </div>

            {expandedJobId === entry.id && (
              <AuditJobDetail auditEntry={entry} />
            )}
          </div>
        )
      })}

      {loading && (
        <div className="h-12 animate-pulse rounded-[16px] border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)]" />
      )}

      {hasMore && !loading && (
        <button
          onClick={onLoadMore}
          className="rounded-full border border-[var(--panel-border)] px-4 py-2 text-sm text-[var(--muted)] hover:text-[var(--text)]"
        >
          Load more
        </button>
      )}
    </div>
  )
}

function AuditJobDetail({ auditEntry }: { auditEntry: AuditEntry }) {
  const jobId = auditEntry.job_id

  if (!jobId) {
    return (
      <div className="ml-4 mt-1 rounded-[16px] border border-[var(--panel-border)] bg-[rgba(0,0,0,0.15)] px-4 py-3 text-xs text-[var(--muted)]">
        Detailed output is no longer retained
      </div>
    )
  }

  return (
    <div className="ml-4 mt-1">
      <ProgressPanel jobId={jobId} />
    </div>
  )
}
