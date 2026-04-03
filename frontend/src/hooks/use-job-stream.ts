import { useEffect, useRef, useState } from 'react'
import { useWs } from '@/contexts/ws-context'
import type { JobEvent, WsServerFrame } from '@/lib/ws-types'

interface UseJobStreamOptions {
  jobId: string | null
  enabled?: boolean
}

export function useJobStream({ jobId, enabled = true }: UseJobStreamOptions) {
  const { connected, send, subscribe } = useWs()
  const [events, setEvents] = useState<JobEvent[]>([])
  const [state, setState] = useState<string | null>(null)
  const requestIdRef = useRef(0)

  useEffect(() => {
    if (!jobId || !connected || !enabled) return

    const streamId = `job_${jobId}_progress`
    const reqId = `req_job_${++requestIdRef.current}`

    send({
      type: 'jobs.subscribe',
      request_id: reqId,
      stream_id: streamId,
      payload: { job_id: jobId },
    })

    const unsub = subscribe(streamId, (frame: WsServerFrame) => {
      if (frame.type === 'jobs.event' && frame.payload) {
        const event = frame.payload as unknown as JobEvent
        setEvents((prev) => [...prev, event])
        setState(event.state)
      }
    })

    return () => {
      unsub()
      if (connected) {
        send({
          type: 'jobs.unsubscribe',
          request_id: `req_job_unsub_${requestIdRef.current}`,
          stream_id: streamId,
          payload: {},
        })
      }
    }
  }, [jobId, connected, enabled, send, subscribe])

  return { events, state, clear: () => { setEvents([]); setState(null) } }
}
