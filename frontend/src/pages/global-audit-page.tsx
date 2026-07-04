import { useCallback, useState } from 'react'
import { getGlobalAudit } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { AuditTable } from '@/components/audit-table'
import type { AuditEntry } from '@/lib/api-types'
import { PageHeader } from '@/components/page-header'

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
    <section className="rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
      <PageHeader kicker="System" title="Audit log" />

      <div className="mt-6">
        {error && (
          <div className="rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">
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
