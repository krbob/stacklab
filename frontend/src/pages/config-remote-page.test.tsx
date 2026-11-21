import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { StrictMode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ConfigRemotePage } from './config-remote-page'
import type { GitIncomingDiffResponse, GitIncomingResponse, GitPullReceipt } from '@/lib/api-types'
import * as api from '@/lib/api-client'

vi.mock('@/lib/api-client', () => ({ checkGitIncoming: vi.fn(), getGitIncomingDiff: vi.fn(), getStacks: vi.fn(), invokeAction: vi.fn(), pullGitWorkspace: vi.fn() }))
vi.mock('@/hooks/use-job-stream', () => ({ useJobStream: () => ({ state: 'running', events: [] }) }))
vi.mock('@/components/progress-panel', () => ({ ProgressPanel: ({ jobId }: { jobId: string }) => <div>Job progress: {jobId}</div> }))

const incoming: GitIncomingResponse = {
  available: true, branch: 'main', upstream_name: 'origin/main', head_commit: 'a'.repeat(40), target_commit: 'b'.repeat(40),
  ahead_count: 0, behind_count: 1, checked_at: '2026-11-21T12:00:00Z', can_pull: true, expected_state: 'review-token',
  files: [{ path: 'config/demo/app.conf', status: 'M', stack_id: 'demo', managed: true }, { path: 'README.md', status: 'M', stack_id: null, managed: false }],
}
const receipt: GitPullReceipt = { branch: 'main', base_commit: incoming.head_commit, head_commit: incoming.target_commit, downloaded_at: incoming.checked_at, files: incoming.files }
const current = { ...incoming, behind_count: 0, can_pull: false, blocked_reason: 'up_to_date', files: [], last_pull: receipt }

function page() { return render(<MemoryRouter><ConfigRemotePage /></MemoryRouter>) }

describe('Remote configuration updates', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.checkGitIncoming).mockResolvedValue(incoming)
    vi.mocked(api.getGitIncomingDiff).mockResolvedValue({ path: incoming.files[0].path, diff: '-old\n+new', truncated: false })
    vi.mocked(api.getStacks).mockResolvedValue({ items: [{ id: 'demo', runtime_state: 'running', config_state: 'in_sync', activity_state: 'idle' }] } as Awaited<ReturnType<typeof api.getStacks>>)
    vi.mocked(api.pullGitWorkspace).mockResolvedValue({ updated: true, receipt })
    vi.mocked(api.invokeAction).mockResolvedValue({ job: { id: 'job-reload' } } as Awaited<ReturnType<typeof api.invokeAction>>)
  })

  it('coalesces the initial remote fetch during StrictMode effect replay', async () => {
    render(<StrictMode><MemoryRouter><ConfigRemotePage /></MemoryRouter></StrictMode>)
    await screen.findByRole('button', { name: 'Download reviewed changes' })
    expect(api.checkGitIncoming).toHaveBeenCalledTimes(1)
  })

  it('reviews external files, downloads only after confirmation, then explicitly reloads a stack', async () => {
    page()
    fireEvent.click(await screen.findByRole('button', { name: /config\/demo\/app.conf/ }))
    expect(await screen.findByText('+new')).toBeInTheDocument()
    expect(api.getGitIncomingDiff).toHaveBeenCalledWith('config/demo/app.conf', 'review-token')
    expect(screen.getByText('Other repository file')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Download reviewed changes' }))
    expect(api.pullGitWorkspace).not.toHaveBeenCalled()
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('README.md')).toBeInTheDocument()
    vi.mocked(api.checkGitIncoming).mockResolvedValue(current)
    fireEvent.click(within(dialog).getByRole('button', { name: 'Download configuration' }))
    const reload = await screen.findByRole('button', { name: 'Reload configuration' })
    await waitFor(() => expect(reload).toBeEnabled())
    expect(api.pullGitWorkspace).toHaveBeenCalledWith('review-token')
    expect(api.invokeAction).not.toHaveBeenCalled()
    fireEvent.click(reload)
    expect(screen.getByRole('dialog')).toHaveTextContent('Running services will be interrupted')
    fireEvent.click(screen.getByRole('button', { name: 'Apply configuration' }))
    await waitFor(() => expect(api.invokeAction).toHaveBeenCalledWith('demo', 'recreate'))
    expect(await screen.findByText('Job progress: job-reload')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Reload configuration' })).toBeDisabled()
  })

  it.each(['local_changes', 'diverged'])('shows why %s blocks downloading', async (blocked) => {
    vi.mocked(api.checkGitIncoming).mockResolvedValue({ ...incoming, can_pull: false, blocked_reason: blocked })
    page()
    expect(await screen.findByRole('button', { name: 'Download reviewed changes' })).toBeDisabled()
    expect(api.pullGitWorkspace).not.toHaveBeenCalled()
  })

  it('keeps the review and error after a rejected download without starting containers', async () => {
    vi.mocked(api.pullGitWorkspace).mockRejectedValue(new Error('The branch or reviewed commits changed.'))
    page()
    fireEvent.click(await screen.findByRole('button', { name: 'Download reviewed changes' }))
    fireEvent.click(screen.getByRole('button', { name: 'Download configuration' }))
    expect(await screen.findByText('The branch or reviewed commits changed.')).toBeInTheDocument()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(api.invokeAction).not.toHaveBeenCalled()
  })

  it('disables a previously valid download when a new remote check fails', async () => {
    page()
    expect(await screen.findByRole('button', { name: 'Download reviewed changes' })).toBeEnabled()
    vi.mocked(api.checkGitIncoming).mockRejectedValue(new Error('Remote unavailable'))
    fireEvent.click(screen.getByRole('button', { name: 'Check remote' }))
    expect(await screen.findByText(/Remote unavailable/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Download reviewed changes' })).toBeDisabled()
  })

  it('restores download history on page entry and uses Deploy for a newly defined stack', async () => {
    vi.mocked(api.checkGitIncoming).mockResolvedValue(current)
    vi.mocked(api.getStacks).mockResolvedValue({ items: [{ id: 'demo', runtime_state: 'defined', config_state: 'unknown', activity_state: 'idle' }] } as Awaited<ReturnType<typeof api.getStacks>>)
    page()
    fireEvent.click(await screen.findByRole('button', { name: 'Deploy configuration' }))
    fireEvent.click(screen.getByRole('button', { name: 'Apply configuration' }))
    await waitFor(() => expect(api.invokeAction).toHaveBeenCalledWith('demo', 'up'))
    expect(api.pullGitWorkspace).not.toHaveBeenCalled()
  })

  it('discards a late diff response when the reviewed version is refreshed', async () => {
    let resolve!: (value: GitIncomingDiffResponse) => void
    vi.mocked(api.getGitIncomingDiff).mockReturnValue(new Promise((done) => { resolve = done }))
    page()
    fireEvent.click(await screen.findByRole('button', { name: /config\/demo\/app.conf/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Check remote' }))
    await waitFor(() => expect(api.checkGitIncoming).toHaveBeenCalledTimes(2))
    await act(async () => resolve({ path: 'config/demo/app.conf', diff: '+old remote version', truncated: false }))
    expect(screen.queryByText('+old remote version')).not.toBeInTheDocument()
  })
})
