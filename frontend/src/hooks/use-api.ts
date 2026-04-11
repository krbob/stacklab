import { useCallback, useEffect, useState } from 'react'

interface UseApiState<T> {
  data: T | null
  error: Error | null
  loading: boolean
  updatedAt: number | null
}

export function useApi<T>(fetcher: () => Promise<T>, deps: unknown[] = []): UseApiState<T> & { refetch: () => void } {
  const [state, setState] = useState<UseApiState<T>>({ data: null, error: null, loading: true, updatedAt: null })

  const load = useCallback(() => {
    setState((s) => ({ ...s, loading: true, error: null }))
    fetcher()
      .then((data) => setState({ data, error: null, loading: false, updatedAt: Date.now() }))
      .catch((error) => setState((s) => ({ data: null, error, loading: false, updatedAt: s.updatedAt })))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  useEffect(() => { load() }, [load])

  return { ...state, refetch: load }
}
