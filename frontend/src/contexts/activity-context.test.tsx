import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ActiveJobsResponse } from '@/lib/api-types'
import { getActiveJobs } from '@/lib/api-client'
import { useActivity } from '@/hooks/use-activity'
import { ActivityProvider } from './activity-context'

vi.mock('@/lib/api-client', () => ({
  getActiveJobs: vi.fn(),
}))

const mockGetActiveJobs = vi.mocked(getActiveJobs)
const emptyResponse: ActiveJobsResponse = {
  items: [],
  summary: {
    active_count: 0,
    running_count: 0,
    queued_count: 0,
    cancel_requested_count: 0,
  },
}
const activeResponse: ActiveJobsResponse = {
  items: [{
    id: 'job_1',
    stack_id: 'demo',
    action: 'pull',
    state: 'running',
    requested_at: '2026-07-12T07:00:00Z',
    started_at: '2026-07-12T07:00:01Z',
  }],
  summary: {
    active_count: 1,
    running_count: 1,
    queued_count: 0,
    cancel_requested_count: 0,
  },
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })

  return { promise, resolve, reject }
}

function ActivityProbe() {
  const activity = useActivity()
  return (
    <div
      data-testid="activity-state"
      data-freshness={activity.freshness}
      data-count={activity.response?.summary.active_count ?? 'none'}
      data-updated={activity.updatedAt ?? 'never'}
    />
  )
}

function renderProvider() {
  return render(
    <ActivityProvider>
      <ActivityProbe />
    </ActivityProvider>,
  )
}

describe('ActivityProvider fallback polling', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mockGetActiveJobs.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('recovers automatically after an unavailable initial snapshot', async () => {
    mockGetActiveJobs
      .mockRejectedValueOnce(new Error('activity endpoint unavailable'))
      .mockResolvedValueOnce(emptyResponse)

    renderProvider()
    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
    })

    const state = screen.getByTestId('activity-state')
    expect(state).toHaveAttribute('data-freshness', 'unavailable')
    expect(state).toHaveAttribute('data-count', 'none')
    expect(mockGetActiveJobs).toHaveBeenCalledTimes(1)

    await act(async () => {
      vi.advanceTimersByTime(2_999)
      await Promise.resolve()
    })
    expect(mockGetActiveJobs).toHaveBeenCalledTimes(1)

    await act(async () => {
      vi.advanceTimersByTime(1)
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(mockGetActiveJobs).toHaveBeenCalledTimes(2)
    expect(state).toHaveAttribute('data-freshness', 'fresh')
    expect(state).toHaveAttribute('data-count', '0')
    expect(state).not.toHaveAttribute('data-updated', 'never')
  })

  it('preserves stale jobs and prevents overlapping fallback polls', async () => {
    const slowRefresh = deferred<ActiveJobsResponse>()
    mockGetActiveJobs
      .mockResolvedValueOnce(activeResponse)
      .mockReturnValueOnce(slowRefresh.promise)
      .mockResolvedValueOnce(emptyResponse)

    renderProvider()
    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
    })
    const state = screen.getByTestId('activity-state')
    expect(state).toHaveAttribute('data-freshness', 'fresh')
    expect(state).toHaveAttribute('data-count', '1')

    await act(async () => {
      vi.advanceTimersByTime(12_000)
      await Promise.resolve()
    })
    expect(mockGetActiveJobs).toHaveBeenCalledTimes(2)
    expect(state).toHaveAttribute('data-count', '1')

    await act(async () => {
      slowRefresh.reject(new Error('poll failed'))
      await Promise.allSettled([slowRefresh.promise])
      await Promise.resolve()
    })
    expect(state).toHaveAttribute('data-freshness', 'stale')
    expect(state).toHaveAttribute('data-count', '1')

    await act(async () => {
      vi.advanceTimersByTime(3_000)
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(mockGetActiveJobs).toHaveBeenCalledTimes(3)
    expect(state).toHaveAttribute('data-freshness', 'fresh')
    expect(state).toHaveAttribute('data-count', '0')
  })
})
