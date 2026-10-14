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

})
