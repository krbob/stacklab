import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { DiffView } from './diff-view'

describe('DiffView', () => {
  it('renders added lines with green color class', () => {
    render(<DiffView diff="+added line" />)
    const line = screen.getByText('+added line')
    expect(line.closest('div')).toHaveClass('text-emerald-400')
  })

  it('renders removed lines with red color class', () => {
    render(<DiffView diff="-removed line" />)
    const line = screen.getByText('-removed line')
    expect(line.closest('div')).toHaveClass('text-red-400')
  })

  it('renders hunk headers with cyan color class', () => {
    render(<DiffView diff="@@ -1,3 +1,4 @@" />)
    const line = screen.getByText('@@ -1,3 +1,4 @@')
    expect(line.closest('div')).toHaveClass('text-cyan-400')
  })

  it('renders context lines with muted color', () => {
    render(<DiffView diff=" unchanged line" />)
    const line = screen.getByText('unchanged line')
    expect(line.closest('div')).toHaveClass('text-[var(--muted)]')
  })

  it('does not color --- and +++ header lines as additions/deletions', () => {
    render(<DiffView diff={"--- a/file.txt\n+++ b/file.txt"} />)
    const minusLine = screen.getByText('--- a/file.txt')
    const plusLine = screen.getByText('+++ b/file.txt')
    expect(minusLine.closest('div')).toHaveClass('text-[var(--muted)]')
    expect(plusLine.closest('div')).toHaveClass('text-[var(--muted)]')
  })

  it('shows truncation notice when truncated', () => {
    render(<DiffView diff="+line" truncated />)
    expect(screen.getByText(/Diff truncated/)).toBeInTheDocument()
  })

  it('does not show truncation notice by default', () => {
    render(<DiffView diff="+line" />)
    expect(screen.queryByText(/Diff truncated/)).not.toBeInTheDocument()
  })
})
