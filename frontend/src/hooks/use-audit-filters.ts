import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useDebouncedValue } from '@/hooks/use-debounced-value'
import {
  clearAuditFilters,
  hasAuditFilters,
  readAuditFilters,
  toAuditQuery,
  validateAuditDateRange,
  writeAuditFilters,
  type AuditFilterPatch,
} from '@/lib/audit-filters'

export function useAuditFilters() {
  const [searchParams, setSearchParams] = useSearchParams()
  const serializedSearchParams = searchParams.toString()
  const filters = useMemo(() => readAuditFilters(new URLSearchParams(serializedSearchParams)), [serializedSearchParams])
  const hasURLFilterParams = useMemo(() => {
    const current = new URLSearchParams(serializedSearchParams)
    return ['q', 'result', 'from', 'to'].some((key) => current.has(key))
  }, [serializedSearchParams])
  const debouncedSearch = useDebouncedValue(filters.search, 300)
  const rangeError = validateAuditDateRange(filters)

  const query = useMemo(
    () => toAuditQuery({
      search: debouncedSearch,
      result: filters.result,
      fromDate: filters.fromDate,
      toDate: filters.toDate,
    }),
    [debouncedSearch, filters.fromDate, filters.result, filters.toDate],
  )
  const queryKey = useMemo(() => JSON.stringify(query), [query])

  const updateFilters = useCallback((patch: AuditFilterPatch) => {
    setSearchParams((current) => writeAuditFilters(current, patch), { replace: true })
  }, [setSearchParams])

  const clearFilters = useCallback(() => {
    setSearchParams((current) => clearAuditFilters(current), { replace: true })
  }, [setSearchParams])

  return {
    filters,
    query,
    queryKey,
    rangeError,
    hasActiveFilters: hasAuditFilters(filters) || hasURLFilterParams,
    updateFilters,
    clearFilters,
  }
}
