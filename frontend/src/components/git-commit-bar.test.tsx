import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { GitCommitBar } from './git-commit-bar'

const mockCommitGitWorkspace = vi.fn()
const mockPushGitWorkspace = vi.fn()

vi.mock('@/lib/api-client', () => ({
  commitGitWorkspace: (...args: unknown[]) => mockCommitGitWorkspace(...args),
  pushGitWorkspace: (...args: unknown[]) => mockPushGitWorkspace(...args),
}))

describe('GitCommitBar', () => {
  const defaultProps = {
    selectedPaths: new Set(['config/demo/app.conf']),
    hasUpstream: true,
    aheadCount: 1,
    onCommitted: vi.fn(),
    onPushed: vi.fn(),
  }

  beforeEach(() => {
    mockCommitGitWorkspace.mockReset()
    mockPushGitWorkspace.mockReset()
    defaultProps.onCommitted = vi.fn()
    defaultProps.onPushed = vi.fn()
  })

  it('shows selected file count', () => {
    render(<GitCommitBar {...defaultProps} />)
    expect(screen.getByText('1 file selected')).toBeInTheDocument()
  })

  it('shows plural for multiple files', () => {
    render(<GitCommitBar {...defaultProps} selectedPaths={new Set(['a', 'b'])} />)
    expect(screen.getByText('2 files selected')).toBeInTheDocument()
  })

  it('disables commit button when no files selected', () => {
    render(<GitCommitBar {...defaultProps} selectedPaths={new Set()} />)
    expect(screen.getByRole('button', { name: 'Commit' })).toBeDisabled()
  })

  it('commits with message and selected paths', async () => {
    mockCommitGitWorkspace.mockResolvedValue({
      committed: true,
      commit: 'abc12345',
      summary: 'Update config',
      paths: ['config/demo/app.conf'],
      remaining_changes: 0,
    })

    render(<GitCommitBar {...defaultProps} />)

    fireEvent.click(screen.getByRole('button', { name: 'Commit' }))
    fireEvent.change(screen.getByTestId('git-commit-message'), { target: { value: 'Update config' } })
    fireEvent.click(screen.getByTestId('git-commit-submit'))

    await waitFor(() => {
      expect(mockCommitGitWorkspace).toHaveBeenCalledWith({
        message: 'Update config',
        paths: ['config/demo/app.conf'],
      })
    })
    expect(await screen.findByText(/Committed abc12345/)).toBeInTheDocument()
    expect(defaultProps.onCommitted).toHaveBeenCalled()
  })

  it('shows error on commit failure', async () => {
    mockCommitGitWorkspace.mockRejectedValue(new Error('Nothing to commit'))

    render(<GitCommitBar {...defaultProps} />)

    fireEvent.click(screen.getByRole('button', { name: 'Commit' }))
    fireEvent.change(screen.getByTestId('git-commit-message'), { target: { value: 'test' } })
    fireEvent.click(screen.getByTestId('git-commit-submit'))

    expect(await screen.findByText('Nothing to commit')).toBeInTheDocument()
  })

  it('shows push button when upstream exists and ahead > 0', () => {
    render(<GitCommitBar {...defaultProps} />)
    expect(screen.getByTestId('git-push')).toBeInTheDocument()
    expect(screen.getByText('Push (1 ahead)')).toBeInTheDocument()
  })

  it('hides push button when no upstream', () => {
    render(<GitCommitBar {...defaultProps} hasUpstream={false} />)
    expect(screen.queryByTestId('git-push')).not.toBeInTheDocument()
  })

  it('hides push button when ahead is 0', () => {
    render(<GitCommitBar {...defaultProps} aheadCount={0} />)
    expect(screen.queryByTestId('git-push')).not.toBeInTheDocument()
  })

  it('pushes and calls onPushed', async () => {
    mockPushGitWorkspace.mockResolvedValue({
      pushed: true,
      remote: 'origin',
      branch: 'main',
      upstream_name: 'origin/main',
      head_commit: 'abc',
      ahead_count: 0,
      behind_count: 0,
    })

    render(<GitCommitBar {...defaultProps} />)
    fireEvent.click(screen.getByTestId('git-push'))

    await waitFor(() => {
      expect(mockPushGitWorkspace).toHaveBeenCalled()
    })
    expect(await screen.findByText(/Pushed to origin\/main/)).toBeInTheDocument()
    expect(defaultProps.onPushed).toHaveBeenCalled()
  })

  it('shows push error', async () => {
    mockPushGitWorkspace.mockRejectedValue(new Error('Auth failed'))

    render(<GitCommitBar {...defaultProps} />)
    fireEvent.click(screen.getByTestId('git-push'))

    expect(await screen.findByText('Auth failed')).toBeInTheDocument()
  })
})
