import { useContext, useEffect, useRef, useState } from 'react'
import { WsContext } from '@/contexts/ws-context'
import { getActiveJobs } from '@/lib/api-client'
import type { ActiveJobsResponse } from '@/lib/api-types'
import type { WsServerFrame } from '@/lib/ws-types'

const FALLBACK_POLL_INTERVAL = 3_000

// Live view of background jobs: pushed over the multiplexed socket
// (activity.snapshot + activity.update); falls back to REST polling while the
// socket is down so the UI never goes blind (Slice D).
export function useActivityStream(): ActiveJobsResponse | null {
  // Context is optional: without a WsProvider (tests, degraded boot) the hook
  // behaves as permanently disconnected and polls.
  const ws = useContext(WsContext)
  const connected = ws?.connected ?? false
  const send = ws?.send
  const subscribe = ws?.subscribe
  const [response, setResponse] = useState<ActiveJobsResponse | null>(null)
  // Older backends do not know activity.subscribe; on a stream error we fall
  // back to REST polling for the rest of the session.
  const [pushUnsupported, setPushUnsupported] = useState(false)
  const responseRef = useRef<ActiveJobsResponse | null>(null)

  useEffect(() => {
    responseRef.current = response
  }, [response])

  useEffect(() => {
    if (!connected || !send || !subscribe || pushUnsupported) {
      let cancelled = false
      const poll = () => {
        getActiveJobs()
          .then((data) => {
            if (!cancelled) setResponse(data)
          })
          .catch(() => {})
      }
      poll()
      const interval = setInterval(poll, FALLBACK_POLL_INTERVAL)
      return () => {
        cancelled = true
        clearInterval(interval)
      }
    }

    const streamId = 'activity_global'
    send({
      type: 'activity.subscribe',
      request_id: 'req_activity_sub',
      stream_id: streamId,
      payload: {},
    })

    const unsub = subscribe(streamId, (frame: WsServerFrame) => {
      if ((frame.type === 'activity.snapshot' || frame.type === 'activity.update') && frame.payload) {
        setResponse(frame.payload as unknown as ActiveJobsResponse)
      }
      if (frame.type === 'error') {
        setPushUnsupported(true)
      }
    })

    return () => {
      unsub()
      if (connected) {
        send({
          type: 'activity.unsubscribe',
          request_id: 'req_activity_unsub',
          stream_id: streamId,
          payload: {},
        })
      }
    }
  }, [connected, send, subscribe, pushUnsupported])

  return response
}
