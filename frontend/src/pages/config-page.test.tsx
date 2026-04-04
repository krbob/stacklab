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

vi.mock('@/lib/api-client', () => ({
  getConfigTree: (...args: unknown[]) => mockGetConfigTree(...args),
  getConfigFile: (...args: unknown[]) => mockGetConfigFile(...args),
  saveConfigFile: (...args: unknown[]) => mockSaveConfigFile(...args),
  getGitWorkspaceStatus: (...args: unknown[]) => mockGetGitWorkspaceStatus(...args),
  getGitWorkspaceDiff: (...args: unknown[]) => mockGetGitWorkspaceDiff(...args),
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

    mockGetConfigTree.mockResolvedValue(rootTree)
    mockGetGitWorkspaceStatus.mockResolvedValue(gitStatus)
    mockGetGitWorkspaceDiff.mockResolvedValue(gitDiff)
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
})
