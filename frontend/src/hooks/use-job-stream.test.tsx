import { renderHook, act } from '@testing-library/react'
import { describe, expect, it, beforeEach } from 'vitest'
import { createMockWsProvider, type MockWsControls } from '@/test/mock-ws-provider'
import { useJobStream } from './use-job-stream'
import type { WsServerFrame } from '@/lib/ws-types'

let controls: MockWsControls
let Provider: ReturnType<typeof createMockWsProvider>['Provider']

beforeEach(() => {
  const mock = createMockWsProvider()
  controls = mock.controls
  Provider = mock.Provider
})

function jobEvent(jobId: string, event: string, state: string, message: string, ts = '2026-01-01T00:00:00Z'): WsServerFrame {
  return {
    type: 'jobs.event',
    stream_id: `job_${jobId}_progress`,
    payload: {
      job_id: jobId,
      stack_id: 'test',
      action: 'pull',
      state,
      event,
      message,
      data: null,
      step: null,
      timestamp: ts,
    },
  }
}

describe('useJobStream', () => {
  it('subscribes for a job on mount', () => {
    renderHook(() => useJobStream({ jobId: 'job_abc' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    const frames = controls.getSentFrames()
    expect(frames[0]).toMatchObject({
      type: 'jobs.subscribe',
      payload: { job_id: 'job_abc' },
    })
  })

  it('does not subscribe when jobId is null', () => {
    renderHook(() => useJobStream({ jobId: null }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    expect(controls.getSentFrames()).toHaveLength(0)
  })

  it('receives and accumulates events', () => {
    const { result } = renderHook(() => useJobStream({ jobId: 'job_abc' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    const streamId = 'job_job_abc_progress'
    act(() => {
      controls.emit(streamId, jobEvent('job_abc', 'job_started', 'running', 'Started'))
    })
    act(() => {
      controls.emit(streamId, jobEvent('job_abc', 'job_progress', 'running', 'Pulling image', '2026-01-01T00:00:01Z'))
    })

    expect(result.current.events).toHaveLength(2)
    expect(result.current.state).toBe('running')
    expect(result.current.events[0].message).toBe('Started')
    expect(result.current.events[1].message).toBe('Pulling image')
  })

  it('deduplicates events with same event+timestamp+message', () => {
    const { result } = renderHook(() => useJobStream({ jobId: 'job_abc' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    const streamId = 'job_job_abc_progress'
    const event = jobEvent('job_abc', 'job_started', 'running', 'Started')

    act(() => {
      controls.emit(streamId, event)
      controls.emit(streamId, event) // duplicate
    })

    expect(result.current.events).toHaveLength(1)
  })

  it('resets events when jobId changes', () => {
    const { result, rerender } = renderHook(
      ({ jobId }: { jobId: string | null }) => useJobStream({ jobId }),
      {
        wrapper: ({ children }) => <Provider>{children}</Provider>,
        initialProps: { jobId: 'job_abc' as string | null },
      },
    )

    const streamId = 'job_job_abc_progress'
    act(() => {
      controls.emit(streamId, jobEvent('job_abc', 'job_started', 'running', 'Started'))
    })
    expect(result.current.events).toHaveLength(1)

    // Change jobId
    rerender({ jobId: 'job_def' })

    // Events from old job should not be visible
    expect(result.current.events).toHaveLength(0)
    expect(result.current.state).toBeNull()
  })

  it('tracks terminal job states correctly', () => {
    const { result } = renderHook(() => useJobStream({ jobId: 'job_abc' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    const streamId = 'job_job_abc_progress'
    act(() => {
      controls.emit(streamId, jobEvent('job_abc', 'job_started', 'running', 'Started'))
    })
    expect(result.current.state).toBe('running')

    act(() => {
      controls.emit(streamId, jobEvent('job_abc', 'job_finished', 'succeeded', 'Done', '2026-01-01T00:00:01Z'))
    })
    expect(result.current.state).toBe('succeeded')
  })

  it('clear resets everything', () => {
    const { result } = renderHook(() => useJobStream({ jobId: 'job_abc' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    const streamId = 'job_job_abc_progress'
    act(() => {
      controls.emit(streamId, jobEvent('job_abc', 'job_started', 'running', 'Started'))
    })
    expect(result.current.events).toHaveLength(1)

    act(() => { result.current.clear() })
    expect(result.current.events).toHaveLength(0)
    expect(result.current.state).toBeNull()
  })

  it('unsubscribes on unmount', () => {
    const { unmount } = renderHook(() => useJobStream({ jobId: 'job_abc' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    unmount()

    const unsubFrames = controls.getSentFrames().filter((f) => f.type === 'jobs.unsubscribe')
    expect(unsubFrames).toHaveLength(1)
  })

  it('does not duplicate replayed events after reconnect', () => {
    const { result } = renderHook(
      () => useJobStream({ jobId: 'job_replay' }),
      {
        wrapper: ({ children }) => <Provider initialConnected={false}>{children}</Provider>,
      },
    )

    const streamId = 'job_job_replay_progress'

    // Connect — get initial events
    act(() => { controls.setConnected(true) })
    act(() => {
      controls.emit(streamId, jobEvent('job_replay', 'job_started', 'running', 'Started', '2026-01-01T00:00:01Z'))
      controls.emit(streamId, jobEvent('job_replay', 'job_progress', 'running', 'Pulling', '2026-01-01T00:00:02Z'))
    })
    expect(result.current.events).toHaveLength(2)
    expect(result.current.state).toBe('running')

    // Disconnect + reconnect
    act(() => { controls.setConnected(false) })
    act(() => { controls.setConnected(true) })

    // Backend replays old events plus the new terminal event.
    act(() => {
      controls.emit(streamId, jobEvent('job_replay', 'job_started', 'running', 'Started', '2026-01-01T00:00:01Z'))
      controls.emit(streamId, jobEvent('job_replay', 'job_progress', 'running', 'Pulling', '2026-01-01T00:00:02Z'))
      controls.emit(streamId, jobEvent('job_replay', 'job_finished', 'succeeded', 'Done', '2026-01-01T00:00:03Z'))
    })

    expect(result.current.state).toBe('succeeded')
    expect(result.current.events).toHaveLength(3)
    expect(result.current.events.map((event) => event.message)).toEqual(['Started', 'Pulling', 'Done'])
  })

  it('deduplicates within a single connection session', () => {
    const { result } = renderHook(
      () => useJobStream({ jobId: 'job_dedup' }),
      {
        wrapper: ({ children }) => <Provider>{children}</Provider>,
      },
    )

    const streamId = 'job_job_dedup_progress'

    // Same event emitted twice in one session
    act(() => {
      controls.emit(streamId, jobEvent('job_dedup', 'job_started', 'running', 'Started', '2026-01-01T00:00:01Z'))
      controls.emit(streamId, jobEvent('job_dedup', 'job_started', 'running', 'Started', '2026-01-01T00:00:01Z'))
      controls.emit(streamId, jobEvent('job_dedup', 'job_finished', 'succeeded', 'Done', '2026-01-01T00:00:02Z'))
    })

    expect(result.current.events).toHaveLength(2)
  })

  it('resubscribes after reconnect', () => {
    renderHook(
      () => useJobStream({ jobId: 'job_resub' }),
      {
        wrapper: ({ children }) => <Provider initialConnected={false}>{children}</Provider>,
      },
    )

    // First connect
    act(() => { controls.setConnected(true) })
    const firstSubs = controls.getSentFrames().filter((f) => f.type === 'jobs.subscribe')
    expect(firstSubs).toHaveLength(1)

    // Disconnect + reconnect
    act(() => { controls.setConnected(false) })
    act(() => { controls.setConnected(true) })

    const allSubs = controls.getSentFrames().filter((f) => f.type === 'jobs.subscribe')
    expect(allSubs.length).toBeGreaterThanOrEqual(2)
  })
})
