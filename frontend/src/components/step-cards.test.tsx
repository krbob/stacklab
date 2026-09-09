import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { StepCards } from './step-cards'
import type { JobEvent } from '@/lib/ws-types'

function makeEvent(partial: Partial<JobEvent> & Pick<JobEvent, 'event' | 'state' | 'timestamp'>): JobEvent {
  return {
    job_id: 'job_1',
    stack_id: null,
    action: 'update_stacks',
    message: '',
    data: null,
    ...partial,
  }
}

describe('StepCards', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-04-09T10:00:10Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('groups step logs into the matching step card', () => {
    const events: JobEvent[] = [
      makeEvent({
        event: 'job_step_started',
        state: 'running',
        message: 'Starting pull for demo.',
        timestamp: '2026-04-09T10:00:00Z',
        step: { index: 1, total: 2, action: 'pull', target_stack_id: 'demo' },
      }),
      makeEvent({
        event: 'job_log',
        state: 'running',
        message: 'Pulling demo image',
        data: 'layer 1',
        timestamp: '2026-04-09T10:00:02Z',
        step: { index: 1, total: 2, action: 'pull', target_stack_id: 'demo' },
      }),
      makeEvent({
        event: 'job_step_started',
        state: 'running',
        message: 'Starting up for demo.',
        timestamp: '2026-04-09T10:00:03Z',
        step: { index: 2, total: 2, action: 'up', target_stack_id: 'demo' },
      }),
    ]

    render(<StepCards events={events} />)

    expect(screen.getByText('pull')).toBeInTheDocument()
    expect(screen.getByText('up')).toBeInTheDocument()
    expect(screen.getByText('Pulling demo image')).toBeInTheDocument()
    expect(screen.getByText('layer 1', { exact: false })).toBeInTheDocument()
  })

  it('shows live elapsed time for running steps and freezes when finished', () => {
    const runningEvents: JobEvent[] = [
      makeEvent({
        event: 'job_step_started',
        state: 'running',
        message: 'Starting pull for demo.',
        timestamp: '2026-04-09T10:00:00Z',
        step: { index: 1, total: 1, action: 'pull', target_stack_id: 'demo' },
      }),
    ]

    const { rerender } = render(<StepCards events={runningEvents} />)
    expect(screen.getByText('10s')).toBeInTheDocument()

    act(() => {
      vi.setSystemTime(new Date('2026-04-09T10:00:12Z'))
      vi.advanceTimersByTime(1000)
    })

    expect(screen.getByText((value) => value === '12s' || value === '13s')).toBeInTheDocument()

    const finishedEvents: JobEvent[] = [
      ...runningEvents,
      makeEvent({
        event: 'job_step_finished',
        state: 'succeeded',
        message: 'Finished pull for demo.',
        timestamp: '2026-04-09T10:00:15Z',
        step: { index: 1, total: 1, action: 'pull', target_stack_id: 'demo' },
      }),
    ]

    rerender(<StepCards events={finishedEvents} />)

    act(() => {
      vi.setSystemTime(new Date('2026-04-09T10:00:20Z'))
      vi.advanceTimersByTime(5000)
    })

    expect(screen.getByText('15s')).toBeInTheDocument()
  })

  it('renders a progress meter for running steps with structured progress', () => {
    const events: JobEvent[] = [
      makeEvent({
        event: 'job_step_started',
        state: 'running',
        message: 'Starting pull for demo.',
        timestamp: '2026-04-09T10:00:00Z',
        step: { index: 1, total: 1, action: 'pull', target_stack_id: 'demo' },
      }),
      makeEvent({
        event: 'job_progress',
        state: 'running',
        message: 'Progress pull for demo.',
        timestamp: '2026-04-09T10:00:02Z',
        step: { index: 1, total: 1, action: 'pull', target_stack_id: 'demo' },
        progress: { phase: 'pull', completed: 7, total: 12, unit: 'layers', detail: '0d6922a6b13e extracting' },
      }),
    ]

    render(<StepCards events={events} />)

    expect(screen.getByText('7/12 layers')).toBeInTheDocument()
    expect(screen.getByText('0d6922a6b13e extracting')).toBeInTheDocument()
    const progress = screen.getByRole('progressbar', { name: 'pull progress' })
    expect(progress).toHaveAttribute('aria-valuemin', '0')
    expect(progress).toHaveAttribute('aria-valuemax', '12')
    expect(progress).toHaveAttribute('aria-valuenow', '7')
    expect(progress).toHaveAttribute('aria-valuetext', '7 of 12 layers')
    // structured progress must not land in the log dump
    expect(screen.queryByText('Progress pull for demo.')).not.toBeInTheDocument()
  })

  it('uses explicit step states while the aggregate job continues', () => {
    const events: JobEvent[] = [
      makeEvent({
        event: 'job_step_started',
        state: 'running',
        timestamp: '2026-04-09T10:00:00Z',
        step: { index: 1, total: 3, action: 'pull', state: 'running', target_stack_id: 'alpha' },
      }),
      makeEvent({
        event: 'job_error',
        state: 'running',
        message: 'Pull failed after retries',
        timestamp: '2026-04-09T10:00:03Z',
        step: { index: 1, total: 3, action: 'pull', state: 'failed', target_stack_id: 'alpha' },
      }),
      makeEvent({
        event: 'job_step_finished',
        state: 'running',
        timestamp: '2026-04-09T10:00:03Z',
        step: { index: 1, total: 3, action: 'pull', state: 'failed', target_stack_id: 'alpha' },
      }),
      makeEvent({
        event: 'job_step_finished',
        state: 'running',
        message: 'Skipped dependent up',
        timestamp: '2026-04-09T10:00:03Z',
        step: { index: 2, total: 3, action: 'up', state: 'skipped', target_stack_id: 'alpha' },
      }),
      makeEvent({
        event: 'job_step_started',
        state: 'running',
        timestamp: '2026-04-09T10:00:04Z',
        step: { index: 3, total: 3, action: 'pull', state: 'running', target_stack_id: 'beta' },
      }),
    ]

    render(<StepCards events={events} />)

    expect(screen.getByText('Failed')).toBeInTheDocument()
    expect(screen.getByText('Skipped')).toBeInTheDocument()
    expect(screen.getByText('Running')).toBeInTheDocument()
    expect(screen.getByText('Pull failed after retries')).toBeInTheDocument()
  })

  it('falls back to the historical top-level event state when step state is absent', () => {
    const events: JobEvent[] = [
      makeEvent({
        event: 'job_step_started',
        state: 'running',
        timestamp: '2026-04-09T10:00:00Z',
        step: { index: 1, total: 1, action: 'pull', target_stack_id: 'demo' },
      }),
      makeEvent({
        event: 'job_step_finished',
        state: 'failed',
        timestamp: '2026-04-09T10:00:03Z',
        step: { index: 1, total: 1, action: 'pull', target_stack_id: 'demo' },
      }),
    ]

    render(<StepCards events={events} />)

    expect(screen.getByText('Failed')).toBeInTheDocument()
  })

  it.each(['failed', 'cancelled', 'timed_out'] as const)('closes an unfinished step on a %s job and preserves completed steps and job-level output', (state) => {
    const events: JobEvent[] = [
      makeEvent({ event: 'job_step_finished', state: 'running', timestamp: '2026-04-09T10:00:00Z',
        step: { index: 1, total: 2, action: 'pull', state: 'succeeded' } }),
      makeEvent({ event: 'job_step_started', state: 'running', timestamp: '2026-04-09T10:00:01Z',
        step: { index: 2, total: 2, action: 'up', state: 'running' } }),
      makeEvent({ event: 'job_error', state, timestamp: '2026-04-09T10:00:03Z', message: 'Deployment stopped.', data: 'Detailed reason.' }),
      makeEvent({ event: 'job_finished', state, timestamp: '2026-04-09T10:00:03Z' }),
    ]
    render(<StepCards events={events} />)
    expect(screen.getByText('Done')).toBeInTheDocument()
    expect(screen.queryByText('Running')).not.toBeInTheDocument()
    expect(screen.getByText('Deployment stopped.')).toBeInTheDocument()
    expect(screen.getByText('Detailed reason.')).toBeInTheDocument()
    expect(screen.getByText('2s')).toBeInTheDocument()
    act(() => { vi.advanceTimersByTime(60_000) })
    expect(screen.getByText('2s')).toBeInTheDocument()
  })

  it('uses a terminal snapshot even when the event response has not caught up', () => {
    const events = [makeEvent({ event: 'job_step_started', state: 'running', timestamp: '2026-04-09T10:00:00Z',
      step: { index: 1, total: 2, action: 'create_stack', state: 'running' } })]
    render(<StepCards events={events} job={{ state: 'failed', finished_at: '2026-04-09T10:00:00Z',
      workflow: { steps: [{ action: 'create_stack', state: 'failed' }, { action: 'up', state: 'skipped' }] },
    }} />)
    expect(screen.getByText('Failed')).toBeInTheDocument()
    expect(screen.getByText('Skipped')).toBeInTheDocument()
    expect(screen.getByText('0s')).toBeInTheDocument()
    expect(screen.queryByText('Running')).not.toBeInTheDocument()
  })

  it('expands collapsed output on demand', () => {
    const events: JobEvent[] = [
      makeEvent({
        event: 'job_step_started',
        state: 'running',
        message: 'Starting pull for demo.',
        timestamp: '2026-04-09T10:00:00Z',
        step: { index: 1, total: 1, action: 'pull', target_stack_id: 'demo' },
      }),
      makeEvent({
        event: 'job_log',
        state: 'running',
        message: 'line one',
        timestamp: '2026-04-09T10:00:01Z',
        step: { index: 1, total: 1, action: 'pull', target_stack_id: 'demo' },
      }),
      makeEvent({
        event: 'job_warning',
        state: 'running',
        message: 'line two',
        timestamp: '2026-04-09T10:00:02Z',
        step: { index: 1, total: 1, action: 'pull', target_stack_id: 'demo' },
      }),
      makeEvent({
        event: 'job_error',
        state: 'failed',
        message: 'line three',
        timestamp: '2026-04-09T10:00:03Z',
        step: { index: 1, total: 1, action: 'pull', target_stack_id: 'demo' },
      }),
    ]

    render(<StepCards events={events} />)

    const expand = screen.getByRole('button', { name: 'Show all (3 lines)' })
    expect(expand).toHaveAttribute('aria-expanded', 'false')
    expect(expand).toHaveAttribute('aria-controls')
    fireEvent.click(expand)
    expect(screen.getByRole('button', { name: 'Collapse' })).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('line three')).toBeInTheDocument()
  })
})
