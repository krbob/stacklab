import { useCallback, useEffect, useRef, useState } from 'react'
import { useWs } from '@/hooks/use-ws'
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
  const [sessionId, setSessionId] = useState<string | null>(null)
  const [streamId] = useState(() => `term_${stackId}_${containerId}_${Math.random().toString(36).slice(2)}`)
  const onDataRef = useRef<((data: string) => void) | null>(null)
  const requestIdRef = useRef(0)
  const wasConnectedRef = useRef(false)
  // Store latest cols/rows for use in callbacks without triggering re-render
  const dimsRef = useRef({ cols, rows })

  // Sync dims ref in effect (not during render)
  useEffect(() => {
    dimsRef.current = { cols, rows }
  }, [cols, rows])

  const open = useCallback(() => {
    if (!connected) return
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
        cols: dimsRef.current.cols,
        rows: dimsRef.current.rows,
      },
    })
  }, [connected, send, streamId, stackId, containerId, shell])

  const attach = useCallback((sid: string) => {
    if (!connected) return
    const reqId = `req_term_attach_${++requestIdRef.current}`

    setErrorMessage(null)
    setTermState((current) => (current === 'connected' ? current : 'connecting'))

    send({
      type: 'terminal.attach',
      request_id: reqId,
      stream_id: streamId,
      payload: { session_id: sid, cols: dimsRef.current.cols, rows: dimsRef.current.rows },
    })
  }, [connected, send, streamId])

  // Auto-attach after reconnect if we had an active session
  useEffect(() => {
    const wasConnected = wasConnectedRef.current
    wasConnectedRef.current = connected
    if (connected && !wasConnected && sessionId && termState === 'connected') {
      // Defer to avoid synchronous setState within effect body
      const sid = sessionId
      queueMicrotask(() => attach(sid))
    }
  }, [connected, attach, termState, sessionId])

  const write = useCallback((data: string) => {
    send({
      type: 'terminal.input',
      stream_id: streamId,
      payload: { session_id: sessionId, data },
    })
  }, [send, streamId, sessionId])

  const resize = useCallback((newCols: number, newRows: number) => {
    dimsRef.current = { cols: newCols, rows: newRows }
    if (!sessionId) return
    send({
      type: 'terminal.resize',
      stream_id: streamId,
      payload: { session_id: sessionId, cols: newCols, rows: newRows },
    })
  }, [send, streamId, sessionId])

  const close = useCallback(() => {
    if (!sessionId) return
    send({
      type: 'terminal.close',
      request_id: `req_term_close_${++requestIdRef.current}`,
      stream_id: streamId,
      payload: { session_id: sessionId },
    })
    setSessionId(null)
    setTermState('ended')
  }, [send, streamId, sessionId])

  const onData = useCallback((cb: (data: string) => void) => {
    onDataRef.current = cb
  }, [])

  useEffect(() => {
    return subscribe(streamId, (frame: WsServerFrame) => {
      switch (frame.type) {
        case 'terminal.opened': {
          const p = frame.payload as unknown as TerminalOpenedPayload
          setSessionId(p.session_id)
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
          setSessionId(null)
          setExitInfo(p)
          setTermState('ended')
          break
        }
        case 'error': {
          if (frame.error?.code === 'terminal_session_not_found') {
            setSessionId(null)
            setTermState('ended')
            setErrorMessage('Session ended. Start a new session?')
          } else {
            setErrorMessage(frame.error?.message ?? 'Terminal error')
            setTermState('error')
          }
          break
        }
      }
    })
  }, [subscribe, streamId])

  return {
    state: termState,
    exitInfo,
    errorMessage,
    sessionId,
    open,
    attach,
    write,
    resize,
    close,
    onData,
  }
}
