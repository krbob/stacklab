import { useCallback, useEffect, useRef, useState } from 'react'

interface UseApiState<T> {
  data: T | null
  error: Error | null
  loading: boolean
  updatedAt: number | null
}

export function useApi<T>(
  fetcher: (signal?: AbortSignal) => Promise<T>,
  deps: unknown[] = [],
  { enabled = true, staleTime = 0, resourceKey }: { enabled?: boolean; staleTime?: number; resourceKey?: unknown } = {},
): UseApiState<T> & { refetch: () => void } {
  const [state, setState] = useState<UseApiState<T>>({ data: null, error: null, loading: true, updatedAt: null })
  const requestIDRef = useRef(0)
  const abortRef = useRef<AbortController | null>(null)

  const lastLoadRef = useRef<(() => void) | null>(null)
  const resourceKeyRef = useRef(resourceKey)
  const settledAtRef = useRef(0)
  const pendingRef = useRef(false)

  const load = useCallback((resetError = false) => {
    const requestID = requestIDRef.current + 1
    requestIDRef.current = requestID
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    pendingRef.current = true
    setState((s) => ({ ...s, loading: true, ...(resetError ? { error: null } : {}) }))
    fetcher(controller.signal)
      .then((data) => {
        if (controller.signal.aborted || requestIDRef.current !== requestID) return
        pendingRef.current = false
        settledAtRef.current = Date.now()
        setState({ data, error: null, loading: false, updatedAt: settledAtRef.current })
      })
      .catch((error) => {
        if (controller.signal.aborted || requestIDRef.current !== requestID) return
        pendingRef.current = false
        setState((s) => ({ ...s, error, loading: false }))
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
      load(resetError)
    }
    return () => abortRef.current?.abort()
  }, [load, enabled, staleTime, resourceKey])

  const refetch = useCallback(() => load(), [load])
  return { ...state, refetch }
}
