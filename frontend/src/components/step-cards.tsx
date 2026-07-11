import { useEffect, useId, useMemo, useRef, useState } from 'react'
import type { JobEvent, JobProgress } from '@/lib/ws-types'
import { cn } from '@/lib/cn'

interface StepCardsProps {
  events: JobEvent[]
}

interface StepData {
  index: number
  total: number
  action: string
  targetStackId?: string
  state: 'running' | 'succeeded' | 'failed' | 'queued'
  startedAt: string | null
  finishedAt: string | null
  progress: JobProgress | null
  logLines: { message: string; data?: string | null; type: string }[]
}

const statusDot: Record<string, string> = {
  running: 'animate-pulse bg-[var(--run)]',
  succeeded: 'bg-[var(--ok)]',
  failed: 'bg-[var(--danger)]',
  queued: 'bg-stone-600',
}

const statusLabel: Record<string, { text: string; color: string }> = {
  running: { text: 'Running', color: 'text-[var(--run)]' },
  succeeded: { text: 'Done', color: 'text-[var(--ok)]' },
  failed: { text: 'Failed', color: 'text-[var(--danger)]' },
  queued: { text: 'Queued', color: 'text-[var(--muted)]' },
}

function formatElapsed(startMs: number, endMs: number): string {
  const seconds = Math.floor((endMs - startMs) / 1000)
  if (seconds < 0) return '—'
  if (seconds < 60) return `${seconds}s`
  const mins = Math.floor(seconds / 60)
  return `${mins}m ${seconds % 60}s`
}

export function StepCards({ events }: StepCardsProps) {
  const steps = useMemo(() => buildSteps(events), [events])

  if (steps.length === 0) {
    return <div className="text-xs text-[var(--muted)]">Waiting for steps...</div>
  }

  return (
    <div className="space-y-2">
      {steps.map((step) => (
        <StepCard key={`${step.index}-${step.action}-${step.targetStackId ?? ''}`} step={step} />
      ))}
    </div>
  )
}

