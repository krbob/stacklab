import { useCallback, useEffect, useRef, useState } from 'react'
import { useWs } from '@/contexts/ws-context'
import type { LogEntry, WsServerFrame } from '@/lib/ws-types'

interface UseLogStreamOptions {
  stackId: string
  serviceNames?: string[]
  tail?: number
  enabled?: boolean
}

export function useLogStream({ stackId, serviceNames = [], tail = 200, enabled = true }: UseLogStreamOptions) {
  const { connected, send, subscribe } = useWs()
  const [entries, setEntries] = useState<LogEntry[]>([])
  const [paused, setPaused] = useState(false)
  const bufferRef = useRef<LogEntry[]>([])
  const streamId = `logs_${stackId}_${serviceNames.join(',') || 'all'}`
  const requestIdRef = useRef(0)
  const hasSubscribedRef = useRef(false)

  const sub = useCallback(() => {
    if (!connected || !enabled) return

    const reqId = `req_logs_${++requestIdRef.current}`
    const isResubscribe = hasSubscribedRef.current

    send({
      type: 'logs.subscribe',
      request_id: reqId,
      stream_id: streamId,
      payload: {
        stack_id: stackId,
        service_names: serviceNames,
        // Only request tail on first subscribe. After reconnect, we already
        // have lines in the buffer — requesting tail again would duplicate them.
        tail: isResubscribe ? 0 : tail,
        timestamps: true,
      },
    })

    hasSubscribedRef.current = true
  }, [connected, enabled, send, streamId, stackId, serviceNames, tail])

  useEffect(() => {
    sub()
    return () => {
      if (connected) {
        send({
          type: 'logs.unsubscribe',
          request_id: `req_logs_unsub_${requestIdRef.current}`,
          stream_id: streamId,
          payload: {},
        })
      }
    }
  }, [sub, connected, send, streamId])

  // Reset first-subscribe flag when stackId or serviceNames change
  useEffect(() => {
    hasSubscribedRef.current = false
  }, [stackId, serviceNames.join(',')])

  useEffect(() => {
    if (!enabled) return

    return subscribe(streamId, (frame: WsServerFrame) => {
      if (frame.type === 'logs.event' && frame.payload?.entries) {
        const newEntries = frame.payload.entries as LogEntry[]
        if (paused) {
          bufferRef.current.push(...newEntries)
        } else {
          setEntries((prev) => {
            const combined = [...prev, ...newEntries]
            return combined.length > 5000 ? combined.slice(-5000) : combined
          })
        }
      }
    })
  }, [subscribe, streamId, paused, enabled])

  const resume = useCallback(() => {
    setPaused(false)
    setEntries((prev) => {
      const combined = [...prev, ...bufferRef.current]
      bufferRef.current = []
      return combined.length > 5000 ? combined.slice(-5000) : combined
    })
  }, [])

  return {
    entries,
    paused,
    pause: () => setPaused(true),
    resume,
    clear: () => { setEntries([]); hasSubscribedRef.current = false },
  }
}
