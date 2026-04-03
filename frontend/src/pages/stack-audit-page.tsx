import { useCallback, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import type { StackDetailResponse, AuditEntry } from '@/lib/api-types'
import { getStackAudit } from '@/lib/api-client'
import { useApi } from '@/hooks/use-api'
import { AuditTable } from '@/components/audit-table'

export function StackAuditPage() {
  const { stack } = useOutletContext<{ stack: StackDetailResponse['stack'] }>()
  const [allEntries, setAllEntries] = useState<AuditEntry[]>([])
  const [cursor, setCursor] = useState<string | undefined>(undefined)
  const [loadingMore, setLoadingMore] = useState(false)

  const { loading, error } = useApi(async () => {
    const result = await getStackAudit(stack.id, { limit: 50 })
    setAllEntries(result.items)
    setCursor(result.next_cursor ?? undefined)
    return result
  }, [stack.id])

  const loadMore = useCallback(async () => {
    if (!cursor) return
    setLoadingMore(true)
    try {
      const result = await getStackAudit(stack.id, { cursor, limit: 50 })
      setAllEntries((prev) => [...prev, ...result.items])
      setCursor(result.next_cursor ?? undefined)
    } finally {
      setLoadingMore(false)
    }
  }, [stack.id, cursor])

  return (
    <div>
      {error && (
        <div className="mb-3 rounded-2xl border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">
          Failed to load history: {error.message}
        </div>
      )}

      <AuditTable
        entries={allEntries}
        loading={loading || loadingMore}
        hasMore={!!cursor}
        onLoadMore={loadMore}
      />
    </div>
  )
}
