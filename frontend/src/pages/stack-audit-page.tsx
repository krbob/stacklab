import { useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import type { StackDetailResponse } from '@/lib/api-types'
import { getStackAudit, type AuditQueryParams } from '@/lib/api-client'
import { AuditTable } from '@/components/audit-table'
import { AuditFilterBar } from '@/components/audit-filter-bar'
import { useAuditFilters } from '@/hooks/use-audit-filters'
import { usePaginatedAudit } from '@/hooks/use-paginated-audit'

export function StackAuditPage() {
  const { stack } = useOutletContext<{ stack: StackDetailResponse['stack'] }>()
  const {
    filters,
    queryKey,
    rangeError,
    hasActiveFilters,
    updateFilters,
    clearFilters,
  } = useAuditFilters()
  const loadPage = useCallback(
    (params: AuditQueryParams, signal: AbortSignal) => getStackAudit(stack.id, params, signal),
    [stack.id],
  )
  const {
    entries,
    error,
    loading,
    loadingMore,
    hasMore,
    loadMore,
    refetch,
  } = usePaginatedAudit({
    queryKey,
    loadPage,
    enabled: rangeError === null,
  })

  return (
    <div>
      <AuditFilterBar
        filters={filters}
        hasActiveFilters={hasActiveFilters}
        rangeError={rangeError}
        onChange={updateFilters}
        onClear={clearFilters}
      />

      <div className="mt-4">
        {error && (
          <div className="mb-3 flex flex-wrap items-center gap-3 rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]" role="alert">
            <span>Failed to load history: {error.message}</span>
            <button type="button" onClick={refetch} className="ml-auto rounded-md border border-[var(--danger)]/30 px-3 py-1 text-xs hover:bg-[var(--danger)]/10">
              Retry
            </button>
          </div>
        )}

        {!rangeError && (!error || entries.length > 0) && (
          <AuditTable
            key={queryKey}
            entries={entries}
            loading={loading || loadingMore}
            hasMore={hasMore}
            onLoadMore={loadMore}
            emptyTitle={hasActiveFilters ? 'No stack operations match these filters' : undefined}
            emptyDescription={hasActiveFilters ? 'Try widening the date range or clearing one of the filters.' : undefined}
          />
        )}
      </div>
    </div>
  )
}
