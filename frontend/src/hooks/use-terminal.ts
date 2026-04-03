import { useCallback, useEffect, useRef, useState } from 'react'
import { useWs } from '@/contexts/ws-context'
import type { TerminalExitedPayload, TerminalOpenedPayload, WsServerFrame } from '@/lib/ws-types'

type TerminalState = 'idle' | 'connecting' | 'connected' | 'ended' | 'error'

interface UseTerminalOptions {
  stackId: string
  containerId: string
  shell?: string
  cols?: number
  rows?: number
}

export function useTerminal({ stackId, containerId, shell = '/bin/sh', cols = 120, rows = 36 }: UseTerminalOptions) {
  const { connected, send, subscribe } = useWs()
  const [termState, setTermState] = useState<TerminalState>('idle')
  const [exitInfo, setExitInfo] = useState<TerminalExitedPayload | null>(null)
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const sessionIdRef = useRef<string | null>(null)
  const streamIdRef = useRef(`term_${stackId}_${containerId}_${Date.now()}`)
  const onDataRef = useRef<((data: string) => void) | null>(null)
  const requestIdRef = useRef(0)

  const open = useCallback(() => {
    if (!connected) return
    const streamId = streamIdRef.current
    const reqId = `req_term_open_${++requestIdRef.current}`

    setTermState('connecting')
    setExitInfo(null)
    setErrorMessage(null)

    send({
      type: 'terminal.open',
      request_id: reqId,
      stream_id: streamId,
      payload: {
        stack_id: stackId,
        container_id: containerId,
        shell,
        cols,
        rows,
      },
    })
  }, [connected, send, stackId, containerId, shell, cols, rows])

  const attach = useCallback((sessionId: string) => {
    if (!connected) return
    const streamId = streamIdRef.current
    const reqId = `req_term_attach_${++requestIdRef.current}`

    setTermState('connecting')

    send({
      type: 'terminal.attach',
      request_id: reqId,
      stream_id: streamId,
      payload: { session_id: sessionId, cols, rows },
    })
  }, [connected, send, cols, rows])

  const write = useCallback((data: string) => {
    if (!sessionIdRef.current) return
    send({
      type: 'terminal.input',
      stream_id: streamIdRef.current,
      payload: { session_id: sessionIdRef.current, data },
    })
  }, [send])

  const resize = useCallback((newCols: number, newRows: number) => {
    if (!sessionIdRef.current) return
    send({
      type: 'terminal.resize',
      stream_id: streamIdRef.current,
      payload: { session_id: sessionIdRef.current, cols: newCols, rows: newRows },
    })
  }, [send])

  const close = useCallback(() => {
    if (!sessionIdRef.current) return
    send({
      type: 'terminal.close',
      request_id: `req_term_close_${++requestIdRef.current}`,
      stream_id: streamIdRef.current,
      payload: { session_id: sessionIdRef.current },
    })
    sessionIdRef.current = null
    setTermState('ended')
  }, [send])

  useEffect(() => {
    const streamId = streamIdRef.current

    return subscribe(streamId, (frame: WsServerFrame) => {
      switch (frame.type) {
        case 'terminal.opened': {
          const p = frame.payload as unknown as TerminalOpenedPayload
          sessionIdRef.current = p.session_id
          setTermState('connected')
          break
        }
        case 'terminal.output': {
          const data = (frame.payload as { data: string })?.data
          if (data) onDataRef.current?.(data)
          break
        }
        case 'terminal.exited': {
          const p = frame.payload as unknown as TerminalExitedPayload
          sessionIdRef.current = null
          setExitInfo(p)
          setTermState('ended')
          break
        }
        case 'error': {
          setErrorMessage(frame.error?.message ?? 'Terminal error')
          setTermState('error')
          break
        }
      }
    })
  }, [subscribe])

  return {
    state: termState,
    exitInfo,
    errorMessage,
    sessionId: sessionIdRef.current,
    open,
    attach,
    write,
    resize,
    close,
    onData: (cb: (data: string) => void) => { onDataRef.current = cb },
  }
}
