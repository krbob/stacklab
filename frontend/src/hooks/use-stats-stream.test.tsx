import { renderHook, act } from '@testing-library/react'
import { describe, expect, it, beforeEach } from 'vitest'
import { createMockWsProvider, type MockWsControls } from '@/test/mock-ws-provider'
import { useStatsStream } from './use-stats-stream'
import type { WsServerFrame } from '@/lib/ws-types'

let controls: MockWsControls
let Provider: ReturnType<typeof createMockWsProvider>['Provider']

beforeEach(() => {
  const mock = createMockWsProvider()
  controls = mock.controls
  Provider = mock.Provider
})

function statsFrame(streamId: string, cpuPercent: number, timestamp = '2026-01-01T00:00:00Z'): WsServerFrame {
  return {
    type: 'stats.frame',
    stream_id: streamId,
    payload: {
      timestamp,
      stack_totals: {
        cpu_percent: cpuPercent,
        memory_bytes: 256000000,
        memory_limit_bytes: 512000000,
        network_rx_bytes_per_sec: 1024,
        network_tx_bytes_per_sec: 512,
      },
      containers: [{
        container_id: 'abc',
        service_name: 'app',
        cpu_percent: cpuPercent,
        memory_bytes: 256000000,
        memory_limit_bytes: 512000000,
        network_rx_bytes_per_sec: 1024,
        network_tx_bytes_per_sec: 512,
      }],
    },
  }
}

describe('useStatsStream', () => {
  it('subscribes on mount', () => {
    renderHook(() => useStatsStream({ stackId: 'test' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    expect(controls.getSentFrames()[0]).toMatchObject({
      type: 'stats.subscribe',
      payload: { stack_id: 'test' },
    })
  })

  it('receives stats frames', () => {
    const { result } = renderHook(() => useStatsStream({ stackId: 'test' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    act(() => {
      controls.emit('stats_test', statsFrame('stats_test', 2.5))
    })

    expect(result.current.current).not.toBeNull()
    expect(result.current.current!.stack_totals.cpu_percent).toBe(2.5)
    expect(result.current.current!.containers).toHaveLength(1)
  })

  it('accumulates history up to the session history frame cap', () => {
    const { result } = renderHook(() => useStatsStream({ stackId: 'test' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    act(() => {
      for (let i = 0; i < 160; i++) {
        const timestamp = new Date(Date.UTC(2026, 0, 1, 0, 0, i * 2)).toISOString()
        controls.emit('stats_test', statsFrame('stats_test', i, timestamp))
      }
    })

    expect(result.current.history).toHaveLength(150)
    expect(result.current.history[0].stack_totals.cpu_percent).toBe(10)
    expect(result.current.history[149].stack_totals.cpu_percent).toBe(159)
  })

  it('drops frames outside the session history time window', () => {
    const { result } = renderHook(() => useStatsStream({ stackId: 'test' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    act(() => {
      controls.emit('stats_test', statsFrame('stats_test', 1, '2026-01-01T00:00:00Z'))
      controls.emit('stats_test', statsFrame('stats_test', 2, '2026-01-01T00:01:00Z'))
      controls.emit('stats_test', statsFrame('stats_test', 3, '2026-01-01T00:06:01Z'))
    })

    expect(result.current.history).toHaveLength(1)
    expect(result.current.history[0].stack_totals.cpu_percent).toBe(3)
  })

  it('current always reflects the latest frame', () => {
    const { result } = renderHook(() => useStatsStream({ stackId: 'test' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    act(() => {
      controls.emit('stats_test', statsFrame('stats_test', 1.0))
    })
    act(() => {
      controls.emit('stats_test', statsFrame('stats_test', 5.0))
    })

    expect(result.current.current!.stack_totals.cpu_percent).toBe(5.0)
  })

  it('does not subscribe when disabled', () => {
    renderHook(() => useStatsStream({ stackId: 'test', enabled: false }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    expect(controls.getSentFrames()).toHaveLength(0)
  })

  it('unsubscribes on unmount', () => {
    const { unmount } = renderHook(() => useStatsStream({ stackId: 'test' }), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    unmount()

    const unsubFrames = controls.getSentFrames().filter((f) => f.type === 'stats.unsubscribe')
    expect(unsubFrames).toHaveLength(1)
  })
})