function StepCard({ step }: { step: StepData }) {
  const [expanded, setExpanded] = useState(false)
  const [clipped, setClipped] = useState(false)
  const [nowMs, setNowMs] = useState(() => Date.now())
  const previewRef = useRef<HTMLDivElement | null>(null)
  const outputId = useId()
  const status = statusLabel[step.state] ?? statusLabel.queued
  const dot = statusDot[step.state] ?? statusDot.queued

  // A single logical line can wrap to many rendered lines, so "does it fit"
  // must be measured, not derived from the line count.
  useEffect(() => {
    if (expanded) return
    const el = previewRef.current
    if (!el) return
    const measure = () => setClipped(el.scrollHeight > el.clientHeight + 1)
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    return () => observer.disconnect()
  }, [expanded, step.logLines.length])

  useEffect(() => {
    if (step.state !== 'running') return
    const interval = setInterval(() => setNowMs(Date.now()), 1000)
    return () => clearInterval(interval)
  }, [step.state])

  const startMs = step.startedAt ? new Date(step.startedAt).getTime() : 0
  const endMs = step.finishedAt ? new Date(step.finishedAt).getTime() : nowMs
  const elapsed = startMs > 0 ? formatElapsed(startMs, endMs) : '—'

  const previewLines = step.logLines.slice(-2)
  const hasMore = step.logLines.length > 2
  const showToggle = hasMore || clipped || expanded
  const progressValue = step.progress
    ? Math.min(step.progress.total, Math.max(0, step.progress.completed))
    : 0
  const progressPercent = step.progress && step.progress.total > 0
    ? Math.min(100, Math.round((progressValue / step.progress.total) * 100))
    : 0

  return (
    <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-3">
      {/* Header */}
      <div className="flex items-center gap-2">
        <span className={cn('inline-block size-2 shrink-0 rounded-full', dot)} />
        <span className="font-mono text-xs font-medium text-[var(--text)]">{step.action}</span>
        {step.targetStackId && (
          <span className="text-xs text-[var(--muted)]">· {step.targetStackId}</span>
        )}
        <span className="text-xs text-[var(--muted)]">{step.index}/{step.total}</span>
        <span className={cn('ml-auto text-xs', status.color)}>{status.text}</span>
        <span className="text-xs text-[var(--muted)]">{elapsed}</span>
      </div>

      {/* Structured progress meter (running steps with streaming data) */}
      {step.progress && step.state === 'running' && step.progress.total > 0 && (
        <div className="mt-2">
          <div className="flex items-center gap-2 font-mono text-xs tabular-nums text-[var(--muted)]">
            <span className="shrink-0" aria-hidden="true">
              {step.progress.completed}/{step.progress.total} {step.progress.unit}
            </span>
            <span
              role="progressbar"
              aria-label={`${step.action} progress`}
              aria-valuemin={0}
              aria-valuemax={step.progress.total}
              aria-valuenow={progressValue}
              aria-valuetext={`${progressValue} of ${step.progress.total} ${step.progress.unit}`}
              className="h-1 flex-1 overflow-hidden rounded-full bg-[rgba(255,255,255,0.07)]"
            >
              <span
                className="block h-full bg-[var(--accent)] transition-[width] duration-300"
                style={{ width: `${progressPercent}%` }}
                aria-hidden="true"
              />
            </span>
          </div>
          {step.progress.detail && (
            <div className="mt-1 truncate font-mono text-xs text-[var(--muted)]">{step.progress.detail}</div>
          )}
        </div>
      )}

      {/* Output preview / expanded */}
      {step.logLines.length > 0 && (
        <div className="mt-2">
          <div
            id={outputId}
            ref={previewRef}
            className={cn(
              'relative overflow-hidden rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.25)] px-2 py-1.5 font-mono text-xs leading-5 [overflow-wrap:anywhere]',
              !expanded && 'max-h-[4.75rem]',
            )}
          >
            {(expanded ? step.logLines : previewLines).map((line, i) => (
              <div key={i} className={cn(
                line.type === 'job_error' ? 'text-[var(--danger)]' :
                line.type === 'job_warning' ? 'text-[var(--warning)]' :
                'text-[var(--muted)]',
              )}>
                {line.message}
                {line.data && <span className="text-[var(--text)]"> {line.data}</span>}
              </div>
            ))}
            {!expanded && (hasMore || clipped) && (
              <div className="pointer-events-none absolute inset-x-0 bottom-0 h-6 bg-gradient-to-t from-[#100C05] to-transparent" aria-hidden />
            )}
          </div>
          {showToggle && (
            <button
              onClick={() => setExpanded(!expanded)}
              aria-expanded={expanded}
              aria-controls={outputId}
              className="mt-1 text-xs text-[var(--accent)] hover:underline"
            >
              {expanded ? 'Collapse' : hasMore ? `Show all (${step.logLines.length} lines)` : 'Show all'}
            </button>
          )}
        </div>
      )}
    </div>
  )
}

function buildSteps(events: JobEvent[]): StepData[] {
  const stepsMap = new Map<string, StepData>()

  for (const event of events) {
    if (!event.step) continue

    const key = `${event.step.index}-${event.step.action}-${event.step.target_stack_id ?? ''}`

    if (!stepsMap.has(key)) {
      stepsMap.set(key, {
        index: event.step.index,
        total: event.step.total,
        action: event.step.action,
        targetStackId: event.step.target_stack_id,
        state: 'queued',
        startedAt: null,
        finishedAt: null,
        progress: null,
        logLines: [],
      })
    }

    const step = stepsMap.get(key)!

    if (event.event === 'job_step_started') {
      step.state = 'running'
      step.startedAt = event.timestamp
    } else if (event.event === 'job_step_finished') {
      step.state = event.state === 'failed' ? 'failed' : 'succeeded'
      step.finishedAt = event.timestamp
    }

    // Structured progress updates feed the meter, not the log dump.
    if (event.event === 'job_progress' && event.progress) {
      step.progress = event.progress
      continue
    }

    if (event.event === 'job_log' || event.event === 'job_progress' || event.event === 'job_warning' || event.event === 'job_error') {
      step.logLines.push({
        message: event.message,
        data: event.data,
        type: event.event,
      })
    }
  }

  return Array.from(stepsMap.values()).sort((a, b) => a.index - b.index)
}
