import { useCallback, useEffect, useRef, useState } from 'react'

interface UseApiState<T> {
  data: T | null
  error: Error | null
  loading: boolean
  backgroundLoading: boolean
  updatedAt: number | null
}

export function useApi<T>(
  fetcher: (signal?: AbortSignal) => Promise<T>,
  deps: unknown[] = [],
  { enabled = true, staleTime = 0, resourceKey, backgroundRefresh = false }: { enabled?: boolean; staleTime?: number; resourceKey?: unknown; backgroundRefresh?: boolean } = {},
): UseApiState<T> & { refetch: () => void; backgroundRefetch: (force?: boolean) => void } {
  const [state, setState] = useState<UseApiState<T>>({ data: null, error: null, loading: true, backgroundLoading: false, updatedAt: null })
  const requestIDRef = useRef(0)
  const abortRef = useRef<AbortController | null>(null)

  const lastLoadRef = useRef<(() => void) | null>(null)
  const resourceKeyRef = useRef(resourceKey)
  const settledAtRef = useRef(0)
  const pendingRef = useRef(false)

  const load = useCallback((resetError = false, background = false, force = false) => {
    // Background refreshes share an in-flight request; a manual retry can supersede it.
    if (background && !force && pendingRef.current && !abortRef.current?.signal.aborted) return
    const requestID = requestIDRef.current + 1
    requestIDRef.current = requestID
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    pendingRef.current = true
    setState((s) => ({ ...s, loading: !background, backgroundLoading: background, ...(resetError ? { error: null } : {}) }))
    fetcher(controller.signal)
      .then((data) => {
        if (controller.signal.aborted || requestIDRef.current !== requestID) return
        pendingRef.current = false
        settledAtRef.current = Date.now()
        setState({ data, error: null, loading: false, backgroundLoading: false, updatedAt: settledAtRef.current })
      })
      .catch((error) => {
        if (controller.signal.aborted || requestIDRef.current !== requestID) return
        pendingRef.current = false
        setState((s) => ({ ...s, error, loading: false, backgroundLoading: false }))
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps, react-hooks/use-memo
  }, deps)

  useEffect(() => {
    const resourceChanged = lastLoadRef.current !== load
    const identityChanged = resourceKey !== undefined && !Object.is(resourceKeyRef.current, resourceKey)
    if (enabled && (resourceChanged || identityChanged || pendingRef.current || !settledAtRef.current || Date.now() - settledAtRef.current >= staleTime)) {
      const resetError = resourceKey === undefined ? resourceChanged : identityChanged
      lastLoadRef.current = load
      resourceKeyRef.current = resourceKey
      load(resetError, backgroundRefresh && !resetError && !!settledAtRef.current)
    }
    return () => abortRef.current?.abort()
  }, [load, enabled, staleTime, resourceKey, backgroundRefresh])

  const refetch = useCallback(() => load(), [load])
  const backgroundRefetch = useCallback((force = false) => load(false, true, force), [load])
  return { ...state, refetch, backgroundRefetch }
}
