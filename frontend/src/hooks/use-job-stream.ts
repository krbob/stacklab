import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useWs } from '@/hooks/use-ws'
import type { JobEvent, WsServerFrame } from '@/lib/ws-types'

interface UseJobStreamOptions {
  jobId: string | null
  enabled?: boolean
}

interface JobStreamState {
  events: JobEvent[]
  jobState: string | null
  forJobId: string | null
}

export function useJobStream({ jobId, enabled = true }: UseJobStreamOptions) {
  const { connected, send, subscribe } = useWs()
  const [streamState, setStreamState] = useState<JobStreamState>({ events: [], jobState: null, forJobId: null })
  const seenRef = useRef<Set<string>>(new Set())

  useEffect(() => {
    seenRef.current = new Set()

    if (!jobId || !connected || !enabled) return

    const streamId = `job_${jobId}_progress`
    const reqId = `req_job_sub_${jobId}`

    send({
      type: 'jobs.subscribe',
      request_id: reqId,
      stream_id: streamId,
      payload: { job_id: jobId },
    })

    const currentJobId = jobId
    const unsub = subscribe(streamId, (frame: WsServerFrame) => {
      if (frame.type === 'jobs.event' && frame.payload) {
        const event = frame.payload as unknown as JobEvent
        const dedup = `${event.event}:${event.timestamp}:${event.message ?? ''}`
        if (seenRef.current.has(dedup)) return
        seenRef.current.add(dedup)

        setStreamState((prev) => {
          // If jobId changed between subscribe and event arrival, ignore
          if (prev.forJobId !== null && prev.forJobId !== currentJobId) return prev
          return {
            events: [...prev.events, event],
            jobState: event.state,
            forJobId: currentJobId,
          }
        })
      }
    })

    const currentReqId = reqId
    return () => {
      unsub()
      if (connected) {
        send({
          type: 'jobs.unsubscribe',
          request_id: `unsub_${currentReqId}`,
          stream_id: streamId,
          payload: {},
        })
      }
    }
  }, [jobId, connected, enabled, send, subscribe])

  // Derive clean events/state — reset automatically when jobId doesn't match
  const events = streamState.forJobId === jobId ? streamState.events : []
  const state = streamState.forJobId === jobId ? streamState.jobState : null

  const clear = useCallback(() => {
    setStreamState({ events: [], jobState: null, forJobId: null })
    seenRef.current = new Set()
  }, [])

  return useMemo(() => ({ events, state, clear }), [events, state, clear])
}
