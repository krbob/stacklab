import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { AuditEntry, AuditResponse } from '@/lib/api-types'
import type { AuditQueryParams } from '@/lib/api-client'

type AuditPageLoader = (params: AuditQueryParams, signal: AbortSignal) => Promise<AuditResponse>

interface PaginatedAuditOptions {
  queryKey: string
  loadPage: AuditPageLoader
  enabled?: boolean
}

export function usePaginatedAudit({ queryKey, loadPage, enabled = true }: PaginatedAuditOptions) {
  const requestQuery = useMemo<AuditQueryParams>(() => JSON.parse(queryKey) as AuditQueryParams, [queryKey])
  const [entries, setEntries] = useState<AuditEntry[]>([])
  const [cursor, setCursor] = useState<string | undefined>()
  const [loading, setLoading] = useState(enabled)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<Error | null>(null)
  const [reloadToken, setReloadToken] = useState(0)
  const generationRef = useRef(0)
  const loadMoreAbortRef = useRef<AbortController | null>(null)
  const loadingMoreRef = useRef(false)

  useEffect(() => {
    const generation = generationRef.current + 1
    generationRef.current = generation
    loadMoreAbortRef.current?.abort()
    loadingMoreRef.current = false
    setEntries([])
    setCursor(undefined)
    setLoadingMore(false)
    setError(null)

    if (!enabled) {
      setLoading(false)
      return
    }

    const controller = new AbortController()
    setLoading(true)
    loadPage({ ...requestQuery, limit: 50 }, controller.signal)
      .then((response) => {
        if (controller.signal.aborted || generationRef.current !== generation) return
        setEntries(response.items)
        setCursor(response.next_cursor ?? undefined)
      })
      .catch((reason) => {
        if (controller.signal.aborted || generationRef.current !== generation) return
        setError(reason instanceof Error ? reason : new Error('Failed to load audit entries'))
      })
      .finally(() => {
        if (!controller.signal.aborted && generationRef.current === generation) setLoading(false)
      })

    return () => controller.abort()
  }, [enabled, loadPage, queryKey, reloadToken, requestQuery])

  const loadMore = useCallback(async () => {
    if (!enabled || !cursor || loadingMoreRef.current) return
    const generation = generationRef.current
    const controller = new AbortController()
    loadMoreAbortRef.current?.abort()
    loadMoreAbortRef.current = controller
    loadingMoreRef.current = true
    setLoadingMore(true)
    try {
      const response = await loadPage({ ...requestQuery, cursor, limit: 50 }, controller.signal)
      if (controller.signal.aborted || generationRef.current !== generation) return
      setEntries((current) => [...current, ...response.items])
      setCursor(response.next_cursor ?? undefined)
    } catch (reason) {
      if (controller.signal.aborted || generationRef.current !== generation) return
      throw reason
    } finally {
      if (generationRef.current === generation) {
        loadingMoreRef.current = false
        setLoadingMore(false)
      }
    }
  }, [cursor, enabled, loadPage, requestQuery])

  const refetch = useCallback(() => setReloadToken((token) => token + 1), [])

  useEffect(() => () => loadMoreAbortRef.current?.abort(), [])

  return {
    entries,
    error,
    loading,
    loadingMore,
    hasMore: Boolean(cursor),
    loadMore,
    refetch,
  }
}
