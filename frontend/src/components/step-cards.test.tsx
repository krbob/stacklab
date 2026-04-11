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

    expect(screen.getByRole('button', { name: 'Show all (3 lines)' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Show all (3 lines)' }))
    expect(screen.getByRole('button', { name: 'Collapse' })).toBeInTheDocument()
    expect(screen.getByText('line three')).toBeInTheDocument()
  })
})
