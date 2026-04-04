import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ConfigPage } from './config-page'
import type {
  ConfigFileResponse,
  ConfigTreeResponse,
  GitDiffResponse,
  GitWorkspaceStatusResponse,
} from '@/lib/api-types'

const mockGetConfigTree = vi.fn()
const mockGetConfigFile = vi.fn()
const mockSaveConfigFile = vi.fn()
const mockGetGitWorkspaceStatus = vi.fn()
const mockGetGitWorkspaceDiff = vi.fn()
const mockCommitGitWorkspace = vi.fn()
const mockPushGitWorkspace = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getConfigTree: (...args: unknown[]) => mockGetConfigTree(...args),
  getConfigFile: (...args: unknown[]) => mockGetConfigFile(...args),
  saveConfigFile: (...args: unknown[]) => mockSaveConfigFile(...args),
  getGitWorkspaceStatus: (...args: unknown[]) => mockGetGitWorkspaceStatus(...args),
  getGitWorkspaceDiff: (...args: unknown[]) => mockGetGitWorkspaceDiff(...args),
  commitGitWorkspace: (...args: unknown[]) => mockCommitGitWorkspace(...args),
  pushGitWorkspace: (...args: unknown[]) => mockPushGitWorkspace(...args),
}))

vi.mock('@/components/yaml-editor', () => ({
  YamlEditor: ({
    value,
    onChange,
    readOnly,
  }: {
    value: string
    onChange: (value: string) => void
    readOnly?: boolean
  }) => (
    <textarea
      aria-label="yaml-editor"
      readOnly={readOnly}
      value={value}
      onChange={(event) => onChange(event.target.value)}
    />
  ),
}))

const rootTree: ConfigTreeResponse = {
  workspace_root: '/opt/stacklab/config',
  current_path: '',
  parent_path: null,
  items: [
    {
      name: 'demo',
      path: 'demo',
      type: 'directory',
      size_bytes: 0,
      modified_at: '2026-04-04T12:00:00Z',
      stack_id: 'demo',
    },
  ],
}

const demoTree: ConfigTreeResponse = {
  workspace_root: '/opt/stacklab/config',
  current_path: 'demo',
  parent_path: '',
  items: [
    {
      name: 'app.conf',
      path: 'demo/app.conf',
      type: 'text_file',
      size_bytes: 20,
      modified_at: '2026-04-04T12:00:00Z',
      stack_id: 'demo',
    },
  ],
}

const fileBefore: ConfigFileResponse = {
  path: 'demo/app.conf',
  name: 'app.conf',
  type: 'text_file',
  stack_id: 'demo',
  content: 'server_name old.local;\n',
  encoding: 'utf-8',
  size_bytes: 24,
  modified_at: '2026-04-04T12:00:00Z',
  writable: true,
}

const fileAfter: ConfigFileResponse = {
  ...fileBefore,
  content: 'server_name new.local;\n',
  modified_at: '2026-04-04T12:01:00Z',
}

const gitStatus: GitWorkspaceStatusResponse = {
  available: true,
  repo_root: '/opt/stacklab',
  managed_roots: ['stacks', 'config'],
  branch: 'main',
  head_commit: 'abcdef1234567890',
  has_upstream: true,
  upstream_name: 'origin/main',
  ahead_count: 1,
  behind_count: 0,
  clean: false,
  items: [
    {
      path: 'config/demo/app.conf',
      scope: 'config',
      stack_id: 'demo',
      status: 'modified',
      old_path: null,
    },
  ],
}

const gitDiff: GitDiffResponse = {
  available: true,
  path: 'config/demo/app.conf',
  scope: 'config',
  stack_id: 'demo',
  status: 'modified',
  old_path: null,
  is_binary: false,
  diff: '@@ -1 +1 @@\n-server_name old.local;\n+server_name new.local;\n',
  truncated: false,
}

function renderPage() {
  return render(
    <MemoryRouter>
      <ConfigPage />
    </MemoryRouter>,
  )
}

