import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { StackFilesPage } from './stack-files-page'
import type {
  StackWorkspaceFileResponse,
  StackWorkspaceTreeResponse,
} from '@/lib/api-types'

const mockGetStackWorkspaceTree = vi.fn()
const mockGetStackWorkspaceFile = vi.fn()
const mockSaveStackWorkspaceFile = vi.fn()

const unsupportedRepairCapability = {
  supported: false,
  reason: 'Workspace permission repair is not configured yet.',
  recursive: true,
}

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useOutletContext: () => ({
      stack: {
        id: 'demo',
        root_path: '/opt/stacklab/stacks/demo',
        config_path: '/opt/stacklab/config/demo',
      },
    }),
  }
})

vi.mock('@/lib/api-client', () => ({
  getStackWorkspaceTree: (...args: unknown[]) => mockGetStackWorkspaceTree(...args),
  getStackWorkspaceFile: (...args: unknown[]) => mockGetStackWorkspaceFile(...args),
  saveStackWorkspaceFile: (...args: unknown[]) => mockSaveStackWorkspaceFile(...args),
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
      aria-label="stack-file-editor"
      readOnly={readOnly}
      value={value}
      onChange={(event) => onChange(event.target.value)}
    />
  ),
}))

const rootTree: StackWorkspaceTreeResponse = {
  stack_id: 'demo',
  workspace_root: '/opt/stacklab/stacks/demo',
  current_path: '',
  parent_path: null,
  items: [
    {
      name: 'compose.yaml',
      path: 'compose.yaml',
      type: 'text_file',
      size_bytes: 42,
      modified_at: '2026-04-09T10:00:00Z',
      permissions: {
        owner_uid: 1000,
        owner_name: 'stacklab',
        group_gid: 1000,
        group_name: 'stacklab',
        mode: '0644',
        readable: true,
        writable: true,
      },
    },
    {
      name: '.env',
      path: '.env',
      type: 'text_file',
      size_bytes: 12,
      modified_at: '2026-04-09T10:00:00Z',
      permissions: {
        owner_uid: 1000,
        owner_name: 'stacklab',
        group_gid: 1000,
        group_name: 'stacklab',
        mode: '0644',
        readable: true,
        writable: true,
      },
    },
    {
      name: 'Dockerfile',
      path: 'Dockerfile',
      type: 'text_file',
      size_bytes: 24,
      modified_at: '2026-04-09T10:00:00Z',
      permissions: {
        owner_uid: 1000,
        owner_name: 'stacklab',
        group_gid: 1000,
        group_name: 'stacklab',
        mode: '0644',
        readable: true,
        writable: true,
      },
    },
  ],
}

