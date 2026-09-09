import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useDashboardStats } from './use-dashboard-stats'

const mockGetStackStats = vi.fn()
vi.mock('@/lib/api-client', () => ({
  getStackStats: (...args: unknown[]) => mockGetStackStats(...args),
}))

const sample = { items: { demo: { cpu_percent: 2, memory_bytes: 1024, sampled_at: '2026-09-09T10:00:00Z' } } }

describe('useDashboardStats', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mockGetStackStats.mockReset().mockResolvedValue(sample)
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('polls every second, pauses while hidden, resumes immediately and aborts on unmount', async () => {
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    const { result, unmount } = renderHook(() => useDashboardStats())
    await act(async () => { await Promise.resolve() })
    expect(result.current.data).toEqual(sample)
    await act(async () => { await vi.advanceTimersByTimeAsync(1_000) })
    expect(mockGetStackStats).toHaveBeenCalledTimes(2)

    hidden.mockReturnValue(true)
    act(() => document.dispatchEvent(new Event('visibilitychange')))
    await act(async () => { await vi.advanceTimersByTimeAsync(3_000) })
    expect(mockGetStackStats).toHaveBeenCalledTimes(2)
    hidden.mockReturnValue(false)
    await act(async () => document.dispatchEvent(new Event('visibilitychange')))
    expect(mockGetStackStats).toHaveBeenCalledTimes(3)

    const signal = mockGetStackStats.mock.calls[0][0] as AbortSignal
    unmount()
    expect(signal.aborted).toBe(true)
    await act(async () => { await vi.advanceTimersByTimeAsync(3_000) })
    expect(mockGetStackStats).toHaveBeenCalledTimes(3)
  })

  it('allows a slow request to finish without overlapping or discarding it', async () => {
    let resolve!: (value: typeof sample) => void
    mockGetStackStats.mockReturnValueOnce(new Promise((done) => { resolve = done }))
    const { result } = renderHook(() => useDashboardStats())
    await act(async () => { await vi.advanceTimersByTimeAsync(3_000) })
    expect(mockGetStackStats).toHaveBeenCalledTimes(1)
    await act(async () => resolve(sample))
    expect(result.current.data).toEqual(sample)
    await act(async () => { await vi.advanceTimersByTimeAsync(1_000) })
    expect(mockGetStackStats).toHaveBeenCalledTimes(2)
  })

  it('retains the last sample on failure and clears the error after recovery', async () => {
    const { result } = renderHook(() => useDashboardStats())
    await act(async () => { await Promise.resolve() })
    mockGetStackStats.mockRejectedValueOnce(new Error('offline'))
    await act(async () => { await vi.advanceTimersByTimeAsync(1_000) })
    expect(result.current.data).toEqual(sample)
    expect(result.current.error?.message).toBe('offline')
    mockGetStackStats.mockResolvedValueOnce({ items: {} })
    await act(async () => { await vi.advanceTimersByTimeAsync(1_000) })
    expect(result.current.error).toBeNull()
    expect(result.current.data).toEqual({ items: {} })
  })
})
