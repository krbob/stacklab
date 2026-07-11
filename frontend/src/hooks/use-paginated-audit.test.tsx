import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { AuditEntry, AuditResponse } from '@/lib/api-types'
import type { AuditQueryParams } from '@/lib/api-client'
import { usePaginatedAudit } from './use-paginated-audit'

function entry(id: string): AuditEntry {
  return {
    id,
    stack_id: 'demo',
    job_id: null,
    action: 'pull',
    requested_by: 'local',
    result: 'failed',
    requested_at: '2026-07-01T00:00:00Z',
    finished_at: '2026-07-01T00:00:01Z',
    duration_ms: 1000,
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

describe('usePaginatedAudit', () => {
  it('reuses the active filters when loading the next page', async () => {
    const loadPage = vi.fn()
      .mockResolvedValueOnce({ items: [entry('first')], next_cursor: 'next' })
      .mockResolvedValueOnce({ items: [entry('second')], next_cursor: null })
    const queryKey = JSON.stringify({ q: 'pull', result: 'failed' } satisfies AuditQueryParams)
    const { result } = renderHook(() => usePaginatedAudit({ queryKey, loadPage }))

    await waitFor(() => expect(result.current.entries.map((item) => item.id)).toEqual(['first']))
    await act(async () => result.current.loadMore())

    expect(result.current.entries.map((item) => item.id)).toEqual(['first', 'second'])
    expect(loadPage).toHaveBeenNthCalledWith(1, { q: 'pull', result: 'failed', limit: 50 }, expect.any(AbortSignal))
    expect(loadPage).toHaveBeenNthCalledWith(2, { q: 'pull', result: 'failed', cursor: 'next', limit: 50 }, expect.any(AbortSignal))
  })

  it('ignores a previous page that resolves after filters change', async () => {
    const staleNextPage = deferred<AuditResponse>()
    const loadPage = vi.fn((params: AuditQueryParams) => {
      if (params.q === 'old' && params.cursor) return staleNextPage.promise
      if (params.q === 'old') return Promise.resolve({ items: [entry('old-first')], next_cursor: 'old-next' })
      return Promise.resolve({ items: [entry('new-first')], next_cursor: null })
    })
    const { result, rerender } = renderHook(
      ({ queryKey }) => usePaginatedAudit({ queryKey, loadPage }),
      { initialProps: { queryKey: JSON.stringify({ q: 'old' }) } },
    )

    await waitFor(() => expect(result.current.hasMore).toBe(true))
    let staleLoad!: Promise<void>
    act(() => { staleLoad = result.current.loadMore() })

    rerender({ queryKey: JSON.stringify({ q: 'new' }) })
    await waitFor(() => expect(result.current.entries.map((item) => item.id)).toEqual(['new-first']))

    staleNextPage.resolve({ items: [entry('old-second')], next_cursor: null })
    await act(async () => staleLoad)
    expect(result.current.entries.map((item) => item.id)).toEqual(['new-first'])
  })
})
