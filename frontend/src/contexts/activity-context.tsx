import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { WsContext } from '@/contexts/ws-context'
import { getActiveJobs } from '@/lib/api-client'
import type { ActiveJobsResponse } from '@/lib/api-types'
import type { WsServerFrame } from '@/lib/ws-types'

const FALLBACK_POLL_INTERVAL = 3_000

export type ActivityFreshness = 'loading' | 'fresh' | 'stale' | 'unavailable'

export interface ActivityContextValue {
  response: ActiveJobsResponse | null
  freshness: ActivityFreshness
  updatedAt: number | null
}

// eslint-disable-next-line react-refresh/only-export-components
export const ActivityContext = createContext<ActivityContextValue>({
  response: null,
  freshness: 'loading',
  updatedAt: null,
})

// Single shared subscription to the global activity stream. Multiple
// consumers (sidebar activity, host strip, mobile drawer) must not each
// subscribe: the backend keys subscriptions by stream_id, so a second
// subscribe/unsubscribe pair would tear down the others' feed.
export function ActivityProvider({ children }: { children: ReactNode }) {
  // Context is optional: without a WsProvider (tests, degraded boot) the
  // provider behaves as permanently disconnected and polls.
  const ws = useContext(WsContext)
  const connected = ws?.connected ?? false
  const send = ws?.send
  const subscribe = ws?.subscribe
  const [state, setState] = useState<ActivityContextValue>({
    response: null,
    freshness: 'loading',
    updatedAt: null,
  })
  // Older backends do not know activity.subscribe; on a stream error we fall
  // back to REST polling for the rest of the session.
  const [pushUnsupported, setPushUnsupported] = useState(false)

  useEffect(() => {
    if (!connected || !send || !subscribe || pushUnsupported) {
      let cancelled = false
      let pollInFlight = false
      const poll = () => {
        if (pollInFlight) return
        pollInFlight = true
        getActiveJobs()
          .then((data) => {
            if (!cancelled) {
              setState({
                response: data,
                freshness: 'fresh',
                updatedAt: Date.now(),
              })
            }
          })
          .catch(() => {
            if (!cancelled) {
              setState((current) => ({
                ...current,
                freshness: current.response ? 'stale' : 'unavailable',
              }))
            }
          })
          .finally(() => {
            pollInFlight = false
          })
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
        setState({
          response: frame.payload as unknown as ActiveJobsResponse,
          freshness: 'fresh',
          updatedAt: Date.now(),
        })
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

  return <ActivityContext.Provider value={state}>{children}</ActivityContext.Provider>
}
