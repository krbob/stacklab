import { createContext, useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import type { WsServerFrame } from '@/lib/ws-types'

type FrameHandler = (frame: WsServerFrame) => void

export interface WsContextValue {
  connected: boolean
  send: (frame: Record<string, unknown>) => void
  subscribe: (streamId: string, handler: FrameHandler) => () => void
}

// eslint-disable-next-line react-refresh/only-export-components
export const WsContext = createContext<WsContextValue | null>(null as WsContextValue | null)

const RECONNECT_DELAYS = [1000, 2000, 5000, 10000, 20000, 30000]

export function WsProvider({ children }: { children: ReactNode }) {
  const [connected, setConnected] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const handlersRef = useRef<Map<string, Set<FrameHandler>>>(new Map())
  const reconnectAttemptRef = useRef(0)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const heartbeatTimerRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const authFailedRef = useRef(false)
  const connectRef = useRef<(() => void) | undefined>(undefined)

  const connect = useCallback(() => {
    if (authFailedRef.current) return

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${protocol}//${window.location.host}/api/ws`)
    wsRef.current = ws

    function scheduleReconnect() {
      if (authFailedRef.current) return
      const delay = RECONNECT_DELAYS[Math.min(reconnectAttemptRef.current, RECONNECT_DELAYS.length - 1)]
      reconnectAttemptRef.current++
      reconnectTimerRef.current = setTimeout(() => connectRef.current?.(), delay)
    }

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

      if (frame.type === 'ping') {
        ws.send(JSON.stringify({ type: 'pong', payload: frame.payload }))
        return
      }

      if (frame.stream_id) {
        const handlers = handlersRef.current.get(frame.stream_id)
        if (handlers) {
          handlers.forEach((h) => h(frame))
        }
      }

      const globalHandlers = handlersRef.current.get('__global__')
      if (globalHandlers) {
        globalHandlers.forEach((h) => h(frame))
      }
    }

    ws.onclose = (event) => {
      setConnected(false)
      wsRef.current = null

      const timer = heartbeatTimerRef.current
      if (timer) {
        clearInterval(timer)
        heartbeatTimerRef.current = undefined
      }

      if (event.code === 1008 || event.code === 4401 || event.code === 1006) {
        fetch('/api/session', { credentials: 'same-origin' })
          .then((res) => {
            if (res.status === 401 || !res.ok) {
              authFailedRef.current = true
              window.location.href = '/login'
            } else {
              scheduleReconnect()
            }
          })
          .catch(() => {
            scheduleReconnect()
          })
        return
      }

      scheduleReconnect()
    }

    ws.onerror = () => {
      ws.close()
    }
  }, [])

  useEffect(() => {
    connectRef.current = connect
    connect()
    return () => {
      authFailedRef.current = true
      const reconnectTimer = reconnectTimerRef.current
      if (reconnectTimer) clearTimeout(reconnectTimer)
      const hbTimer = heartbeatTimerRef.current
      if (hbTimer) clearInterval(hbTimer)
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
