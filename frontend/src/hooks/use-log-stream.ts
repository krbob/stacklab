import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useWs } from '@/hooks/use-ws'
import { parseAnsi } from '@/lib/ansi'
import type { LogEntry, WsServerFrame } from '@/lib/ws-types'

interface UseLogStreamOptions {
  stackId: string
  serviceNames?: string[]
  tail?: number
  enabled?: boolean
}

export function useLogStream({ stackId, serviceNames = [], tail = 200, enabled = true }: UseLogStreamOptions) {
  const { connected, send, subscribe } = useWs()
  const serviceKey = serviceNames.join(',')
  const streamKey = `${stackId}:${serviceKey}`
  const selectedServiceNames = useMemo(() => (serviceKey ? serviceKey.split(',') : []), [serviceKey])
  const streamId = `logs_${stackId}_${serviceKey || 'all'}`
  const [entriesState, setEntriesState] = useState<{ streamKey: string; entries: LogEntry[] }>({ streamKey, entries: [] })
  const [paused, setPaused] = useState(false)
  const bufferRef = useRef<{ streamKey: string; entries: LogEntry[] }>({ streamKey, entries: [] })
  const requestIdRef = useRef(0)
  const subscribedStreamKeyRef = useRef<string | null>(null)

  const sub = useCallback(() => {
    if (!connected || !enabled) return

    const reqId = `req_logs_${++requestIdRef.current}`
    const isResubscribe = subscribedStreamKeyRef.current === streamKey
    if (!isResubscribe) {
      bufferRef.current = { streamKey, entries: [] }
      setEntriesState({ streamKey, entries: [] })
    }

    send({
      type: 'logs.subscribe',
      request_id: reqId,
      stream_id: streamId,
      payload: {
        stack_id: stackId,
        service_names: selectedServiceNames,
        // Only request tail on first subscribe. After reconnect, we already
        // have lines in the buffer — requesting tail again would duplicate them.
        tail: isResubscribe ? 0 : tail,
        timestamps: true,
      },
    })

    subscribedStreamKeyRef.current = streamKey
  }, [connected, enabled, send, streamId, stackId, streamKey, selectedServiceNames, tail])

  useEffect(() => {
    sub()
    const currentReqId = requestIdRef.current
    return () => {
      if (connected) {
        send({
          type: 'logs.unsubscribe',
          request_id: `req_logs_unsub_${currentReqId}`,
          stream_id: streamId,
          payload: {},
        })
      }
    }
  }, [sub, connected, send, streamId])

  useEffect(() => {
    if (!enabled) return

    return subscribe(streamId, (frame: WsServerFrame) => {
      if (frame.type === 'logs.event' && frame.payload?.entries) {
        const newEntries = (frame.payload.entries as LogEntry[]).map((entry) => {
          // Parse ANSI once, at ingestion: `line` becomes the plain text (for
          // filtering) and `spans` carry the colour for rendering.
          const spans = parseAnsi(entry.line)
          return { ...entry, line: spans.map((s) => s.text).join(''), spans }
        })
        if (paused) {
          if (bufferRef.current.streamKey !== streamKey) {
            bufferRef.current = { streamKey, entries: [] }
          }
          bufferRef.current.entries.push(...newEntries)
        } else {
          setEntriesState((prev) => {
            const previousEntries = prev.streamKey === streamKey ? prev.entries : []
            const combined = [...previousEntries, ...newEntries]
            return { streamKey, entries: combined.length > 5000 ? combined.slice(-5000) : combined }
          })
        }
      }
    })
  }, [subscribe, streamId, streamKey, paused, enabled])

  const resume = useCallback(() => {
    setPaused(false)
    setEntriesState((prev) => {
      const previousEntries = prev.streamKey === streamKey ? prev.entries : []
      const bufferedEntries = bufferRef.current.streamKey === streamKey ? bufferRef.current.entries : []
      const combined = [...previousEntries, ...bufferedEntries]
      bufferRef.current = { streamKey, entries: [] }
      return { streamKey, entries: combined.length > 5000 ? combined.slice(-5000) : combined }
    })
  }, [streamKey])

  const entries = entriesState.streamKey === streamKey ? entriesState.entries : []

  return {
    entries,
    paused,
    pause: () => setPaused(true),
    resume,
    clear: () => {
      setEntriesState({ streamKey, entries: [] })
      bufferRef.current = { streamKey, entries: [] }
      subscribedStreamKeyRef.current = null
    },
  }
}
