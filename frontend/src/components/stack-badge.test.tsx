import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { StackBadge } from './stack-badge'

describe('StackBadge', () => {
  it('shows Running label for running state', () => {
    render(<StackBadge displayState="running" configState="in_sync" activityState="idle" />)
    expect(screen.getByText('Running')).toBeInTheDocument()
  })

  it('shows Stopped label for stopped state', () => {
    render(<StackBadge displayState="stopped" configState="in_sync" activityState="idle" />)
    expect(screen.getByText('Stopped')).toBeInTheDocument()
  })

  it('shows Defined label for defined state', () => {
    render(<StackBadge displayState="defined" configState="unknown" activityState="idle" />)
    expect(screen.getByText('Defined')).toBeInTheDocument()
  })

  it('shows Orphaned label for orphaned state', () => {
    render(<StackBadge displayState="orphaned" configState="unknown" activityState="idle" />)
    expect(screen.getByText('Orphaned')).toBeInTheDocument()
  })

  it('shows drifted config indicator', () => {
    render(<StackBadge displayState="running" configState="drifted" activityState="idle" />)
    expect(screen.getByText('Running')).toBeInTheDocument()
    expect(screen.getByText(/Drifted/)).toBeInTheDocument()
  })

  it('shows invalid config indicator', () => {
    render(<StackBadge displayState="stopped" configState="invalid" activityState="idle" />)
    expect(screen.getByText(/Invalid/)).toBeInTheDocument()
  })

  it('does not show config indicator for in_sync', () => {
    render(<StackBadge displayState="running" configState="in_sync" activityState="idle" />)
    expect(screen.queryByText(/Drifted/)).not.toBeInTheDocument()
    expect(screen.queryByText(/Invalid/)).not.toBeInTheDocument()
  })

  it('does not show config indicator for unknown', () => {
    render(<StackBadge displayState="defined" configState="unknown" activityState="idle" />)
    expect(screen.queryByText(/Drifted/)).not.toBeInTheDocument()
    expect(screen.queryByText(/Invalid/)).not.toBeInTheDocument()
  })

  it('shows locked overlay for locked activity state', () => {
    render(<StackBadge displayState="running" configState="in_sync" activityState="locked" />)
    expect(screen.getByText('Running')).toBeInTheDocument()
    expect(screen.getByText(/Locked/)).toBeInTheDocument()
  })

  it('does not show locked for idle activity state', () => {
    render(<StackBadge displayState="running" configState="in_sync" activityState="idle" />)
    expect(screen.queryByText(/Locked/)).not.toBeInTheDocument()
  })

  it('shows all indicators together', () => {
    render(<StackBadge displayState="partial" configState="drifted" activityState="locked" />)
    expect(screen.getByText('Partial')).toBeInTheDocument()
    expect(screen.getByText(/Drifted/)).toBeInTheDocument()
    expect(screen.getByText(/Locked/)).toBeInTheDocument()
  })
})
