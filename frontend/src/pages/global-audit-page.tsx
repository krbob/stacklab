import { useCallback, useMemo, useState } from 'react'
import { getGlobalAudit } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { AuditTable } from '@/components/audit-table'
import type { AuditEntry } from '@/lib/api-types'
import { PageHeader } from '@/components/page-header'
import { cn } from '@/lib/cn'

type ResultFilter = 'all' | 'succeeded' | 'failed'

export function GlobalAuditPage() {
  const [allEntries, setAllEntries] = useState<AuditEntry[]>([])
  const [cursor, setCursor] = useState<string | undefined>(undefined)
  const [loadingMore, setLoadingMore] = useState(false)
  const [filter, setFilter] = useState('')
  const [result, setResult] = useState<ResultFilter>('all')

  const { loading, error } = useApi(async () => {
    const response = await getGlobalAudit({ limit: 50 })
    setAllEntries(response.items)
    setCursor(response.next_cursor ?? undefined)
    return response
  }, [])

  const loadMore = useCallback(async () => {
    if (!cursor) return
    setLoadingMore(true)
    try {
      const response = await getGlobalAudit({ cursor, limit: 50 })
      setAllEntries((prev) => [...prev, ...response.items])
      setCursor(response.next_cursor ?? undefined)
    } finally {
      setLoadingMore(false)
    }
  }, [cursor])

  const visible = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    return allEntries.filter((entry) => {
      if (result === 'failed' && entry.result !== 'failed' && entry.result !== 'timed_out') return false
      if (result === 'succeeded' && entry.result !== 'succeeded') return false
      if (!needle) return true
      return (
        entry.action.toLowerCase().includes(needle) ||
        (entry.stack_id ?? '').toLowerCase().includes(needle)
      )
    })
  }, [allEntries, filter, result])

  const failedCount = useMemo(
    () => allEntries.filter((e) => e.result === 'failed' || e.result === 'timed_out').length,
    [allEntries],
  )

  return (
    <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <PageHeader
        kicker="System"
        title="Audit log"
        meta={allEntries.length > 0 && (
          <>
            <span>{allEntries.length} loaded</span>
            {failedCount > 0 && <span className="text-[var(--danger)]">{failedCount} failed</span>}
          </>
        )}
      />

      {/* Filters (client-side over loaded pages) */}
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <input
          data-testid="audit-filter"
          type="text"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter by action or stack…"
          className="w-full max-w-72 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-1.5 font-mono text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]"
        />
        {(['all', 'succeeded', 'failed'] as const).map((value) => (
          <button
            key={value}
            onClick={() => setResult(value)}
            className={cn(
              'rounded-md border px-3 py-1.5 text-xs capitalize transition',
              result === value
                ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                : 'border-[var(--panel-border)] text-[var(--muted)] hover:text-[var(--text)]',
            )}
          >
            {value}
          </button>
        ))}
      </div>

      <div className="mt-4">
        {error && (
          <div className="rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">
            Failed to load audit: {error.message}
          </div>
        )}

        <AuditTable
          entries={visible}
          showStack
          loading={loading || loadingMore}
          hasMore={!!cursor}
          onLoadMore={loadMore}
        />

        {!loading && visible.length === 0 && allEntries.length > 0 && (
          <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-8 text-center text-sm text-[var(--muted)]">
            No entries match the current filter.
          </div>
        )}
      </div>
    </section>
  )
}
