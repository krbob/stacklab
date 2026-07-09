import { useCallback, useEffect, useRef, useState } from 'react'

interface UseApiState<T> {
  data: T | null
  error: Error | null
  loading: boolean
  updatedAt: number | null
}

export function useApi<T>(fetcher: (signal?: AbortSignal) => Promise<T>, deps: unknown[] = []): UseApiState<T> & { refetch: () => void } {
  const [state, setState] = useState<UseApiState<T>>({ data: null, error: null, loading: true, updatedAt: null })
  const requestIDRef = useRef(0)
  const abortRef = useRef<AbortController | null>(null)

  const load = useCallback(() => {
    const requestID = requestIDRef.current + 1
    requestIDRef.current = requestID
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setState((s) => ({ ...s, loading: true, error: null }))
    fetcher(controller.signal)
      .then((data) => {
        if (controller.signal.aborted || requestIDRef.current !== requestID) return
        setState({ data, error: null, loading: false, updatedAt: Date.now() })
      })
      .catch((error) => {
        if (controller.signal.aborted || requestIDRef.current !== requestID) return
        setState((s) => ({ ...s, error, loading: false }))
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps, react-hooks/use-memo
  }, deps)

  useEffect(() => {
    load()
    return () => abortRef.current?.abort()
  }, [load])

  return { ...state, refetch: load }
}
