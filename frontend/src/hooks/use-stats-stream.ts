import { useCallback, useEffect, useRef, useState } from 'react'
import { useWs } from '@/hooks/use-ws'
import type { StatsFrame, WsServerFrame } from '@/lib/ws-types'

interface UseStatsStreamOptions {
  stackId: string
  enabled?: boolean
}

export const STATS_HISTORY_WINDOW_MS = 5 * 60 * 1000
const STATS_HISTORY_MAX_FRAMES = 150

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
          const latestTimestamp = Date.parse(statsFrame.timestamp)
          const windowed = Number.isFinite(latestTimestamp)
            ? next.filter((item) => Date.parse(item.timestamp) >= latestTimestamp - STATS_HISTORY_WINDOW_MS)
            : next
          return windowed.length > STATS_HISTORY_MAX_FRAMES
            ? windowed.slice(-STATS_HISTORY_MAX_FRAMES)
            : windowed
        })
      }
    })
  }, [subscribe, streamId, enabled])

  return { current, history }
}