const blockedFile: StackWorkspaceFileResponse = {
  stack_id: 'demo',
  path: 'secret.key',
  name: 'secret.key',
  type: 'unknown_file',
  content: null,
  encoding: null,
  size_bytes: 64,
  modified_at: '2026-04-09T10:00:00Z',
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

const dockerfileBefore: StackWorkspaceFileResponse = {
  stack_id: 'demo',
  path: 'Dockerfile',
  name: 'Dockerfile',
  type: 'text_file',
  content: 'FROM alpine:3.20\n',
  encoding: 'utf-8',
  size_bytes: 17,
  modified_at: '2026-04-09T10:00:00Z',
  readable: true,
  writable: true,
  blocked_reason: null,
  permissions: rootTree.items[2].permissions,
  repair_capability: unsupportedRepairCapability,
}

const dockerfileAfter: StackWorkspaceFileResponse = {
  ...dockerfileBefore,
  content: 'FROM alpine:3.21\n',
  modified_at: '2026-04-09T10:01:00Z',
}

function renderPage() {
  const router = createMemoryRouter(
    [{ path: '*', element: <StackFilesPage /> }],
    { initialEntries: ['/stacks/demo/files'] },
  )
  return { router, ...render(<RouterProvider router={router} />) }
}

describe('StackFilesPage', () => {
  beforeEach(() => {
    mockGetStackWorkspaceTree.mockReset()
    mockGetStackWorkspaceFile.mockReset()
    mockSaveStackWorkspaceFile.mockReset()
  })

  it('distinguishes stack-local files from managed config and links to its subtree', async () => {
    mockGetStackWorkspaceTree.mockResolvedValue(rootTree)

    renderPage()

    const locations = screen.getByRole('region', { name: 'Stack file locations' })
    expect(locations).toHaveTextContent('/opt/stacklab/stacks/demo')
    expect(locations).toHaveTextContent('/opt/stacklab/config/demo')
    expect(within(locations).getByRole('link', { name: 'Open managed config' })).toHaveAttribute('href', '/config?path=demo')
    expect(await screen.findByRole('button', { name: /compose\.yaml/i })).toBeInTheDocument()
  })

  it('shows a file-tree failure with Retry and recovers without a false empty state', async () => {
    mockGetStackWorkspaceTree
      .mockRejectedValueOnce(new Error('workspace tree unavailable'))
      .mockResolvedValueOnce(rootTree)

    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Failed to load file tree: workspace tree unavailable',
    )
    expect(screen.queryByText('Empty directory')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Retry file tree' }))

    expect(await screen.findByRole('button', { name: /compose\.yaml/i })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(mockGetStackWorkspaceTree).toHaveBeenCalledTimes(2)
  })

  it('shows reserved root files and redirects them to the Editor tab', async () => {
    mockGetStackWorkspaceTree.mockResolvedValue(rootTree)
    const { router } = renderPage()

    const composeButton = await screen.findByRole('button', { name: /compose\.yaml/i })
    expect(screen.getByRole('button', { name: /\.env/i })).toBeInTheDocument()
    expect(screen.getByText('Dockerfile')).toBeInTheDocument()

    fireEvent.click(composeButton)
    expect(router.state.location.pathname).toBe('/stacks/demo/editor')
  })

  it('renders blocked files with the blocked file card', async () => {
    mockGetStackWorkspaceTree.mockResolvedValue({
      ...rootTree,
      items: [
        {
          name: 'secret.key',
          path: 'secret.key',
          type: 'unknown_file',
          size_bytes: 64,
          modified_at: '2026-04-09T10:00:00Z',
          permissions: blockedFile.permissions,
        },
      ],
    })
    mockGetStackWorkspaceFile.mockResolvedValue(blockedFile)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /secret\.key/i }))

    await waitFor(() => {
      expect(screen.getByText('File access blocked')).toBeInTheDocument()
    })
    expect(screen.getAllByText('root')).toHaveLength(2)
    expect(screen.getByText('0600')).toBeInTheDocument()
  })

  it('saves stack workspace files with the loaded modified_at', async () => {
    mockGetStackWorkspaceTree.mockResolvedValue(rootTree)
    mockGetStackWorkspaceFile
      .mockResolvedValueOnce(dockerfileBefore)
      .mockResolvedValueOnce(dockerfileAfter)
    mockSaveStackWorkspaceFile.mockResolvedValue({
      saved: true,
      stack_id: 'demo',
      path: 'Dockerfile',
      modified_at: '2026-04-09T10:01:00Z',
      audit_action: 'save_stack_file',
    })

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /Dockerfile/i }))
    const editor = await screen.findByLabelText('stack-file-editor')
    fireEvent.change(editor, { target: { value: 'FROM alpine:3.21\n' } })
    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => {
      expect(mockSaveStackWorkspaceFile).toHaveBeenCalledWith('demo', 'Dockerfile', 'FROM alpine:3.21\n', false, '2026-04-09T10:00:00Z')
    })
    expect(await screen.findByRole('status')).toHaveTextContent('Saved')
  })

  it('requires confirmation before discarding stack file changes', async () => {
    mockGetStackWorkspaceTree.mockResolvedValue(rootTree)
    mockGetStackWorkspaceFile.mockResolvedValue(dockerfileBefore)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /Dockerfile/i }))
    const editor = await screen.findByLabelText('stack-file-editor')
    fireEvent.change(editor, { target: { value: 'FROM alpine:3.21\n' } })

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }))
    expect(screen.getByRole('dialog', { name: 'Discard changes to "Dockerfile"?' })).toBeInTheDocument()
    expect(editor).toHaveValue('FROM alpine:3.21\n')

    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }))

    expect(editor).toHaveValue('FROM alpine:3.20\n')
  })

  it('keeps a stack file draft until opening another file is confirmed', async () => {
    const nginxFile: StackWorkspaceFileResponse = {
      ...dockerfileBefore,
      path: 'nginx.conf',
      name: 'nginx.conf',
      content: 'worker_processes 1;\n',
    }
    mockGetStackWorkspaceTree.mockResolvedValue({
      ...rootTree,
      items: [
        ...rootTree.items,
        { ...rootTree.items[2], name: 'nginx.conf', path: 'nginx.conf' },
      ],
    })
    mockGetStackWorkspaceFile
      .mockResolvedValueOnce(dockerfileBefore)
      .mockResolvedValueOnce(nginxFile)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /Dockerfile/i }))
    const editor = await screen.findByLabelText('stack-file-editor')
    fireEvent.change(editor, { target: { value: 'FROM alpine:edge\n' } })

    fireEvent.click(screen.getByRole('button', { name: 'nginx.conf' }))
    expect(screen.getByRole('dialog', { name: 'Discard changes to "Dockerfile"?' })).toBeInTheDocument()
    expect(mockGetStackWorkspaceFile).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(editor).toHaveValue('FROM alpine:edge\n')
    expect(mockGetStackWorkspaceFile).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: 'nginx.conf' }))
    fireEvent.click(screen.getByRole('button', { name: 'Discard and continue' }))

    await waitFor(() => expect(mockGetStackWorkspaceFile).toHaveBeenLastCalledWith('demo', 'nginx.conf'))
    expect(editor).toHaveValue('worker_processes 1;\n')
  })

  it('does not enter another stack directory with an unsaved file without confirmation', async () => {
    const directoryEntry = {
      ...rootTree.items[2],
      name: 'configs',
      path: 'configs',
      type: 'directory' as const,
      size_bytes: 0,
    }
    mockGetStackWorkspaceTree
      .mockResolvedValueOnce({ ...rootTree, items: [...rootTree.items, directoryEntry] })
      .mockResolvedValueOnce({
        ...rootTree,
        current_path: 'configs',
        parent_path: '',
        items: [],
      })
    mockGetStackWorkspaceFile.mockResolvedValue(dockerfileBefore)

    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /Dockerfile/i }))
    fireEvent.change(await screen.findByLabelText('stack-file-editor'), { target: { value: 'FROM alpine:edge\n' } })

    fireEvent.click(screen.getByRole('button', { name: 'configs' }))
    expect(screen.getByRole('dialog', { name: 'Discard changes to "Dockerfile"?' })).toBeInTheDocument()
    expect(mockGetStackWorkspaceTree).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: 'Discard and continue' }))

    await waitFor(() => expect(mockGetStackWorkspaceTree).toHaveBeenLastCalledWith('demo', 'configs'))
    expect(screen.queryByLabelText('stack-file-editor')).not.toBeInTheDocument()
  })
})
