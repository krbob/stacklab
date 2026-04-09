import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { StackFilesPage } from './stack-files-page'
import type {
  StackWorkspaceFileResponse,
  StackWorkspaceTreeResponse,
} from '@/lib/api-types'

const mockNavigate = vi.fn()
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
    useNavigate: () => mockNavigate,
    useOutletContext: () => ({
      stack: { id: 'demo' },
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

describe('StackFilesPage', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    mockGetStackWorkspaceTree.mockReset()
    mockGetStackWorkspaceFile.mockReset()
    mockSaveStackWorkspaceFile.mockReset()
  })

  it('shows reserved root files and redirects them to the Editor tab', async () => {
    mockGetStackWorkspaceTree.mockResolvedValue(rootTree)
    render(<StackFilesPage />)

    const composeButton = await screen.findByRole('button', { name: /compose\.yaml/i })
    expect(screen.getByRole('button', { name: /\.env/i })).toBeInTheDocument()
    expect(screen.getByText('Dockerfile')).toBeInTheDocument()

    fireEvent.click(composeButton)
    expect(mockNavigate).toHaveBeenCalledWith('../editor', { relative: 'path' })
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

    render(<StackFilesPage />)

    fireEvent.click(await screen.findByRole('button', { name: /secret\.key/i }))

    await waitFor(() => {
      expect(screen.getByText('File access blocked')).toBeInTheDocument()
    })
    expect(screen.getAllByText('root')).toHaveLength(2)
    expect(screen.getByText('0600')).toBeInTheDocument()
  })
})
