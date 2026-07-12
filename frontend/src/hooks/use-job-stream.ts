import { useCallback, useEffect, useRef, useState } from 'react'
import { useWs } from '@/hooks/use-ws'
import { getJob, getJobEvents } from '@/lib/api-client'
import type { JobDetailResponse, JobHistoryEvent } from '@/lib/api-types'
import type { JobEvent, WsServerFrame } from '@/lib/ws-types'

const FALLBACK_POLL_INTERVAL = 1_000
const TERMINAL_STATES = new Set(['succeeded', 'failed', 'cancelled', 'timed_out'])

interface UseJobStreamOptions {
  jobId: string | null
  enabled?: boolean
}

interface JobStreamState {
  events: JobEvent[]
  jobState: string | null
  forJobId: string | null
}

function toJobEvent(event: JobHistoryEvent, job: JobDetailResponse['job']): JobEvent {
  return {
    job_id: event.job_id,
    stack_id: job.stack_id ?? null,
    action: job.action,
    state: event.state,
    event: event.event,
    message: event.message ?? '',
    data: event.data,
    step: event.step ? {
      index: event.step.index,
      total: event.step.total,
      action: event.step.action,
      target_stack_id: event.step.target_stack_id,
    } : null,
    progress: event.progress ?? null,
    timestamp: event.timestamp,
  }
}

function eventKey(event: Pick<JobEvent, 'event' | 'timestamp' | 'message'>): string {
  return `${event.event}:${event.timestamp}:${event.message ?? ''}`
}

function mergeEvents(existing: JobEvent[], incoming: JobEvent[]): JobEvent[] {
  const merged = new Map(existing.map((event) => [eventKey(event), event]))
  for (const event of incoming) merged.set(eventKey(event), event)
  return Array.from(merged.values())
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
        const dedup = eventKey(event)
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

  // A job must not remain visually pending forever just because its push
  // channel is unavailable. REST is authoritative too, so keep the same
  // stream contract alive by polling until WS reconnects or the job finishes.
  useEffect(() => {
    if (!jobId || connected || !enabled) return

    let cancelled = false
    let pollInFlight = false
    let interval: number | undefined
    const currentJobId = jobId

    const poll = async () => {
      if (cancelled || pollInFlight) return
      pollInFlight = true
      try {
        const [jobResult, eventsResult] = await Promise.allSettled([
          getJob(currentJobId),
          getJobEvents(currentJobId),
        ])
        if (cancelled || activeJobIdRef.current !== currentJobId) return
        if (jobResult.status === 'rejected') return

        const job = jobResult.value.job
        const fallbackEvents = eventsResult.status === 'fulfilled'
          ? eventsResult.value.items.map((event) => toJobEvent(event, job))
          : null

        setStreamState((previous) => {
          const previousEvents = previous.forJobId === currentJobId ? previous.events : []
          const events = fallbackEvents ? mergeEvents(previousEvents, fallbackEvents) : previousEvents
          seenRef.current = new Set(events.map(eventKey))
          return {
            events,
            jobState: job.state,
            forJobId: currentJobId,
          }
        })

        if (TERMINAL_STATES.has(job.state) && interval !== undefined) {
          window.clearInterval(interval)
          interval = undefined
        }
      } finally {
        pollInFlight = false
      }
    }

    void poll()
    interval = window.setInterval(() => { void poll() }, FALLBACK_POLL_INTERVAL)
    return () => {
      cancelled = true
      if (interval !== undefined) window.clearInterval(interval)
    }
  }, [jobId, connected, enabled])

  // Derive clean events/state — reset automatically when jobId doesn't match
  const events = streamState.forJobId === jobId ? streamState.events : []
  const state = streamState.forJobId === jobId ? streamState.jobState : null

  const clear = useCallback(() => {
    setStreamState({ events: [], jobState: null, forJobId: null })
    seenRef.current = new Set()
  }, [])

  return { events, state, clear }
}
