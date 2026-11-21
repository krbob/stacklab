import { StrictMode } from 'react'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { checkGitIncoming } from '@/lib/api-client'
import { GitSyncBanner } from '@/components/git-sync-banner'
import { checkGitSync, clearGitSync } from './use-git-sync'
import type { GitIncomingResponse } from '@/lib/api-types'

vi.mock('@/lib/api-client', () => ({ checkGitIncoming: vi.fn() }))
const current: GitIncomingResponse = { available: true, branch: 'main', upstream_name: 'origin/main', head_commit: 'a', target_commit: 'a', ahead_count: 0, behind_count: 0, checked_at: '2026-11-21T12:00:00Z', can_pull: false, expected_state: 'current', files: [], local_changes: 0 }

describe('automatic Git checks', () => {
  beforeEach(() => { clearGitSync(); vi.mocked(checkGitIncoming).mockReset(); vi.mocked(checkGitIncoming).mockResolvedValue(current) })
  afterEach(() => { vi.restoreAllMocks() })

  it('coalesces StrictMode entry and checks again on focus only after the cooldown', async () => {
    let now = 1000
    vi.spyOn(Date, 'now').mockImplementation(() => now)
    render(<StrictMode><MemoryRouter><GitSyncBanner /></MemoryRouter></StrictMode>)
    await screen.findByText('No incoming commits at the last check.')
    expect(checkGitIncoming).toHaveBeenCalledTimes(1)
    fireEvent.focus(window)
    expect(checkGitIncoming).toHaveBeenCalledTimes(1)
    now += 60_001
    vi.mocked(checkGitIncoming).mockResolvedValue({ ...current, behind_count: 2, ahead_count: 1 })
    fireEvent.focus(window)
    await screen.findByText('2 incoming commits · 1 local commits — reconciliation needed')
    expect(checkGitIncoming).toHaveBeenCalledTimes(2)
    fireEvent(document, new Event('visibilitychange'))
    expect(checkGitIncoming).toHaveBeenCalledTimes(2)
  })

  it('marks old evidence unverified after a failed check and allows an explicit retry', async () => {
    render(<MemoryRouter><GitSyncBanner /></MemoryRouter>)
    await screen.findByText('No incoming commits at the last check.')
    vi.mocked(checkGitIncoming).mockRejectedValue(new Error('Network unavailable'))
    fireEvent.click(screen.getByRole('button', { name: 'Check remote' }))
    await screen.findByText('Remote not verified. Local editing is available.')
    expect(screen.getByText(/Last checked/)).toBeInTheDocument()
    vi.mocked(checkGitIncoming).mockResolvedValue({ ...current, behind_count: 1, local_changes: 2 })
    fireEvent.click(screen.getByRole('button', { name: 'Check remote' }))
    await screen.findByText(/1 incoming commit — review before editing/)
    expect(screen.getByText(/Preserve local edits with Commit/)).toBeInTheDocument()
  })

  it('does not restore a previous session cache after logout', async () => {
    let resolve!: (value: GitIncomingResponse) => void
    vi.mocked(checkGitIncoming).mockReturnValue(new Promise((done) => { resolve = done }))
    const pending = checkGitSync()
    await waitFor(() => expect(checkGitIncoming).toHaveBeenCalledOnce())
    clearGitSync()
    await act(async () => { resolve(current); expect(await pending).toBeNull() })
    vi.mocked(checkGitIncoming).mockResolvedValue({ ...current, branch: 'new-session' })
    expect((await checkGitSync(false))?.branch).toBe('new-session')
  })
})
