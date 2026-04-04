import { renderHook, act } from '@testing-library/react'
import { describe, expect, it, beforeEach } from 'vitest'
import { createMockWsProvider, type MockWsControls } from '@/test/mock-ws-provider'
import { useLogStream } from './use-log-stream'
import type { WsServerFrame } from '@/lib/ws-types'

let controls: MockWsControls
let Provider: ReturnType<typeof createMockWsProvider>['Provider']

beforeEach(() => {
  const mock = createMockWsProvider()
  controls = mock.controls
  Provider = mock.Provider
})

function logEvent(streamId: string, serviceName: string, line: string, ts = '2026-01-01T00:00:00Z'): WsServerFrame {
  return {
    type: 'logs.event',
    stream_id: streamId,
    payload: {
      entries: [{
        timestamp: ts,
        service_name: serviceName,
        container_id: 'abc123',
        stream: 'stdout',
        line,
      }],
    },
  }
}

describe('useLogStream', () => {
  it('subscribes on mount with tail', () => {
    renderHook(() => useLogStream({ stackId: 'test', tail: 100 }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    const frames = controls.getSentFrames()
    expect(frames).toHaveLength(1)
    expect(frames[0]).toMatchObject({
      type: 'logs.subscribe',
      payload: expect.objectContaining({
        stack_id: 'test',
        tail: 100,
      }),
    })
  })

  it('receives log entries', () => {
    const { result } = renderHook(() => useLogStream({ stackId: 'test' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    const streamId = 'logs_test_all'
    act(() => {
      controls.emit(streamId, logEvent(streamId, 'app', 'Hello world'))
    })

    expect(result.current.entries).toHaveLength(1)
    expect(result.current.entries[0].line).toBe('Hello world')
    expect(result.current.entries[0].service_name).toBe('app')
  })

  it('buffers entries when paused', () => {
    const { result } = renderHook(() => useLogStream({ stackId: 'test' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    const streamId = 'logs_test_all'

    act(() => {
      controls.emit(streamId, logEvent(streamId, 'app', 'Line 1'))
    })
    expect(result.current.entries).toHaveLength(1)

    act(() => { result.current.pause() })

    act(() => {
      controls.emit(streamId, logEvent(streamId, 'app', 'Line 2'))
      controls.emit(streamId, logEvent(streamId, 'app', 'Line 3'))
    })

    // Entries should not grow while paused
    expect(result.current.entries).toHaveLength(1)

    act(() => { result.current.resume() })

    // After resume, buffered entries are appended
    expect(result.current.entries).toHaveLength(3)
    expect(result.current.entries[1].line).toBe('Line 2')
    expect(result.current.entries[2].line).toBe('Line 3')
  })

  it('sends tail=0 on resubscribe after reconnect', () => {
    // Start disconnected so we control the connect sequence
    renderHook(
      () => useLogStream({ stackId: 'test', tail: 200 }),
      {
        wrapper: ({ children }) => <Provider initialConnected={false}>{children}</Provider>,
      },
    )

    // No subscribe while disconnected
    expect(controls.getSentFrames()).toHaveLength(0)

    // First connect
    act(() => { controls.setConnected(true) })

    const firstSubs = controls.getSentFrames().filter((f) => f.type === 'logs.subscribe')
    expect(firstSubs).toHaveLength(1)
    expect((firstSubs[0].payload as Record<string, unknown>).tail).toBe(200)

    // Disconnect
    act(() => { controls.setConnected(false) })

    // Reconnect — hasSubscribedRef is now true, so tail should be 0
    act(() => { controls.setConnected(true) })

    const allSubs = controls.getSentFrames().filter((f) => f.type === 'logs.subscribe')
    expect(allSubs.length).toBeGreaterThanOrEqual(2)
    expect((allSubs[allSubs.length - 1].payload as Record<string, unknown>).tail).toBe(0)
  })

  it('clear resets entries and hasSubscribed flag', () => {
    const { result } = renderHook(() => useLogStream({ stackId: 'test' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    const streamId = 'logs_test_all'
    act(() => {
      controls.emit(streamId, logEvent(streamId, 'app', 'Line 1'))
    })
    expect(result.current.entries).toHaveLength(1)

    act(() => { result.current.clear() })
    expect(result.current.entries).toHaveLength(0)
  })

  it('does not subscribe when disabled', () => {
    renderHook(() => useLogStream({ stackId: 'test', enabled: false }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    expect(controls.getSentFrames()).toHaveLength(0)
  })

  it('caps entries at 5000', () => {
    const { result } = renderHook(() => useLogStream({ stackId: 'test' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    const streamId = 'logs_test_all'
    act(() => {
      // Send batch of 5001 entries
      const entries = Array.from({ length: 5001 }, (_, i) => ({
        timestamp: '2026-01-01T00:00:00Z',
        service_name: 'app',
        container_id: 'abc',
        stream: 'stdout',
        line: `Line ${i}`,
      }))
      controls.emit(streamId, {
        type: 'logs.event',
        stream_id: streamId,
        payload: { entries },
      })
    })

    expect(result.current.entries).toHaveLength(5000)
    // Should keep the latest, drop the earliest
    expect(result.current.entries[0].line).toBe('Line 1')
  })

  it('preserves existing entries after reconnect without duplicating', () => {
    const { result } = renderHook(
      () => useLogStream({ stackId: 'test', tail: 50 }),
      {
        wrapper: ({ children }) => <Provider initialConnected={false}>{children}</Provider>,
      },
    )

    const streamId = 'logs_test_all'

    // Connect and receive initial entries
    act(() => { controls.setConnected(true) })
    act(() => {
      controls.emit(streamId, logEvent(streamId, 'app', 'Line A', '2026-01-01T00:00:01Z'))
      controls.emit(streamId, logEvent(streamId, 'app', 'Line B', '2026-01-01T00:00:02Z'))
    })
    expect(result.current.entries).toHaveLength(2)

    // Disconnect
    act(() => { controls.setConnected(false) })

    // Reconnect — tail=0 sent, so backend sends only new lines
    act(() => { controls.setConnected(true) })

    // Backend sends only new line (not replaying A and B)
    act(() => {
      controls.emit(streamId, logEvent(streamId, 'app', 'Line C', '2026-01-01T00:00:03Z'))
    })

    // Should have all 3 entries, no duplicates
    expect(result.current.entries).toHaveLength(3)
    expect(result.current.entries[0].line).toBe('Line A')
    expect(result.current.entries[1].line).toBe('Line B')
    expect(result.current.entries[2].line).toBe('Line C')
  })
})
