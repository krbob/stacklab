import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
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
const mockDeleteConfigFile = vi.fn()
const mockDeleteGitWorkspaceFile = vi.fn()
const mockGetGitWorkspaceStatus = vi.fn()
const mockGetGitWorkspaceDiff = vi.fn()
const mockCommitGitWorkspace = vi.fn()
const mockPushGitWorkspace = vi.fn()
const mockRepairConfigWorkspacePermissions = vi.fn()
const mockRepairStackWorkspacePermissions = vi.fn()

const unsupportedRepairCapability = {
  supported: false,
  reason: 'Workspace permission repair is not configured yet.',
  recursive: true,
}

vi.mock('@/lib/api-client', () => ({
  getConfigTree: (...args: unknown[]) => mockGetConfigTree(...args),
  getConfigFile: (...args: unknown[]) => mockGetConfigFile(...args),
  saveConfigFile: (...args: unknown[]) => mockSaveConfigFile(...args),
  deleteConfigFile: (...args: unknown[]) => mockDeleteConfigFile(...args),
  deleteGitWorkspaceFile: (...args: unknown[]) => mockDeleteGitWorkspaceFile(...args),
  getGitWorkspaceStatus: (...args: unknown[]) => mockGetGitWorkspaceStatus(...args),
  getGitWorkspaceDiff: (...args: unknown[]) => mockGetGitWorkspaceDiff(...args),
  commitGitWorkspace: (...args: unknown[]) => mockCommitGitWorkspace(...args),
  pushGitWorkspace: (...args: unknown[]) => mockPushGitWorkspace(...args),
  repairConfigWorkspacePermissions: (...args: unknown[]) => mockRepairConfigWorkspacePermissions(...args),
  repairStackWorkspacePermissions: (...args: unknown[]) => mockRepairStackWorkspacePermissions(...args),
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
      git_ignored: false,
      permissions: {
        owner_uid: 1000,
        owner_name: 'bob',
        group_gid: 1000,
        group_name: 'bob',
        mode: '0755',
        readable: true,
        writable: true,
      },
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
      git_ignored: false,
      permissions: {
        owner_uid: 1000,
        owner_name: 'bob',
        group_gid: 1000,
        group_name: 'bob',
        mode: '0644',
        readable: true,
        writable: true,
      },
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
  git_ignored: false,
  readable: true,
  writable: true,
  blocked_reason: null,
  permissions: {
    owner_uid: 1000,
    owner_name: 'bob',
    group_gid: 1000,
    group_name: 'bob',
    mode: '0644',
    readable: true,
    writable: true,
  },
  repair_capability: unsupportedRepairCapability,
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
      permissions: {
        owner_uid: 1000,
        owner_name: 'bob',
        group_gid: 1000,
        group_name: 'bob',
        mode: '0644',
        readable: true,
        writable: true,
      },
      diff_available: true,
      commit_allowed: true,
      blocked_reason: null,
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
  permissions: {
    owner_uid: 1000,
    owner_name: 'bob',
    group_gid: 1000,
    group_name: 'bob',
    mode: '0644',
    readable: true,
    writable: true,
  },
  diff_available: true,
  blocked_reason: null,
  is_binary: false,
  diff: '@@ -1 +1 @@\n-server_name old.local;\n+server_name new.local;\n',
  truncated: false,
}

const blockedFile: ConfigFileResponse = {
  path: 'demo/secret.conf',
  name: 'secret.conf',
  type: 'unknown_file',
  stack_id: 'demo',
  content: null,
  encoding: null,
  size_bytes: 12,
  modified_at: '2026-04-04T12:00:00Z',
  git_ignored: false,
  readable: false,
  writable: false,
  blocked_reason: 'not_readable',
  permissions: {
    owner_uid: 0,
    owner_name: 'root',
    group_gid: 0,
    group_name: 'root',
    mode: '0600',
    readable: false,
    writable: false,
  },
  repair_capability: unsupportedRepairCapability,
}

const blockedGitStatus: GitWorkspaceStatusResponse = {
  ...gitStatus,
  items: [
    {
      path: 'config/demo/app.conf',
      scope: 'config',
      stack_id: 'demo',
      status: 'modified',
      old_path: null,
      permissions: {
        owner_uid: 1000,
        owner_name: 'bob',
        group_gid: 1000,
        group_name: 'bob',
        mode: '0644',
        readable: true,
        writable: true,
      },
      diff_available: true,
      commit_allowed: true,
      blocked_reason: null,
    },
    {
      path: 'config/demo/secret.conf',
      scope: 'config',
      stack_id: 'demo',
      status: 'modified',
      old_path: null,
      permissions: {
        owner_uid: 0,
        owner_name: 'root',
        group_gid: 0,
        group_name: 'root',
        mode: '0600',
        readable: false,
        writable: false,
      },
      diff_available: false,
      commit_allowed: false,
      blocked_reason: 'not_readable',
    },
  ],
}

const blockedGitDiff: GitDiffResponse = {
  available: true,
  path: 'config/demo/secret.conf',
  scope: 'config',
  stack_id: 'demo',
  status: 'modified',
  old_path: null,
  permissions: {
    owner_uid: 0,
    owner_name: 'root',
    group_gid: 0,
    group_name: 'root',
    mode: '0600',
    readable: false,
    writable: false,
  },
  diff_available: false,
  blocked_reason: 'not_readable',
  is_binary: false,
  diff: null,
  truncated: false,
}

function renderPage(initialEntry = '/config') {
  const router = createMemoryRouter(
    [{ path: '*', element: <ConfigPage /> }],
    { initialEntries: [initialEntry] },
  )
  return { router, ...render(<RouterProvider router={router} />) }
}

describe('ConfigPage', () => {
  beforeEach(() => {
    mockGetConfigTree.mockReset()
    mockGetConfigFile.mockReset()
    mockSaveConfigFile.mockReset()
    mockDeleteConfigFile.mockReset()
    mockDeleteGitWorkspaceFile.mockReset()
    mockGetGitWorkspaceStatus.mockReset()
    mockGetGitWorkspaceDiff.mockReset()
    mockCommitGitWorkspace.mockReset()
    mockPushGitWorkspace.mockReset()
    mockRepairConfigWorkspacePermissions.mockReset()
    mockRepairStackWorkspacePermissions.mockReset()

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

    expect(screen.getByRole('heading', { level: 1, name: 'Config' })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)

    fireEvent.click(await screen.findByRole('button', { name: 'demo' }))
    fireEvent.click(await screen.findByRole('button', { name: 'app.conf' }))

    const editor = await screen.findByLabelText('yaml-editor')
    fireEvent.change(editor, { target: { value: 'server_name new.local;\n' } })
    fireEvent.click(screen.getByTestId('config-save'))

    await waitFor(() => {
      expect(mockSaveConfigFile).toHaveBeenCalledWith('demo/app.conf', 'server_name new.local;\n', false, '2026-04-04T12:00:00Z')
    })
    expect(await screen.findByRole('status')).toHaveTextContent('Saved')
    expect(mockGetConfigFile).toHaveBeenLastCalledWith('demo/app.conf')
  })

  it('reviews deletion without losing unsaved changes on cancel, then refreshes Files and Git on success', async () => {
    mockGetConfigTree.mockResolvedValue(demoTree)
    mockGetConfigFile.mockResolvedValue(fileBefore)
    let resolveDelete!: () => void
    mockDeleteConfigFile.mockImplementation(() => new Promise<void>((resolve) => { resolveDelete = resolve }))
    renderPage('/config?path=demo')
    fireEvent.click(await screen.findByRole('button', { name: 'app.conf' }))
    const editor = await screen.findByLabelText('yaml-editor')
    fireEvent.change(editor, { target: { value: 'unsaved content' } })

    fireEvent.click(screen.getByRole('button', { name: 'Delete file' }))
    let dialog = screen.getByRole('dialog', { name: 'Delete "app.conf"?' })
    expect(within(dialog).getByText('demo/app.conf')).toBeInTheDocument()
    expect(dialog).toHaveTextContent('discard its unsaved editor changes')
    expect(dialog).toHaveTextContent('No backup is created')
    expect(mockDeleteConfigFile).not.toHaveBeenCalled()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(editor).toHaveValue('unsaved content')

    fireEvent.click(screen.getByRole('button', { name: 'Delete file' }))
    dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete file' }))
    expect(mockDeleteConfigFile).toHaveBeenCalledExactlyOnceWith('demo/app.conf', fileBefore.modified_at)
    expect(within(dialog).getByRole('button', { name: 'Deleting...' })).toBeDisabled()
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeDisabled()
    mockGetConfigTree.mockResolvedValue({ ...demoTree, items: [] })
    await act(async () => { resolveDelete() })
    expect(await screen.findByRole('status')).toHaveTextContent('Deleted demo/app.conf')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('yaml-editor')).not.toBeInTheDocument()
    expect(await screen.findByText('Empty directory')).toBeInTheDocument()
    expect(mockGetConfigTree).toHaveBeenLastCalledWith('demo')
    expect(mockGetGitWorkspaceStatus).toHaveBeenCalledOnce()
  })

  it.each([
    'File changed on disk. Reload it before deleting.',
    'File cannot be deleted due to permissions. Check access to its parent directory.',
  ])('keeps the file and unsaved editor content after deletion fails: %s', async (message) => {
    mockGetConfigTree.mockResolvedValue(demoTree)
    mockGetConfigFile.mockResolvedValue(fileBefore)
    mockDeleteConfigFile.mockRejectedValue(new Error(message))
    renderPage('/config?path=demo')
    fireEvent.click(await screen.findByRole('button', { name: 'app.conf' }))
    const editor = await screen.findByLabelText('yaml-editor')
    fireEvent.change(editor, { target: { value: 'keep this draft' } })
    fireEvent.click(screen.getByRole('button', { name: 'Delete file' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete file' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent(message)
    expect(mockGetConfigTree).toHaveBeenCalledOnce()
    expect(mockGetGitWorkspaceStatus).not.toHaveBeenCalled()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(editor).toHaveValue('keep this draft')
    expect(screen.getByRole('heading', { name: 'app.conf' })).toBeInTheDocument()
  })

  it('offers deletion for binary files that cannot be edited', async () => {
    mockGetConfigTree.mockResolvedValue(demoTree)
    mockGetConfigFile.mockResolvedValue({ ...fileBefore, type: 'binary_file', content: null, writable: false })
    renderPage('/config?path=demo')
    fireEvent.click(await screen.findByRole('button', { name: 'app.conf' }))
    expect(await screen.findByRole('button', { name: 'Delete file' })).toBeEnabled()
    expect(screen.queryByTestId('config-save')).not.toBeInTheDocument()
  })

  it('opens a URL-addressed config subtree and keeps directory navigation in the URL', async () => {
    mockGetConfigTree
      .mockResolvedValueOnce(demoTree)
      .mockResolvedValueOnce(rootTree)

    const { router } = renderPage('/config?path=demo')

    expect(await screen.findByRole('button', { name: 'app.conf' })).toBeInTheDocument()
    expect(mockGetConfigTree).toHaveBeenNthCalledWith(1, 'demo')
    expect(router.state.location.search).toBe('?path=demo')
    expect(screen.getAllByText('/opt/stacklab/config/demo', { exact: true })[0]).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '.. (up)' }))

    await waitFor(() => expect(mockGetConfigTree).toHaveBeenNthCalledWith(2, undefined))
    expect(router.state.location.search).toBe('')
  })

  it('offers a safe return to the config root for a stale deep link', async () => {
    mockGetConfigTree
      .mockRejectedValueOnce(new Error('Config directory not found'))
      .mockResolvedValueOnce(rootTree)

    const { router } = renderPage('/config?path=missing')

    expect(await screen.findByRole('alert')).toHaveTextContent('Files unavailable: Config directory not found')
    fireEvent.click(screen.getByRole('button', { name: 'Open config root' }))

    await waitFor(() => expect(mockGetConfigTree).toHaveBeenNthCalledWith(2, undefined))
    expect(router.state.location.search).toBe('')
    expect(await screen.findByRole('button', { name: 'demo' })).toBeInTheDocument()
  })

  it('ignores a superseded directory response after URL navigation', async () => {
    let resolveDemoTree!: (value: ConfigTreeResponse) => void
    const pendingDemoTree = new Promise<ConfigTreeResponse>((resolve) => {
      resolveDemoTree = resolve
    })
    mockGetConfigTree
      .mockImplementationOnce(() => pendingDemoTree)
      .mockResolvedValueOnce(rootTree)

    const { router } = renderPage('/config?path=demo')
    await waitFor(() => expect(mockGetConfigTree).toHaveBeenCalledWith('demo'))

    await act(async () => {
      await router.navigate('/config')
    })

    expect(await screen.findByRole('button', { name: 'demo' })).toBeInTheDocument()
    await act(async () => {
      resolveDemoTree(demoTree)
      await pendingDemoTree
    })

    expect(router.state.location.search).toBe('')
    expect(screen.queryByRole('button', { name: 'app.conf' })).not.toBeInTheDocument()
    expect(screen.getAllByText('/opt/stacklab/config', { exact: true })[0]).toBeInTheDocument()
  })

  it('requires confirmation before discarding config file changes', async () => {
    mockGetConfigTree
      .mockResolvedValueOnce(rootTree)
      .mockResolvedValueOnce(demoTree)
    mockGetConfigFile.mockResolvedValue(fileBefore)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'demo' }))
    fireEvent.click(await screen.findByRole('button', { name: 'app.conf' }))

    const editor = await screen.findByLabelText('yaml-editor')
    fireEvent.change(editor, { target: { value: 'server_name new.local;\n' } })

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }))
    expect(screen.getByRole('dialog', { name: 'Discard changes to "app.conf"?' })).toBeInTheDocument()
    expect(editor).toHaveValue('server_name new.local;\n')

    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }))

    expect(editor).toHaveValue('server_name old.local;\n')
  })

  it('keeps a config draft when another file is selected until the pending action is confirmed', async () => {
    const workerFile: ConfigFileResponse = {
      ...fileBefore,
      path: 'demo/worker.conf',
      name: 'worker.conf',
      content: 'worker_processes 1;\n',
    }
    mockGetConfigTree
      .mockResolvedValueOnce(rootTree)
      .mockResolvedValueOnce({
        ...demoTree,
        items: [
          ...demoTree.items,
          { ...demoTree.items[0], name: 'worker.conf', path: 'demo/worker.conf' },
        ],
      })
    mockGetConfigFile
      .mockResolvedValueOnce(fileBefore)
      .mockResolvedValueOnce(workerFile)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'demo' }))
    fireEvent.click(await screen.findByRole('button', { name: 'app.conf' }))
    const editor = await screen.findByLabelText('yaml-editor')
    fireEvent.change(editor, { target: { value: 'server_name draft.local;\n' } })

    fireEvent.click(screen.getByRole('button', { name: 'worker.conf' }))
    expect(screen.getByRole('dialog', { name: 'Discard changes to "app.conf"?' })).toBeInTheDocument()
    expect(mockGetConfigFile).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(editor).toHaveValue('server_name draft.local;\n')
    expect(mockGetConfigFile).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: 'worker.conf' }))
    fireEvent.click(screen.getByRole('button', { name: 'Discard and continue' }))

    await waitFor(() => expect(mockGetConfigFile).toHaveBeenLastCalledWith('demo/worker.conf'))
    expect(editor).toHaveValue('worker_processes 1;\n')
  })

  it('does not leave the current config directory with an unsaved file without confirmation', async () => {
    mockGetConfigTree
      .mockResolvedValueOnce(rootTree)
      .mockResolvedValueOnce(demoTree)
      .mockResolvedValueOnce(rootTree)
    mockGetConfigFile.mockResolvedValue(fileBefore)

    const { router } = renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'demo' }))
    fireEvent.click(await screen.findByRole('button', { name: 'app.conf' }))
    fireEvent.change(await screen.findByLabelText('yaml-editor'), { target: { value: 'server_name draft.local;\n' } })

    fireEvent.click(screen.getByRole('button', { name: '.. (up)' }))
    const dialog = screen.getByRole('dialog', { name: 'Discard unsaved changes?' })
    expect(mockGetConfigTree).toHaveBeenCalledTimes(2)

    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(screen.getByLabelText('yaml-editor')).toHaveValue('server_name draft.local;\n')
    expect(mockGetConfigTree).toHaveBeenCalledTimes(2)
    expect(router.state.location.search).toBe('?path=demo')

    fireEvent.click(screen.getByRole('button', { name: '.. (up)' }))
    const confirmedDialog = screen.getByRole('dialog', { name: 'Discard unsaved changes?' })

    fireEvent.click(within(confirmedDialog).getByRole('button', { name: 'Discard changes' }))

    await waitFor(() => expect(mockGetConfigTree).toHaveBeenCalledTimes(3))
    expect(screen.queryByLabelText('yaml-editor')).not.toBeInTheDocument()
    expect(router.state.location.search).toBe('')
  })

  it('protects a config draft when switching from Files to Changes mode', async () => {
    mockGetConfigTree
      .mockResolvedValueOnce(rootTree)
      .mockResolvedValueOnce(demoTree)
    mockGetConfigFile.mockResolvedValue(fileBefore)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'demo' }))
    fireEvent.click(await screen.findByRole('button', { name: 'app.conf' }))
    const editor = await screen.findByLabelText('yaml-editor')
    fireEvent.change(editor, { target: { value: 'server_name draft.local;\n' } })

    fireEvent.click(screen.getByRole('button', { name: /^Changes/ }))
    expect(screen.getByRole('dialog', { name: 'Discard changes to "app.conf"?' })).toBeInTheDocument()
    expect(mockGetGitWorkspaceStatus).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(editor).toHaveValue('server_name draft.local;\n')

    fireEvent.click(screen.getByRole('button', { name: /^Changes/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Discard and continue' }))

    expect(await screen.findByText('main')).toBeInTheDocument()
    expect(mockGetGitWorkspaceStatus).toHaveBeenCalledTimes(1)
  })

  it('marks git-ignored config entries and selected files', async () => {
    mockGetConfigTree.mockResolvedValue({
      ...rootTree,
      items: [
        {
          name: 'ignored.env',
          path: 'ignored.env',
          type: 'text_file',
          size_bytes: 12,
          modified_at: '2026-04-04T12:00:00Z',
          stack_id: null,
          git_ignored: true,
          permissions: rootTree.items[0].permissions,
        },
      ],
    })
    mockGetConfigFile.mockResolvedValue({
      ...fileBefore,
      path: 'ignored.env',
      name: 'ignored.env',
      stack_id: null,
      git_ignored: true,
    })

    renderPage()

    const ignoredButton = await screen.findByRole('button', { name: 'ignored.env' })
    expect(ignoredButton).toHaveTextContent('ignored')
    fireEvent.click(ignoredButton)

    expect(await screen.findByLabelText('yaml-editor')).toBeInTheDocument()
    expect(screen.getAllByText('ignored')).toHaveLength(2)
  })

  it('recovers the config tree through the shared desktop and mobile Retry state', async () => {
    mockGetConfigTree
      .mockRejectedValueOnce(new Error('Config tree offline'))
      .mockResolvedValueOnce(rootTree)

    renderPage()

    const desktopAlert = await screen.findByRole('alert')
    expect(desktopAlert).toHaveTextContent('Files unavailable: Config tree offline')
    expect(screen.getByRole('button', { name: 'Retry config files' })).toBeInTheDocument()
    expect(screen.queryByText('Empty directory')).not.toBeInTheDocument()

    fireEvent.click(screen.getByTestId('config-open-tree'))
    const mobileWorkspace = screen.getByRole('dialog', { name: 'Config workspace' })
    expect(within(mobileWorkspace).getByRole('alert')).toHaveTextContent(
      'Files unavailable: Config tree offline',
    )

    fireEvent.click(within(mobileWorkspace).getByRole('button', { name: 'Retry config files' }))

    await waitFor(() => expect(mockGetConfigTree).toHaveBeenCalledTimes(2))
    expect((await screen.findAllByRole('button', { name: 'demo' })).length).toBeGreaterThanOrEqual(1)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('renders Changes mode and opens a diff, then switches back to editor', async () => {
    mockGetConfigFile.mockResolvedValue(fileBefore)

    renderPage()

    const filesButton = await screen.findByRole('button', { name: 'Files' })
    const changesButton = screen.getByRole('button', { name: /^Changes/ })
    expect(filesButton).toHaveAttribute('aria-pressed', 'true')
    expect(changesButton).toHaveAttribute('aria-pressed', 'false')
    fireEvent.click(changesButton)
    expect(changesButton).toHaveAttribute('aria-pressed', 'true')

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

  it('deletes an untracked stack backup from Changes after reviewing its full path', async () => {
    const path = 'stacks/samba/compose.yaml.bak-20260806T1108'
    mockGetGitWorkspaceStatus.mockResolvedValueOnce({
      ...gitStatus, items: [{ ...gitStatus.items![0], path, scope: 'stacks', stack_id: 'samba', status: 'untracked' }],
    }).mockResolvedValue({ ...gitStatus, items: [], clean: true })
    mockGetGitWorkspaceDiff.mockResolvedValue({
      ...gitDiff, path, scope: 'stacks', stack_id: 'samba', status: 'untracked',
      delete_allowed: true, modified_at: fileBefore.modified_at,
    })
    renderPage()
    expect(screen.queryByText('compose.yaml.bak-20260806T1108')).not.toBeInTheDocument()
    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))
    fireEvent.click(await screen.findByRole('button', { name: /compose.yaml.bak-20260806T1108$/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete file' }))
    let dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText(path)).toBeInTheDocument()
    expect(within(dialog).getByText(/This file is untracked/)).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(mockDeleteGitWorkspaceFile).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Delete file' }))
    dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete file' }))
    await waitFor(() => expect(mockDeleteGitWorkspaceFile).toHaveBeenCalledExactlyOnceWith(path, fileBefore.modified_at))
    expect(await screen.findByRole('status')).toHaveTextContent(`Deleted ${path}`)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /compose.yaml.bak-20260806T1108$/ })).not.toBeInTheDocument()
    expect(mockGetGitWorkspaceStatus).toHaveBeenCalledTimes(2)
    expect(mockDeleteConfigFile).not.toHaveBeenCalled()
    expect(mockCommitGitWorkspace).not.toHaveBeenCalled()
    expect(mockPushGitWorkspace).not.toHaveBeenCalled()
  })

  it('keeps a changed file and its review visible when deletion fails', async () => {
    mockGetGitWorkspaceDiff.mockResolvedValue({ ...gitDiff, delete_allowed: true, modified_at: fileBefore.modified_at })
    mockDeleteGitWorkspaceFile.mockRejectedValue(new Error('File changed on disk. Reload its diff before deleting.'))
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))
    fireEvent.click(await screen.findByRole('button', { name: /app\.conf$/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete file' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete file' }))
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent('File changed on disk')
    expect(screen.getByRole('button', { name: /app\.conf$/ })).toBeInTheDocument()
    expect(mockGetGitWorkspaceStatus).toHaveBeenCalledOnce()
  })

  it.each([
    { delete_allowed: false, modified_at: fileBefore.modified_at },
    { delete_allowed: true },
    { delete_allowed: true, modified_at: fileBefore.modified_at, status: 'deleted' },
  ])('hides deletion when the diff is not eligible: %j', async (metadata) => {
    mockGetGitWorkspaceDiff.mockResolvedValue({ ...gitDiff, ...metadata })
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))
    fireEvent.click(await screen.findByRole('button', { name: /app\.conf$/ }))
    expect(await screen.findByText('-server_name old.local;')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete file' })).not.toBeInTheDocument()
  })

  it('keeps Git Retry available on desktop and mobile, then restores Refresh', async () => {
    mockGetGitWorkspaceStatus
      .mockRejectedValueOnce(new Error('Git status offline'))
      .mockResolvedValueOnce(gitStatus)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))

    const desktopAlert = await screen.findByRole('alert')
    expect(desktopAlert).toHaveTextContent('Git status unavailable: Git status offline')
    expect(screen.getByRole('button', { name: 'Retry Git status' })).toBeInTheDocument()
    expect(screen.queryByText('Working tree clean')).not.toBeInTheDocument()

    fireEvent.click(screen.getByTestId('config-open-tree'))
    const mobileWorkspace = screen.getByRole('dialog', { name: 'Config workspace' })
    expect(within(mobileWorkspace).getByRole('alert')).toHaveTextContent(
      'Git status unavailable: Git status offline',
    )

    fireEvent.click(within(mobileWorkspace).getByRole('button', { name: 'Retry Git status' }))

    await waitFor(() => expect(mockGetGitWorkspaceStatus).toHaveBeenCalledTimes(2))
    expect((await screen.findAllByText('main')).length).toBeGreaterThanOrEqual(1)
    expect(within(mobileWorkspace).getByRole('button', { name: 'Refresh' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
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
            git_ignored: false,
            permissions: {
              owner_uid: 1000,
              owner_name: 'bob',
              group_gid: 1000,
              group_name: 'bob',
              mode: '0644',
              readable: true,
              writable: false,
            },
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
      git_ignored: false,
      readable: true,
      writable: false,
      blocked_reason: null,
      permissions: {
        owner_uid: 1000,
        owner_name: 'bob',
        group_gid: 1000,
        group_name: 'bob',
        mode: '0644',
        readable: true,
        writable: false,
      },
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
    expect(await screen.findByTestId('git-push')).toHaveTextContent('Push (2 ahead)')
  })

  it('pushes from a clean ahead branch and hides Git actions after refresh', async () => {
    const cleanAheadStatus = {
      ...gitStatus,
      ahead_count: 1,
      items: [],
      clean: true,
    }
    mockGetGitWorkspaceStatus
      .mockResolvedValueOnce(cleanAheadStatus)
      .mockResolvedValueOnce({
        ...cleanAheadStatus,
        ahead_count: 0,
      })

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))
    fireEvent.click(await screen.findByTestId('git-push'))

    await waitFor(() => {
      expect(mockPushGitWorkspace).toHaveBeenCalled()
    })
    await waitFor(() => {
      expect(screen.queryByTestId('git-push')).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Commit' })).not.toBeInTheDocument()
    })
  })

  it('shows blocked file card in Files mode', async () => {
    mockGetConfigTree
      .mockResolvedValueOnce(rootTree)
      .mockResolvedValueOnce({
        ...demoTree,
        items: [
          {
            name: 'secret.conf',
            path: 'demo/secret.conf',
            type: 'unknown_file',
            size_bytes: 12,
            modified_at: '2026-04-04T12:00:00Z',
            stack_id: 'demo',
            git_ignored: false,
            permissions: blockedFile.permissions,
          },
        ],
      })
    mockGetConfigFile.mockResolvedValue(blockedFile)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'demo' }))
    fireEvent.click(await screen.findByRole('button', { name: 'secret.conf' }))

    expect(await screen.findByText('File access blocked')).toBeInTheDocument()
    expect(screen.getAllByText('root')).toHaveLength(2)
    expect(screen.queryByLabelText('yaml-editor')).not.toBeInTheDocument()
    expect(screen.queryByTestId('config-save')).not.toBeInTheDocument()
  })

  it('shows blocked diff state and disables commit selection for blocked files', async () => {
    mockGetGitWorkspaceStatus.mockResolvedValue(blockedGitStatus)
    mockGetGitWorkspaceDiff.mockResolvedValue(blockedGitDiff)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))

    const checkboxes = await screen.findAllByRole('checkbox')
    expect(checkboxes[2]).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: /secret\.conf/ }))

    expect(await screen.findByText('File access blocked')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Open in editor' })).not.toBeInTheDocument()
  })

  it.each(['config', 'stacks'] as const)('repairs a blocked %s file from Changes and refreshes its diff and commit availability', async (scope) => {
    const path = `${scope}/demo/secret.conf`
    const blockedDiff = { ...blockedGitDiff, scope, path, repair_capability: { supported: true, recursive: true } }
    const blockedItem = { ...blockedGitStatus.items![1], scope, path }
    const repairedDiff = { ...blockedDiff, blocked_reason: null, diff_available: true, diff: 'repaired diff' }
    mockGetGitWorkspaceStatus
      .mockResolvedValueOnce({ ...gitStatus, items: [blockedItem] })
      .mockResolvedValue({ ...gitStatus, items: [{ ...blockedItem, blocked_reason: null, diff_available: true, commit_allowed: true }] })
    mockGetGitWorkspaceDiff.mockResolvedValueOnce(blockedDiff).mockResolvedValue(repairedDiff)
    const repairResult = { repaired: true, changed_items: 1, target_permissions_before: blockedFile.permissions, target_permissions_after: fileBefore.permissions }
    mockRepairConfigWorkspacePermissions.mockResolvedValue(repairResult)
    mockRepairStackWorkspacePermissions.mockResolvedValue(repairResult)

    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))
    fireEvent.click(await screen.findByRole('button', { name: /secret\.conf/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Repair access' }))

    await waitFor(() => expect(screen.queryByText('File access blocked')).not.toBeInTheDocument())
    expect(mockGetGitWorkspaceDiff).toHaveBeenLastCalledWith(path)
    expect(mockGetGitWorkspaceDiff).toHaveBeenCalledTimes(2)
    expect(screen.getByRole('button', { name: /^Changes/, pressed: true })).toBeInTheDocument()
    expect(screen.getAllByRole('checkbox').every((checkbox) => !checkbox.hasAttribute('disabled'))).toBe(true)
    if (scope === 'config') {
      expect(mockRepairConfigWorkspacePermissions).toHaveBeenCalledWith({ path: 'demo/secret.conf', recursive: false })
      expect(mockRepairStackWorkspacePermissions).not.toHaveBeenCalled()
    } else {
      expect(mockRepairStackWorkspacePermissions).toHaveBeenCalledWith('demo', { path: 'secret.conf', recursive: false })
      expect(mockRepairConfigWorkspacePermissions).not.toHaveBeenCalled()
    }
  })

  it('keeps a repair failure visible in Changes and allows retrying', async () => {
    mockGetGitWorkspaceStatus.mockResolvedValue(blockedGitStatus)
    mockGetGitWorkspaceDiff.mockResolvedValue({ ...blockedGitDiff, repair_capability: { supported: true, recursive: true } })
    mockRepairConfigWorkspacePermissions.mockRejectedValue(new Error('Workspace helper unavailable'))

    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))
    fireEvent.click(await screen.findByRole('button', { name: /secret\.conf/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Repair access' }))

    expect(await screen.findByText('Workspace helper unavailable')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Repair access' })).toBeEnabled()
    expect(mockGetGitWorkspaceDiff).toHaveBeenCalledTimes(1)
  })

  it('does not reopen a repaired file after another change was selected', async () => {
    let finishRepair!: (result: unknown) => void
    mockGetGitWorkspaceStatus.mockResolvedValue(blockedGitStatus)
    mockGetGitWorkspaceDiff.mockImplementation((path) => Promise.resolve(path === blockedGitDiff.path
      ? { ...blockedGitDiff, repair_capability: { supported: true, recursive: true } }
      : gitDiff))
    mockRepairConfigWorkspacePermissions.mockReturnValue(new Promise((resolve) => { finishRepair = resolve }))

    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))
    fireEvent.click(await screen.findByRole('button', { name: /secret\.conf/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Repair access' }))
    fireEvent.click(screen.getByRole('button', { name: /app\.conf/ }))
    expect(await screen.findByRole('heading', { name: 'app.conf' })).toBeInTheDocument()

    await act(async () => finishRepair({ repaired: true, changed_items: 1, target_permissions_before: blockedFile.permissions, target_permissions_after: fileBefore.permissions }))

    expect(screen.getByRole('heading', { name: 'app.conf' })).toBeInTheDocument()
    expect(mockGetGitWorkspaceDiff).toHaveBeenCalledTimes(2)
  })

  it('ignores an older diff response after selecting another changed file', async () => {
    let finishDiff!: (result: GitDiffResponse) => void
    mockGetGitWorkspaceStatus.mockResolvedValue(blockedGitStatus)
    mockGetGitWorkspaceDiff.mockReturnValueOnce(new Promise((resolve) => { finishDiff = resolve })).mockResolvedValue(gitDiff)

    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))
    fireEvent.click(await screen.findByRole('button', { name: /secret\.conf/ }))
    fireEvent.click(screen.getByRole('button', { name: /app\.conf/ }))
    expect(await screen.findByRole('heading', { name: 'app.conf' })).toBeInTheDocument()
    await act(async () => finishDiff(blockedGitDiff))

    expect(screen.getByRole('heading', { name: 'app.conf' })).toBeInTheDocument()
    expect(screen.queryByText('File access blocked')).not.toBeInTheDocument()
  })

  it('group selection skips blocked files and still shows group as selected when all committable files are selected', async () => {
    mockGetGitWorkspaceStatus.mockResolvedValue(blockedGitStatus)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /^Changes/ }))

    const groupButton = await screen.findByRole('button', { name: /demo/ })
    fireEvent.click(groupButton)

    const checkboxes = screen.getAllByRole('checkbox')
    expect(checkboxes[0]).toBeChecked()
    expect(checkboxes[1]).toBeChecked()
    expect(checkboxes[2]).toBeDisabled()
    expect(checkboxes[2]).not.toBeChecked()
  })
})
