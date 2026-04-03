import { useCallback, useEffect, useRef, useState } from 'react'
import { useWs } from '@/hooks/use-ws'
import type { StatsFrame, WsServerFrame } from '@/lib/ws-types'

interface UseStatsStreamOptions {
  stackId: string
  enabled?: boolean
}

const MAX_HISTORY = 150 // ~5 min at 2s intervals

export function useStatsStream({ stackId, enabled = true }: UseStatsStreamOptions) {
  const { connected, send, subscribe } = useWs()
  const [current, setCurrent] = useState<StatsFrame | null>(null)
  const [history, setHistory] = useState<StatsFrame[]>([])
  const streamId = `stats_${stackId}`
  const requestIdRef = useRef(0)

  const sub = useCallback(() => {
    if (!connected || !enabled) return

    const reqId = `req_stats_${++requestIdRef.current}`
    send({
      type: 'stats.subscribe',
      request_id: reqId,
      stream_id: streamId,
      payload: { stack_id: stackId },
    })
  }, [connected, enabled, send, streamId, stackId])

  useEffect(() => {
    sub()
    const currentReqId = requestIdRef.current
    return () => {
      if (connected) {
        send({
          type: 'stats.unsubscribe',
          request_id: `req_stats_unsub_${currentReqId}`,
          stream_id: streamId,
          payload: {},
        })
      }
    }
  }, [sub, connected, send, streamId])

  useEffect(() => {
    if (!enabled) return

    return subscribe(streamId, (frame: WsServerFrame) => {
      if (frame.type === 'stats.frame' && frame.payload) {
        const statsFrame = frame.payload as unknown as StatsFrame
        setCurrent(statsFrame)
        setHistory((prev) => {
          const next = [...prev, statsFrame]
          return next.length > MAX_HISTORY ? next.slice(-MAX_HISTORY) : next
        })
      }
    })
  }, [subscribe, streamId, enabled])

  return { current, history }
}
