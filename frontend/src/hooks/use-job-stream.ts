import { useCallback, useEffect, useRef, useState } from 'react'
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
  const activeJobIdRef = useRef<string | null>(jobId)

  useEffect(() => {
    activeJobIdRef.current = jobId
    seenRef.current = new Set()
  }, [jobId])

  useEffect(() => {
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
        if (activeJobIdRef.current !== currentJobId) return

        const event = frame.payload as unknown as JobEvent
        const dedup = `${event.event}:${event.timestamp}:${event.message ?? ''}`
        if (seenRef.current.has(dedup)) return
        seenRef.current.add(dedup)

        setStreamState((prev) => {
          const sameJob = prev.forJobId === currentJobId
          return {
            events: sameJob ? [...prev.events, event] : [event],
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

  return { events, state, clear }
}
