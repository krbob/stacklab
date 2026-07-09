import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useOutletContext } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getDefinition, getResolvedConfig, invokeAction, resolveConfigDraft, saveDefinition } from '@/lib/api-client'
import { StackEditorPage } from './stack-editor-page'

vi.mock('react-router-dom', () => ({
  useOutletContext: vi.fn(),
}))

vi.mock('@/lib/api-client', () => ({
  getDefinition: vi.fn(),
  getResolvedConfig: vi.fn(),
  invokeAction: vi.fn(),
  resolveConfigDraft: vi.fn(),
  saveDefinition: vi.fn(),
}))

vi.mock('@/components/yaml-editor', () => ({
  YamlEditor: ({ value, onChange }: { value: string; onChange: (value: string) => void }) => (
    <textarea
      aria-label="yaml-editor"
      value={value}
      onChange={(event) => onChange(event.target.value)}
    />
  ),
}))

vi.mock('@/components/progress-panel', () => ({
  ProgressPanel: ({ jobId }: { jobId: string | null }) => <div data-testid="progress-panel">{jobId}</div>,
}))

const mockUseOutletContext = vi.mocked(useOutletContext)
const mockGetDefinition = vi.mocked(getDefinition)
const mockGetResolvedConfig = vi.mocked(getResolvedConfig)
const mockResolveConfigDraft = vi.mocked(resolveConfigDraft)
const mockSaveDefinition = vi.mocked(saveDefinition)
const mockInvokeAction = vi.mocked(invokeAction)

const stack = {
  id: 'demo',
  name: 'demo',
  root_path: '/srv/stacklab/stacks/demo',
  compose_file_path: '/srv/stacklab/stacks/demo/compose.yaml',
  env_file_path: '/srv/stacklab/stacks/demo/.env',
  config_path: '/srv/stacklab/config/demo',
  data_path: '/srv/stacklab/data/demo',
  display_state: 'running' as const,
  runtime_state: 'running' as const,
  config_state: 'in_sync' as const,
  activity_state: 'idle' as const,
  health_summary: { healthy_container_count: 1, unhealthy_container_count: 0, unknown_health_container_count: 0 },
  capabilities: { terminal: true, logs: true, stats: true, files: true },
  available_actions: ['up', 'save_definition'] as const,
  services: [],
  containers: [],
  last_deployed_at: '2026-07-06T03:17:00Z',
  last_action: null,
}

describe('StackEditorPage', () => {
  beforeEach(() => {
    mockUseOutletContext.mockReset()
    mockGetDefinition.mockReset()
    mockGetResolvedConfig.mockReset()
    mockResolveConfigDraft.mockReset()
    mockSaveDefinition.mockReset()
    mockInvokeAction.mockReset()

    mockUseOutletContext.mockReturnValue({ stack, refetch: vi.fn() })
    mockGetDefinition.mockResolvedValue({
      stack_id: 'demo',
      files: {
        compose_yaml: {
          path: '/srv/stacklab/stacks/demo/compose.yaml',
          content: 'services:\n  app:\n    image: nginx:alpine\n',
          modified_at: '2026-07-09T08:00:00Z',
        },
        env: {
          path: '/srv/stacklab/stacks/demo/.env',
          content: '',
          exists: false,
          modified_at: null,
        },
      },
      config_state: 'in_sync',
    })
    mockGetResolvedConfig.mockImplementation((_stackId, source) => Promise.resolve({
      stack_id: 'demo',
      valid: true,
      content: source === 'last_valid'
        ? 'services:\n  app:\n    image: nginx:last-valid\n'
        : 'services:\n  app:\n    image: nginx:alpine\n',
      warnings: [],
    }))
  })

  it('marks draft validation stale after editing a last deployed preview', async () => {
    render(<StackEditorPage />)

    await screen.findByText('✓ Config valid')
    const saveDeploy = screen.getByTestId('editor-save-deploy')
    expect(saveDeploy).not.toBeDisabled()

    fireEvent.click(screen.getByText('Last deployed'))
    await waitFor(() => {
      expect(mockGetResolvedConfig).toHaveBeenCalledWith('demo', 'last_valid')
    })
    expect(screen.getByText('Last deployed config')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('yaml-editor'), {
      target: { value: 'services:\n  app:\n    image: [\n' },
    })

    expect(screen.queryByText('✓ Config valid')).not.toBeInTheDocument()
    expect(screen.getByText('Preview current changes before deploy')).toBeInTheDocument()
    expect(saveDeploy).toBeDisabled()
    expect(mockResolveConfigDraft).not.toHaveBeenCalled()
  })

  it('saves with the loaded definition revision', async () => {
    mockSaveDefinition.mockResolvedValue({
      job: { id: 'job-save', stack_id: 'demo', action: 'save_definition', state: 'succeeded' },
    })

    render(<StackEditorPage />)

    await screen.findByText('✓ Config valid')
    fireEvent.change(screen.getByLabelText('yaml-editor'), {
      target: { value: 'services:\n  app:\n    image: nginx:stable\n' },
    })
    fireEvent.click(screen.getByTestId('editor-save'))

    await waitFor(() => {
      expect(mockSaveDefinition).toHaveBeenCalledWith('demo', expect.objectContaining({
        compose_yaml: 'services:\n  app:\n    image: nginx:stable\n',
        expected_revision: {
          compose_modified_at: '2026-07-09T08:00:00Z',
          env_modified_at: null,
        },
      }))
    })
  })
})
