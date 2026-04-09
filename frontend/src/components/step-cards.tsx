import { useEffect, useMemo, useState } from 'react'
import type { JobEvent } from '@/lib/ws-types'
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
  logLines: { message: string; data?: string | null; type: string }[]
}

const statusDot: Record<string, string> = {
  running: 'animate-pulse bg-sky-400',
  succeeded: 'bg-emerald-400',
  failed: 'bg-red-400',
  queued: 'bg-zinc-600',
}

const statusLabel: Record<string, { text: string; color: string }> = {
  running: { text: 'Running', color: 'text-sky-400' },
  succeeded: { text: 'Done', color: 'text-emerald-400' },
  failed: { text: 'Failed', color: 'text-red-400' },
  queued: { text: 'Queued', color: 'text-zinc-500' },
}

function formatElapsed(startMs: number, endMs: number): string {
  const seconds = Math.round((endMs - startMs) / 1000)
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
        <StepCard key={`${step.index}-${step.action}`} step={step} />
      ))}
    </div>
  )
}

function StepCard({ step }: { step: StepData }) {
  const [expanded, setExpanded] = useState(false)
  const [nowMs, setNowMs] = useState(() => Date.now())
  const status = statusLabel[step.state] ?? statusLabel.queued
  const dot = statusDot[step.state] ?? statusDot.queued

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

      {/* Output preview / expanded */}
      {step.logLines.length > 0 && (
        <div className="mt-2">
          <div className={cn(
            'overflow-hidden rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.25)] px-2 py-1.5 font-mono text-xs leading-5',
            !expanded && 'max-h-14',
          )}>
            {(expanded ? step.logLines : previewLines).map((line, i) => (
              <div key={i} className={cn(
                line.type === 'job_error' ? 'text-red-400' :
                line.type === 'job_warning' ? 'text-amber-400' :
                'text-[var(--muted)]',
              )}>
                {line.message}
                {line.data && <span className="text-[var(--text)]"> {line.data}</span>}
              </div>
            ))}
          </div>
          {hasMore && (
            <button
              onClick={() => setExpanded(!expanded)}
              className="mt-1 text-xs text-[var(--accent)] hover:underline"
            >
              {expanded ? 'Collapse' : `Show all (${step.logLines.length} lines)`}
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
