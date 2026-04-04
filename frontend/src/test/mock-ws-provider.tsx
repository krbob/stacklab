import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { WsContext, type WsContextValue } from '@/contexts/ws-context'
import type { WsServerFrame } from '@/lib/ws-types'

type FrameHandler = (frame: WsServerFrame) => void

export interface MockWsControls {
  emit: (streamId: string, frame: WsServerFrame) => void
  getSentFrames: () => Record<string, unknown>[]
  setConnected: (value: boolean) => void
}

export function createMockWsProvider() {
  const sentFrames: Record<string, unknown>[] = []
  const handlers = new Map<string, Set<FrameHandler>>()
  const connectedRef: { current: ((v: boolean) => void) | null } = { current: null }

  const controls: MockWsControls = {
    emit(streamId, frame) {
      const streamHandlers = handlers.get(streamId)
      if (streamHandlers) {
        streamHandlers.forEach((h) => h(frame))
      }
    },
    getSentFrames() {
      return sentFrames
    },
    setConnected(value) {
      connectedRef.current?.(value)
    },
  }

  function Provider({ children, initialConnected = true }: { children: ReactNode; initialConnected?: boolean }) {
    const [connected, setConnected] = useState(initialConnected)

    useEffect(() => {
      connectedRef.current = setConnected
      return () => { connectedRef.current = null }
    }, [])

    const handlersRef = useRef(handlers)

    const send = useCallback((frame: Record<string, unknown>) => {
      sentFrames.push(frame)
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

    const value: WsContextValue = { connected, send, subscribe }

    return <WsContext.Provider value={value}>{children}</WsContext.Provider>
  }

  return { Provider, controls }
}
