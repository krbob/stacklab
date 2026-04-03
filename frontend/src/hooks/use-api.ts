import { useCallback, useEffect, useState } from 'react'

interface UseApiState<T> {
  data: T | null
  error: Error | null
  loading: boolean
}

export function useApi<T>(fetcher: () => Promise<T>, deps: unknown[] = []): UseApiState<T> & { refetch: () => void } {
  const [state, setState] = useState<UseApiState<T>>({ data: null, error: null, loading: true })

  const load = useCallback(() => {
    setState((s) => ({ ...s, loading: true, error: null }))
    fetcher()
      .then((data) => setState({ data, error: null, loading: false }))
      .catch((error) => setState({ data: null, error, loading: false }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  useEffect(() => { load() }, [load])

  return { ...state, refetch: load }
}
