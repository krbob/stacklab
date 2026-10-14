import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { useApi } from './use-api'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('useApi', () => {
  it('ignores stale responses after dependencies change', async () => {
    const first = deferred<string>()
    const second = deferred<string>()
    const signals: AbortSignal[] = []
    const fetcher = vi.fn((value: string, signal?: AbortSignal) => {
      if (signal) signals.push(signal)
      return value === 'first' ? first.promise : second.promise
    })

    const { result, rerender } = renderHook(
      ({ value }) => useApi((signal) => fetcher(value, signal), [value]),
      { initialProps: { value: 'first' } },
    )

    rerender({ value: 'second' })
    expect(signals[0].aborted).toBe(true)

    await act(async () => {
      second.resolve('new data')
      await second.promise
    })
    await waitFor(() => expect(result.current.data).toBe('new data'))

    await act(async () => {
      first.resolve('old data')
      await first.promise
    })

    expect(result.current.data).toBe('new data')
  })

  it('keeps previous data when a refetch fails', async () => {
    const first = deferred<string>()
    const second = deferred<string>()
    let call = 0
    const fetcher = vi.fn(() => {
      call += 1
      return call === 1 ? first.promise : second.promise
    })

    const { result } = renderHook(() => useApi(fetcher, []))

    await act(async () => {
      first.resolve('loaded data')
      await first.promise
    })
    await waitFor(() => expect(result.current.data).toBe('loaded data'))

    act(() => result.current.refetch())
    await act(async () => {
      second.reject(new Error('network failed'))
      await second.promise.catch(() => {})
    })

    await waitFor(() => expect(result.current.error?.message).toBe('network failed'))
    expect(result.current.data).toBe('loaded data')
    expect(result.current.loading).toBe(false)
  })
  it('preserves the same error through retries and clears it only after success', async () => {
    const retry = deferred<string>()
    const failure = new Error('Offline')
    const fetcher = vi.fn().mockRejectedValueOnce(failure).mockReturnValueOnce(retry.promise)
    const { result } = renderHook(() => useApi(fetcher, []))
    await waitFor(() => expect(result.current.error).toBe(failure))
    act(() => result.current.refetch())
    expect(result.current.error).toBe(failure)
    await act(async () => { retry.resolve('online'); await retry.promise })
    expect(result.current.error).toBeNull()
    expect(result.current.data).toBe('online')
  })

  it('clears another resource error immediately while the new resource is pending', async () => {
    const next = deferred<string>()
    const fetcher = vi.fn().mockRejectedValueOnce(new Error('NAS unavailable')).mockReturnValueOnce(next.promise)
    const { result, rerender } = renderHook(({ id }) => useApi(fetcher, [id]), { initialProps: { id: 'nas' } })
    await waitFor(() => expect(result.current.error?.message).toBe('NAS unavailable'))
    rerender({ id: 'cloud' })
    expect(result.current.error).toBeNull()
    expect(result.current.loading).toBe(true)
    await act(async () => { next.resolve('Cloud'); await next.promise })
    expect(result.current.data).toBe('Cloud')
  })

  it('reuses a fresh resource on reactivation but always loads an invalidated resource', async () => {
    const fetcher = vi.fn().mockResolvedValue('snapshots')
    const { rerender } = renderHook(({ enabled, revision }) => useApi(fetcher, [revision], { enabled, staleTime: 60000 }), { initialProps: { enabled: true, revision: 0 } })
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1))
    rerender({ enabled: false, revision: 0 })
    rerender({ enabled: true, revision: 0 })
    expect(fetcher).toHaveBeenCalledTimes(1)
    rerender({ enabled: false, revision: 1 })
    expect(fetcher).toHaveBeenCalledTimes(1)
    rerender({ enabled: true, revision: 1 })
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2))
  })

  it('preserves the same resource error across invalidation revisions but resets a changed identity', async () => {
    const pending = deferred<string>()
    const fetcher = vi.fn().mockRejectedValueOnce(new Error('Policy unavailable')).mockReturnValue(pending.promise)
    const { result, rerender } = renderHook(({ resourceKey, revision }) => useApi(fetcher, [resourceKey, revision], { resourceKey }), { initialProps: { resourceKey: 'policy', revision: 0 } })
    await waitFor(() => expect(result.current.error?.message).toBe('Policy unavailable'))
    rerender({ resourceKey: 'policy', revision: 1 })
    expect(result.current.error?.message).toBe('Policy unavailable')
    expect(result.current.loading).toBe(true)
    rerender({ resourceKey: 'other-policy', revision: 1 })
    expect(result.current.error).toBeNull()
    await act(async () => { pending.resolve('loaded'); await pending.promise })
  })

})


describe('Background API refreshes', () => {
  it('keeps an error visible without manual loading feedback and lets Retry supersede polling', async () => {
    const background = deferred<string>()
    const manual = deferred<string>()
    const signals: AbortSignal[] = []
    const fetcher = vi.fn((signal?: AbortSignal): Promise<string> => { if (signal) signals.push(signal); return Promise.reject(new Error('Offline')) })
    const { result } = renderHook(() => useApi(fetcher, []))
    await waitFor(() => expect(result.current.error?.message).toBe('Offline'))
    fetcher.mockImplementationOnce((signal) => { if (signal) signals.push(signal); return background.promise })
    act(() => result.current.backgroundRefetch())
    expect(result.current.loading).toBe(false)
    expect(result.current.backgroundLoading).toBe(true)
    expect(result.current.error?.message).toBe('Offline')
    act(() => result.current.backgroundRefetch())
    expect(fetcher).toHaveBeenCalledTimes(2)
    fetcher.mockImplementationOnce((signal) => { if (signal) signals.push(signal); return manual.promise })
    act(() => result.current.refetch())
    expect(signals[1].aborted).toBe(true)
    expect(result.current.loading).toBe(true)
    await act(async () => { manual.resolve('Recovered'); await manual.promise })
    expect(result.current.data).toBe('Recovered')
    expect(result.current.backgroundLoading).toBe(false)
    await act(async () => { background.resolve('Stale'); await background.promise })
    expect(result.current.data).toBe('Recovered')
  })
})
