import { renderHook, act } from '@testing-library/react'
import { describe, expect, it, beforeEach, vi } from 'vitest'
import { createMockWsProvider, type MockWsControls } from '@/test/mock-ws-provider'
import { useTerminal } from './use-terminal'
import type { WsServerFrame } from '@/lib/ws-types'

let controls: MockWsControls
let Provider: ReturnType<typeof createMockWsProvider>['Provider']

beforeEach(() => {
  const mock = createMockWsProvider()
  controls = mock.controls
  Provider = mock.Provider
})

describe('useTerminal', () => {
  const defaultOpts = { stackId: 'test', containerId: 'abc123' }

  it('starts in idle state', () => {
    const { result } = renderHook(() => useTerminal(defaultOpts), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    expect(result.current.state).toBe('idle')
    expect(result.current.sessionId).toBeNull()
  })

  it('sends terminal.open on open()', () => {
    const { result } = renderHook(() => useTerminal(defaultOpts), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    act(() => { result.current.open() })

    expect(result.current.state).toBe('connecting')

    const openFrames = controls.getSentFrames().filter((f) => f.type === 'terminal.open')
    expect(openFrames).toHaveLength(1)
    expect(openFrames[0]).toMatchObject({
      payload: expect.objectContaining({
        stack_id: 'test',
        container_id: 'abc123',
        shell: '/bin/sh',
      }),
    })
  })

  it('transitions to connected on terminal.opened', () => {
    const { result } = renderHook(() => useTerminal(defaultOpts), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    act(() => { result.current.open() })

    // Get the streamId from the sent frame
    const openFrame = controls.getSentFrames().find((f) => f.type === 'terminal.open')!
    const streamId = openFrame.stream_id as string

    act(() => {
      controls.emit(streamId, {
        type: 'terminal.opened',
        stream_id: streamId,
        payload: {
          session_id: 'term_xyz',
          container_id: 'abc123',
          shell: '/bin/sh',
        },
      })
    })

    expect(result.current.state).toBe('connected')
    expect(result.current.sessionId).toBe('term_xyz')
  })

  it('forwards terminal output to onData callback', () => {
    const onDataSpy = vi.fn()
    const { result } = renderHook(() => useTerminal(defaultOpts), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    act(() => { result.current.onData(onDataSpy) })
    act(() => { result.current.open() })

    const openFrame = controls.getSentFrames().find((f) => f.type === 'terminal.open')!
    const streamId = openFrame.stream_id as string

    act(() => {
      controls.emit(streamId, {
        type: 'terminal.opened',
        stream_id: streamId,
        payload: { session_id: 'term_xyz', container_id: 'abc123', shell: '/bin/sh' },
      })
    })

    act(() => {
      controls.emit(streamId, {
        type: 'terminal.output',
        stream_id: streamId,
        payload: { session_id: 'term_xyz', data: 'hello\r\n' },
      })
    })

    expect(onDataSpy).toHaveBeenCalledWith('hello\r\n')
  })

  it('sends terminal.input on write()', () => {
    const { result } = renderHook(() => useTerminal(defaultOpts), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    act(() => { result.current.open() })

    const openFrame = controls.getSentFrames().find((f) => f.type === 'terminal.open')!
    const streamId = openFrame.stream_id as string

    act(() => {
      controls.emit(streamId, {
        type: 'terminal.opened',
        stream_id: streamId,
        payload: { session_id: 'term_xyz', container_id: 'abc123', shell: '/bin/sh' },
      })
    })

    act(() => { result.current.write('ls\r') })

    const inputFrames = controls.getSentFrames().filter((f) => f.type === 'terminal.input')
    expect(inputFrames).toHaveLength(1)
    expect(inputFrames[0]).toMatchObject({
      payload: expect.objectContaining({
        session_id: 'term_xyz',
        data: 'ls\r',
      }),
    })
  })

  it('transitions to ended on terminal.exited', () => {
    const { result } = renderHook(() => useTerminal(defaultOpts), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    act(() => { result.current.open() })

    const openFrame = controls.getSentFrames().find((f) => f.type === 'terminal.open')!
    const streamId = openFrame.stream_id as string

    act(() => {
      controls.emit(streamId, {
        type: 'terminal.opened',
        stream_id: streamId,
        payload: { session_id: 'term_xyz', container_id: 'abc123', shell: '/bin/sh' },
      })
    })

    act(() => {
      controls.emit(streamId, {
        type: 'terminal.exited',
        stream_id: streamId,
        payload: { session_id: 'term_xyz', exit_code: 0, reason: 'process_exit' },
      })
    })

    expect(result.current.state).toBe('ended')
    expect(result.current.sessionId).toBeNull()
    expect(result.current.exitInfo).toMatchObject({
      exit_code: 0,
      reason: 'process_exit',
    })
  })

  it('transitions to ended on terminal_session_not_found error', () => {
    const { result } = renderHook(() => useTerminal(defaultOpts), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    act(() => { result.current.open() })

    const openFrame = controls.getSentFrames().find((f) => f.type === 'terminal.open')!
    const streamId = openFrame.stream_id as string

    act(() => {
      controls.emit(streamId, {
        type: 'error',
        stream_id: streamId,
        error: { code: 'terminal_session_not_found', message: 'Session not found' },
      } as WsServerFrame)
    })

    expect(result.current.state).toBe('ended')
    expect(result.current.errorMessage).toBe('Session ended. Start a new session?')
  })

  it('transitions to error on other errors', () => {
    const { result } = renderHook(() => useTerminal(defaultOpts), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    act(() => { result.current.open() })

    const openFrame = controls.getSentFrames().find((f) => f.type === 'terminal.open')!
    const streamId = openFrame.stream_id as string

    act(() => {
      controls.emit(streamId, {
        type: 'error',
        stream_id: streamId,
        error: { code: 'internal_error', message: 'Something broke' },
      } as WsServerFrame)
    })

    expect(result.current.state).toBe('error')
    expect(result.current.errorMessage).toBe('Something broke')
  })

  it('sends terminal.close on close()', () => {
    const { result } = renderHook(() => useTerminal(defaultOpts), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    act(() => { result.current.open() })

    const openFrame = controls.getSentFrames().find((f) => f.type === 'terminal.open')!
    const streamId = openFrame.stream_id as string

    act(() => {
      controls.emit(streamId, {
        type: 'terminal.opened',
        stream_id: streamId,
        payload: { session_id: 'term_xyz', container_id: 'abc123', shell: '/bin/sh' },
      })
    })

    act(() => { result.current.close() })

    expect(result.current.state).toBe('ended')
    expect(result.current.sessionId).toBeNull()

    const closeFrames = controls.getSentFrames().filter((f) => f.type === 'terminal.close')
    expect(closeFrames).toHaveLength(1)
    expect(closeFrames[0]).toMatchObject({
      payload: expect.objectContaining({ session_id: 'term_xyz' }),
    })
  })

  it('sends terminal.resize on resize()', () => {
    const { result } = renderHook(() => useTerminal(defaultOpts), {
      wrapper: ({ children }) => <Provider>{children}</Provider>,
    })

    act(() => { result.current.open() })

    const openFrame = controls.getSentFrames().find((f) => f.type === 'terminal.open')!
    const streamId = openFrame.stream_id as string

    act(() => {
      controls.emit(streamId, {
        type: 'terminal.opened',
        stream_id: streamId,
        payload: { session_id: 'term_xyz', container_id: 'abc123', shell: '/bin/sh' },
      })
    })

    act(() => { result.current.resize(200, 50) })

    const resizeFrames = controls.getSentFrames().filter((f) => f.type === 'terminal.resize')
    expect(resizeFrames).toHaveLength(1)
    expect(resizeFrames[0]).toMatchObject({
      payload: expect.objectContaining({ cols: 200, rows: 50 }),
    })
  })

  it('does not open when disconnected', () => {
    const { result } = renderHook(() => useTerminal(defaultOpts), {
      wrapper: ({ children }) => <Provider initialConnected={false}>{children}</Provider>,
    })

    act(() => { result.current.open() })

    // Should stay idle — no frame sent
    expect(result.current.state).toBe('idle')
    expect(controls.getSentFrames()).toHaveLength(0)
  })
})
