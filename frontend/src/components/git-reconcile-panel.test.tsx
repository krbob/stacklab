import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { GitReconcilePanel } from './git-reconcile-panel'
import type { GitIncomingResponse, GitReconcileResponse, GitReconcileFileResponse } from '@/lib/api-types'
import * as api from '@/lib/api-client'

vi.mock('@/lib/api-client', () => ({ getGitReconcile: vi.fn(), prepareGitReconcile: vi.fn(), getGitReconcileFile: vi.fn(), resolveGitReconcile: vi.fn(), applyGitReconcile: vi.fn(), discardGitReconcile: vi.fn() }))
vi.mock('@/components/unsaved-changes-guard', () => ({ UnsavedChangesGuard: () => null }))
const incoming: GitIncomingResponse = { available: true, branch: 'main', upstream_name: 'origin/main', head_commit: 'a'.repeat(40), target_commit: 'b'.repeat(40), ahead_count: 1, behind_count: 1, can_pull: false, can_reconcile: true, expected_state: 'review', checked_at: '2026-11-21T12:00:00Z', files: [] }
const draft: GitReconcileResponse = { id: 'draft', revision: 1, expected_state: 'review', branch: 'main', local_commit: incoming.head_commit, remote_commit: incoming.target_commit, created_at: incoming.checked_at, files: [{ path: 'config/demo/app.conf', status: 'U', stack_id: 'demo', managed: true }], conflicts: [{ path: 'config/demo/app.conf', resolved: false }], ready: false, applied: false }
const version = (text: string) => ({ exists: true, mode: '100644', size: text.length, text })
const file: GitReconcileFileResponse = { path: draft.files[0].path, base: version('base\n'), local: version('local\n'), remote: version('remote\n'), editable: true, choice: '', content: '', diff: '', truncated: false }
const resolved: GitReconcileResponse = { ...draft, revision: 2, ready: true, conflicts: [{ ...draft.conflicts[0], resolved: true }], files: [{ ...draft.files[0], status: 'M' }] }
const receipt = { branch: 'main', base_commit: incoming.head_commit, head_commit: 'merge', downloaded_at: incoming.checked_at, files: resolved.files }
const onApplied = vi.fn().mockResolvedValue(undefined)
function page() { return render(<MemoryRouter><GitReconcilePanel incoming={incoming} disabled={false} onApplied={onApplied} /></MemoryRouter>) }

describe('reconciliation UI', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.getGitReconcile).mockResolvedValue({ draft })
    vi.mocked(api.prepareGitReconcile).mockResolvedValue(draft)
    vi.mocked(api.getGitReconcileFile).mockResolvedValue(file)
    vi.mocked(api.resolveGitReconcile).mockResolvedValue(resolved)
    vi.mocked(api.applyGitReconcile).mockResolvedValue({ updated: true, receipt })
    vi.mocked(api.discardGitReconcile).mockResolvedValue({ discarded: true })
    onApplied.mockResolvedValue(undefined)
  })

  it('resumes a draft, saves an edited result privately, then applies only after review', async () => {
    page()
    fireEvent.click(await screen.findByRole('button', { name: /config\/demo\/app.conf/ }))
    const editor = await screen.findByRole('textbox', { name: 'Edited merge result' })
    expect(screen.getByText('base')).toBeInTheDocument()
    expect(screen.getByText('remote')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Review and apply merge' })).toBeDisabled()
    fireEvent.change(editor, { target: { value: 'combined\n' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save edited result' }))
    await waitFor(() => expect(api.resolveGitReconcile).toHaveBeenCalledWith({ id: 'draft', revision: 1, path: file.path, choice: 'edit', content: 'combined\n' }))
    expect(api.applyGitReconcile).not.toHaveBeenCalled()
    const apply = await screen.findByRole('button', { name: 'Review and apply merge' })
    await waitFor(() => expect(apply).toBeEnabled())
    fireEvent.click(apply)
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveTextContent('Mounted configuration may react immediately')
    expect(dialog).toHaveTextContent(file.path)
    fireEvent.click(within(dialog).getByRole('button', { name: 'Apply merged configuration' }))
    await waitFor(() => expect(onApplied).toHaveBeenCalledWith(receipt))
    expect(api.applyGitReconcile).toHaveBeenCalledWith({ id: 'draft', revision: 2 })
    expect(screen.getByText(/This merge was applied/)).toBeInTheDocument()
  })

  it('requires explicit discard and retains an error after a stale approval', async () => {
    vi.mocked(api.getGitReconcile).mockResolvedValue({ draft: resolved })
    vi.mocked(api.applyGitReconcile).mockRejectedValue(new Error('This draft is stale.'))
    page()
    fireEvent.click(await screen.findByRole('button', { name: 'Review and apply merge' }))
    fireEvent.click(screen.getByRole('button', { name: 'Apply merged configuration' }))
    await waitFor(() => expect(screen.getByRole('dialog')).toHaveTextContent('This draft is stale.'))
    expect(onApplied).not.toHaveBeenCalled()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))
    fireEvent.click(screen.getByRole('button', { name: 'Discard draft' }))
    expect(api.discardGitReconcile).not.toHaveBeenCalled()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Discard draft' }))
    await waitFor(() => expect(api.discardGitReconcile).toHaveBeenCalledWith({ id: 'draft', revision: 2 }))
    expect(await screen.findByRole('button', { name: 'Prepare reconciliation' })).toBeEnabled()
  })

  it('protects unsaved resolution text before switching to a side', async () => {
    page()
    fireEvent.click(await screen.findByRole('button', { name: /config\/demo\/app.conf/ }))
    fireEvent.change(await screen.findByRole('textbox', { name: 'Edited merge result' }), { target: { value: 'unsaved' } })
    fireEvent.click(screen.getByRole('button', { name: 'Use remote version' }))
    expect(screen.getByRole('dialog')).toHaveTextContent('Discard unsaved resolution?')
    expect(api.resolveGitReconcile).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.getByRole('textbox')).toHaveValue('unsaved')
  })

  it('discards a late file result when the draft is reloaded', async () => {
    let resolve!: (value: GitReconcileFileResponse) => void
    vi.mocked(api.getGitReconcileFile).mockReturnValue(new Promise((done) => { resolve = done }))
    page()
    fireEvent.click(await screen.findByRole('button', { name: /config\/demo\/app.conf/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Reload draft' }))
    await waitFor(() => expect(api.getGitReconcile).toHaveBeenCalledTimes(2))
    await act(async () => resolve(file))
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })
})
