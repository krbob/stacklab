import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import type { WsServerFrame } from '@/lib/ws-types'

type FrameHandler = (frame: WsServerFrame) => void

interface WsContextValue {
  connected: boolean
  send: (frame: Record<string, unknown>) => void
  subscribe: (streamId: string, handler: FrameHandler) => () => void
}

const WsContext = createContext<WsContextValue | null>(null as WsContextValue | null)

const RECONNECT_DELAYS = [1000, 2000, 5000, 10000, 20000, 30000]

export function WsProvider({ children }: { children: ReactNode }) {
  const [connected, setConnected] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const handlersRef = useRef<Map<string, Set<FrameHandler>>>(new Map())
  const reconnectAttemptRef = useRef(0)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const heartbeatTimerRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const authFailedRef = useRef(false)

  const connect = useCallback(() => {
    if (authFailedRef.current) return

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${protocol}//${window.location.host}/api/ws`)
    wsRef.current = ws

    ws.onopen = () => {
      setConnected(true)
      reconnectAttemptRef.current = 0
      authFailedRef.current = false
    }

    ws.onmessage = (event) => {
      let frame: WsServerFrame
      try {
        frame = JSON.parse(event.data)
      } catch {
        return
      }

      // handle heartbeat
      if (frame.type === 'ping') {
        ws.send(JSON.stringify({ type: 'pong', payload: frame.payload }))
        return
      }

      // dispatch to stream handlers
      if (frame.stream_id) {
        const handlers = handlersRef.current.get(frame.stream_id)
        if (handlers) {
          handlers.forEach((h) => h(frame))
        }
      }

      // dispatch to global handlers (no stream_id, e.g. hello, error without stream)
      const globalHandlers = handlersRef.current.get('__global__')
      if (globalHandlers) {
        globalHandlers.forEach((h) => h(frame))
      }
    }

    ws.onclose = (event) => {
      setConnected(false)
      wsRef.current = null

      if (heartbeatTimerRef.current) {
        clearInterval(heartbeatTimerRef.current)
      }

      // 1008 = policy violation (auth rejected), 4401 = custom unauthorized
      // HTTP 401 on upgrade also results in close before open (readyState never reaches OPEN)
      if (event.code === 1008 || event.code === 4401 || event.code === 1006) {
        // Check if this was an auth failure by trying GET /api/session
        fetch('/api/session', { credentials: 'same-origin' })
          .then((res) => {
            if (res.status === 401 || !res.ok) {
              authFailedRef.current = true
              window.location.href = '/login'
            } else {
              // Session is valid, this was a transient failure — reconnect
              scheduleReconnect()
            }
          })
          .catch(() => {
            // Backend unreachable — reconnect, not auth failure
            scheduleReconnect()
          })
        return
      }

      scheduleReconnect()
    }

    ws.onerror = () => {
      ws.close()
    }

    function scheduleReconnect() {
      if (authFailedRef.current) return
      const delay = RECONNECT_DELAYS[Math.min(reconnectAttemptRef.current, RECONNECT_DELAYS.length - 1)]
      reconnectAttemptRef.current++
      reconnectTimerRef.current = setTimeout(connect, delay)
    }
  }, [])

  useEffect(() => {
    connect()
    return () => {
      authFailedRef.current = true // prevent reconnect on unmount
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
      if (heartbeatTimerRef.current) clearInterval(heartbeatTimerRef.current)
      wsRef.current?.close()
    }
  }, [connect])

  const send = useCallback((frame: Record<string, unknown>) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(frame))
    }
  }, [])

  const subscribe = useCallback((streamId: string, handler: FrameHandler) => {
    if (!handlersRef.current.has(streamId)) {
      handlersRef.current.set(streamId, new Set())
    }
    handlersRef.current.get(streamId)!.add(handler)

    return () => {
      const set = handlersRef.current.get(streamId)
      if (set) {
        set.delete(handler)
        if (set.size === 0) handlersRef.current.delete(streamId)
      }
    }
  }, [])

  return (
    <WsContext.Provider value={{ connected, send, subscribe }}>
      {children}
    </WsContext.Provider>
  )
}

export function useWs(): WsContextValue {
  const ctx = useContext(WsContext)
  if (!ctx) throw new Error('useWs must be used within WsProvider')
  return ctx
}
