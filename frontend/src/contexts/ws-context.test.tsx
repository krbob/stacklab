import { act, render } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { WsProvider } from './ws-context'

class MockWebSocket {
  static readonly OPEN = 1
  static instances: MockWebSocket[] = []

  readonly url: string
  readyState = 0
  onopen: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onclose: ((event: CloseEvent) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  send = vi.fn()
  close = vi.fn()

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }

  emitClose(code: number) {
    this.onclose?.(new CloseEvent('close', { code }))
  }
}

const mockFetch = vi.fn()

describe('WsProvider session verification', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    MockWebSocket.instances = []
    mockFetch.mockReset()
    vi.stubGlobal('WebSocket', MockWebSocket)
    vi.stubGlobal('fetch', mockFetch)
    window.history.replaceState({}, '', '/stacks')
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('reconnects instead of ending the session when its verification endpoint returns 500', async () => {
    mockFetch.mockResolvedValue({ status: 500, ok: false })
    render(<WsProvider authenticated><div>App</div></WsProvider>)

    expect(MockWebSocket.instances).toHaveLength(1)
    await act(async () => {
      MockWebSocket.instances[0].emitClose(1006)
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(mockFetch).toHaveBeenCalledWith('/api/session', { credentials: 'same-origin' })
    expect(window.location.pathname).toBe('/stacks')

    act(() => { vi.advanceTimersByTime(999) })
    expect(MockWebSocket.instances).toHaveLength(1)
    act(() => { vi.advanceTimersByTime(1) })
    expect(MockWebSocket.instances).toHaveLength(2)
  })

  it('reconnects after a network failure while verifying the session', async () => {
    mockFetch.mockRejectedValue(new TypeError('Failed to fetch'))
    render(<WsProvider authenticated><div>App</div></WsProvider>)

    await act(async () => {
      MockWebSocket.instances[0].emitClose(1006)
      await Promise.resolve()
      await Promise.resolve()
    })
    act(() => { vi.advanceTimersByTime(1_000) })

    expect(MockWebSocket.instances).toHaveLength(2)
    expect(window.location.pathname).toBe('/stacks')
  })

  it('does not reconnect after an explicit 401 session response', async () => {
    window.history.replaceState({}, '', '/login')
    mockFetch.mockResolvedValue({ status: 401, ok: false })
    render(<WsProvider authenticated><div>App</div></WsProvider>)

    await act(async () => {
      MockWebSocket.instances[0].emitClose(1008)
      await Promise.resolve()
      await Promise.resolve()
    })
    act(() => { vi.advanceTimersByTime(60_000) })

    expect(MockWebSocket.instances).toHaveLength(1)
    expect(window.location.pathname).toBe('/login')
  })
})
