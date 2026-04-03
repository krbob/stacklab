import { useCallback, useState } from 'react'
import { getGlobalAudit } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { AuditTable } from '@/components/audit-table'
import type { AuditEntry } from '@/lib/api-types'

export function GlobalAuditPage() {
  const [allEntries, setAllEntries] = useState<AuditEntry[]>([])
  const [cursor, setCursor] = useState<string | undefined>(undefined)
  const [loadingMore, setLoadingMore] = useState(false)

  const { loading, error } = useApi(async () => {
    const result = await getGlobalAudit({ limit: 50 })
    setAllEntries(result.items)
    setCursor(result.next_cursor ?? undefined)
    return result
  }, [])

  const loadMore = useCallback(async () => {
    if (!cursor) return
    setLoadingMore(true)
    try {
      const result = await getGlobalAudit({ cursor, limit: 50 })
      setAllEntries((prev) => [...prev, ...result.items])
      setCursor(result.next_cursor ?? undefined)
    } finally {
      setLoadingMore(false)
    }
  }, [cursor])

  return (
    <section className="rounded-[28px] border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <div className="text-xs uppercase tracking-[0.28em] text-[var(--accent)]">System</div>
      <h2 className="mt-2 text-3xl font-semibold tracking-[-0.04em] text-[var(--text)]">Audit log</h2>

      <div className="mt-6">
        {error && (
          <div className="rounded-2xl border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">
            Failed to load audit: {error.message}
          </div>
        )}

        <AuditTable
          entries={allEntries}
          showStack
          loading={loading || loadingMore}
          hasMore={!!cursor}
          onLoadMore={loadMore}
        />
      </div>
    </section>
  )
}