describe('ConfigPage', () => {
  beforeEach(() => {
    mockGetConfigTree.mockReset()
    mockGetConfigFile.mockReset()
    mockSaveConfigFile.mockReset()
    mockGetGitWorkspaceStatus.mockReset()
    mockGetGitWorkspaceDiff.mockReset()
    mockCommitGitWorkspace.mockReset()
    mockPushGitWorkspace.mockReset()

    mockGetConfigTree.mockResolvedValue(rootTree)
    mockGetGitWorkspaceStatus.mockResolvedValue(gitStatus)
    mockGetGitWorkspaceDiff.mockResolvedValue(gitDiff)
    mockCommitGitWorkspace.mockResolvedValue({
      committed: true,
      commit: 'abc12345',
      summary: 'Update demo config',
      paths: ['config/demo/app.conf'],
      remaining_changes: 0,
    })
    mockPushGitWorkspace.mockResolvedValue({
      pushed: true,
      remote: 'origin',
      branch: 'main',
      upstream_name: 'origin/main',
      head_commit: 'abc12345',
      ahead_count: 0,
      behind_count: 0,
    })
  })

  it('loads, edits, and saves a config file in Files mode', async () => {
    mockGetConfigTree
      .mockResolvedValueOnce(rootTree)
      .mockResolvedValueOnce(demoTree)
    mockGetConfigFile
      .mockResolvedValueOnce(fileBefore)
      .mockResolvedValueOnce(fileAfter)
    mockSaveConfigFile.mockResolvedValue({
      saved: true,
      path: 'demo/app.conf',
      modified_at: '2026-04-04T12:01:00Z',
      audit_action: 'save_config_file',
    })

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'demo' }))
    fireEvent.click(await screen.findByRole('button', { name: 'app.conf' }))

    const editor = await screen.findByLabelText('yaml-editor')
    fireEvent.change(editor, { target: { value: 'server_name new.local;\n' } })
    fireEvent.click(screen.getByTestId('config-save'))

    await waitFor(() => {
      expect(mockSaveConfigFile).toHaveBeenCalledWith('demo/app.conf', 'server_name new.local;\n')
    })
    expect(await screen.findByText('Saved')).toBeInTheDocument()
    expect(mockGetConfigFile).toHaveBeenLastCalledWith('demo/app.conf')
  })

  it('renders Changes mode and opens a diff, then switches back to editor', async () => {
    mockGetConfigFile.mockResolvedValue(fileBefore)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))

    expect(await screen.findByText('main')).toBeInTheDocument()
    expect(screen.getByText('+1')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /app\.conf$/ }))

    expect(await screen.findByText('modified')).toBeInTheDocument()
    expect(screen.getByText('-server_name old.local;')).toBeInTheDocument()
    expect(screen.getByText('+server_name new.local;')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Open in editor' }))

    await waitFor(() => {
      expect(mockGetConfigFile).toHaveBeenCalledWith('demo/app.conf')
    })
    expect(await screen.findByLabelText('yaml-editor')).toBeInTheDocument()
  })

  it('shows "Not a Git repository" when Git is unavailable', async () => {
    mockGetGitWorkspaceStatus.mockResolvedValue({
      available: false,
      repo_root: '/opt/stacklab',
      managed_roots: ['stacks', 'config'],
      reason: 'not_a_git_repository',
    })

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))

    expect(await screen.findByText(/Not a Git repository/)).toBeInTheDocument()
  })

  it('disables Changes button when Git is unavailable', async () => {
    mockGetGitWorkspaceStatus.mockResolvedValue({
      available: false,
      repo_root: '/opt/stacklab',
      managed_roots: ['stacks', 'config'],
      reason: 'not_a_git_repository',
    })

    renderPage()

    // Wait for initial load and git status check
    await screen.findByRole('button', { name: 'demo' })

    // Switch to changes to trigger the status fetch, then back
    fireEvent.click(screen.getByRole('button', { name: /^Changes/ }))
    await screen.findByText(/Not a Git repository/)

    // Button should now be disabled since git is unavailable
    fireEvent.click(screen.getByRole('button', { name: 'Files' }))
    const changesBtn = screen.getByRole('button', { name: /^Changes/ })
    expect(changesBtn).toBeDisabled()
  })

  it('shows read-only card for binary files', async () => {
    mockGetConfigTree
      .mockResolvedValueOnce(rootTree)
      .mockResolvedValueOnce({
        ...demoTree,
        items: [
          {
            name: 'cert.p12',
            path: 'demo/cert.p12',
            type: 'binary_file',
            size_bytes: 4096,
            modified_at: '2026-04-04T12:00:00Z',
            stack_id: 'demo',
          },
        ],
      })
    mockGetConfigFile.mockResolvedValue({
      path: 'demo/cert.p12',
      name: 'cert.p12',
      type: 'binary_file',
      stack_id: 'demo',
      content: null,
      encoding: null,
      size_bytes: 4096,
      modified_at: '2026-04-04T12:00:00Z',
      writable: false,
    })

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'demo' }))
    fireEvent.click(await screen.findByRole('button', { name: 'cert.p12' }))

    expect(await screen.findByText('Binary file')).toBeInTheDocument()
    expect(screen.getByText(/cannot be edited/)).toBeInTheDocument()
    expect(screen.queryByLabelText('yaml-editor')).not.toBeInTheDocument()
  })

  it('commits selected files and clears stale diff when the file is no longer changed', async () => {
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))

    const diffButton = await screen.findByRole('button', { name: /app\.conf$/ })
    const checkboxes = screen.getAllByRole('checkbox')
    fireEvent.click(checkboxes[1])
    fireEvent.click(diffButton)

    expect(await screen.findByText('modified')).toBeInTheDocument()

    mockGetGitWorkspaceStatus.mockResolvedValue({
      ...gitStatus,
      ahead_count: 2,
      items: [],
      clean: true,
    })

    fireEvent.click(screen.getByRole('button', { name: 'Commit' }))
    fireEvent.change(screen.getByTestId('git-commit-message'), { target: { value: 'Update demo config' } })
    fireEvent.click(screen.getByTestId('git-commit-submit'))

    await waitFor(() => {
      expect(mockCommitGitWorkspace).toHaveBeenCalledWith({
        message: 'Update demo config',
        paths: ['config/demo/app.conf'],
      })
    })

    expect(await screen.findByText('No local changes detected.')).toBeInTheDocument()
    expect(screen.queryByText('modified')).not.toBeInTheDocument()
  })

  it('pushes ahead commits and refreshes Git status', async () => {
    mockGetGitWorkspaceStatus
      .mockResolvedValueOnce(gitStatus)
      .mockResolvedValueOnce({
        ...gitStatus,
        ahead_count: 0,
      })

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))
    fireEvent.click(await screen.findByTestId('git-push'))

    await waitFor(() => {
      expect(mockPushGitWorkspace).toHaveBeenCalled()
    })
    expect(await screen.findByText('main')).toBeInTheDocument()
    expect(screen.queryByTestId('git-push')).not.toBeInTheDocument()
  })
})
