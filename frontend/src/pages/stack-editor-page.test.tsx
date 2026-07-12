import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { createMemoryRouter, RouterProvider, useOutletContext } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getDefinition, getResolvedConfig, invokeAction, resolveConfigDraft, saveDefinition } from '@/lib/api-client'
import { StackEditorPage } from './stack-editor-page'
import type { DefinitionResponse, ResolvedConfigResponse } from '@/lib/api-types'

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useOutletContext: vi.fn() }
})

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
  ProgressPanel: ({ jobId, onDone }: { jobId: string | null; onDone?: (state: string) => void }) => (
    <div data-testid="progress-panel">
      {jobId}
      <button type="button" onClick={() => onDone?.('succeeded')}>Finish {jobId}</button>
    </div>
  ),
}))

const mockUseOutletContext = vi.mocked(useOutletContext)
const mockGetDefinition = vi.mocked(getDefinition)
const mockGetResolvedConfig = vi.mocked(getResolvedConfig)
const mockResolveConfigDraft = vi.mocked(resolveConfigDraft)
const mockSaveDefinition = vi.mocked(saveDefinition)
const mockInvokeAction = vi.mocked(invokeAction)

function renderPage() {
  const router = createMemoryRouter([{ path: '/', element: <StackEditorPage /> }])
  return render(<RouterProvider router={router} />)
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })

  return { promise, resolve, reject }
}

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

const definition: DefinitionResponse = {
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
    mockGetDefinition.mockResolvedValue(definition)
    mockGetResolvedConfig.mockImplementation((_stackId, source) => Promise.resolve({
      stack_id: 'demo',
      valid: true,
      content: source === 'last_valid'
        ? 'services:\n  app:\n    image: nginx:last-valid\n'
        : 'services:\n  app:\n    image: nginx:alpine\n',
      warnings: [],
    }))
  })

  it('exposes definition files as keyboard-operated tabs', async () => {
    renderPage()
    await screen.findByText('✓ Config valid')

    expect(screen.getByRole('tablist', { name: 'Definition files' })).toBeInTheDocument()
    const composeTab = screen.getByRole('tab', { name: 'compose.yaml' })
    const envTab = screen.getByRole('tab', { name: '.env (new)' })
    expect(composeTab).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tabpanel')).toHaveAccessibleName('compose.yaml')

    composeTab.focus()
    fireEvent.keyDown(composeTab, { key: 'ArrowRight' })

    expect(envTab).toHaveFocus()
    expect(envTab).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tabpanel')).toHaveAccessibleName('.env (new)')

    fireEvent.keyDown(envTab, { key: 'ArrowRight' })
    expect(composeTab).toHaveFocus()
    expect(composeTab).toHaveAttribute('aria-selected', 'true')
  })

  it('marks draft validation stale after editing a last deployed preview', async () => {
    renderPage()

    await screen.findByText('✓ Config valid')
    const saveDeploy = screen.getByTestId('editor-save-deploy')
    expect(saveDeploy).toBeDisabled()

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
    expect(saveDeploy).not.toBeDisabled()
    expect(mockResolveConfigDraft).not.toHaveBeenCalled()
  })

  it('previews a stale draft before save and deploy', async () => {
    mockResolveConfigDraft.mockResolvedValue({
      stack_id: 'demo',
      valid: true,
      content: 'services:\n  app:\n    image: nginx:stable\n',
      warnings: [],
    })
    mockSaveDefinition.mockResolvedValue({
      job: { id: 'job-save-deploy', stack_id: 'demo', action: 'save_definition', state: 'running', requested_at: '2026-07-09T08:00:00Z' },
      definition,
    })

    renderPage()

    await screen.findByText('✓ Config valid')
    fireEvent.change(screen.getByLabelText('yaml-editor'), {
      target: { value: 'services:\n  app:\n    image: nginx:stable\n' },
    })
    fireEvent.click(screen.getByTestId('editor-save-deploy'))

    await waitFor(() => {
      expect(mockResolveConfigDraft).toHaveBeenCalledWith('demo', {
        compose_yaml: 'services:\n  app:\n    image: nginx:stable\n',
        env: '',
      })
    })
    await waitFor(() => {
      expect(mockSaveDefinition).toHaveBeenCalledWith('demo', expect.objectContaining({
        compose_yaml: 'services:\n  app:\n    image: nginx:stable\n',
      }))
    })
  })

  it('does not save and deploy when automatic preview fails', async () => {
    mockResolveConfigDraft.mockResolvedValue({
      stack_id: 'demo',
      valid: false,
      error: {
        code: 'validation_failed',
        message: 'compose is invalid',
      },
    })

    renderPage()

    await screen.findByText('✓ Config valid')
    fireEvent.change(screen.getByLabelText('yaml-editor'), {
      target: { value: 'services:\n  app:\n    image: [\n' },
    })
    fireEvent.click(screen.getByTestId('editor-save-deploy'))

    await waitFor(() => {
      expect(mockResolveConfigDraft).toHaveBeenCalled()
    })
    expect(mockSaveDefinition).not.toHaveBeenCalled()
    expect(await screen.findByText('✗ compose is invalid')).toBeInTheDocument()
  })

  it('saves with the loaded definition revision', async () => {
    mockSaveDefinition.mockResolvedValue({
      job: { id: 'job-save', stack_id: 'demo', action: 'save_definition', state: 'succeeded', requested_at: '2026-07-09T08:00:00Z' },
      definition,
    })

    renderPage()

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

  it('requires confirmation before discarding editor changes', async () => {
    renderPage()

    await screen.findByText('✓ Config valid')
    const editor = screen.getByLabelText('yaml-editor')
    fireEvent.change(editor, {
      target: { value: 'services:\n  app:\n    image: nginx:edited\n' },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }))
    expect(screen.getByRole('dialog', { name: 'Discard unsaved changes?' })).toBeInTheDocument()
    expect(editor).toHaveValue('services:\n  app:\n    image: nginx:edited\n')

    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }))

    expect(editor).toHaveValue('services:\n  app:\n    image: nginx:alpine\n')
  })

  it('keeps the loaded definition editable when the optional resolved preview fails', async () => {
    mockGetResolvedConfig.mockRejectedValue(new Error('Docker is unavailable'))

    renderPage()

    const editor = await screen.findByLabelText('yaml-editor')
    expect(editor).toHaveValue('services:\n  app:\n    image: nginx:alpine\n')
    expect(await screen.findByRole('alert')).toHaveTextContent('Docker is unavailable')
    expect(screen.getByRole('button', { name: 'Retry resolved preview' })).toBeInTheDocument()
    expect(screen.getByTestId('editor-save')).toBeDisabled()

    fireEvent.change(editor, {
      target: { value: 'services:\n  app:\n    image: nginx:edited\n' },
    })
    expect(screen.getByTestId('editor-save')).not.toBeDisabled()
  })

  it('does not render an empty editor when the definition fails and retries the load', async () => {
    mockGetDefinition
      .mockRejectedValueOnce(new Error('Definition request failed'))
      .mockResolvedValueOnce({
        stack_id: 'demo',
        files: {
          compose_yaml: {
            path: '/srv/stacklab/stacks/demo/compose.yaml',
            content: 'services:\n  recovered:\n    image: nginx:stable\n',
            modified_at: '2026-07-09T09:00:00Z',
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

    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Definition request failed')
    expect(screen.queryByLabelText('yaml-editor')).not.toBeInTheDocument()
    expect(screen.queryByTestId('editor-save')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))

    expect(await screen.findByLabelText('yaml-editor')).toHaveValue('services:\n  recovered:\n    image: nginx:stable\n')
    expect(mockGetDefinition).toHaveBeenCalledTimes(2)
    expect(screen.getByTestId('editor-save')).toBeDisabled()
  })

  it('preserves and retries a stale resolved preview after a successful save', async () => {
    const initialResolved: ResolvedConfigResponse = {
      stack_id: 'demo',
      valid: true,
      content: 'services:\n  app:\n    image: nginx:previous-preview\n',
      warnings: [],
    }
    const recoveredResolved: ResolvedConfigResponse = {
      stack_id: 'demo',
      valid: true,
      content: 'services:\n  app:\n    image: nginx:refreshed-preview\n',
      warnings: [],
    }
    const refresh = deferred<ResolvedConfigResponse>()
    mockGetResolvedConfig
      .mockResolvedValueOnce(initialResolved)
      .mockReturnValueOnce(refresh.promise)
      .mockResolvedValueOnce(recoveredResolved)
    mockSaveDefinition.mockResolvedValue({
      job: { id: 'job-save-refresh', stack_id: 'demo', action: 'save_definition', state: 'running', requested_at: '2026-07-09T08:00:00Z' },
      definition: {
        ...definition,
        files: {
          ...definition.files,
          compose_yaml: {
            ...definition.files.compose_yaml,
            content: 'services:\n  app:\n    image: nginx:saved\n',
            modified_at: '2026-07-09T09:00:00Z',
          },
        },
      },
    })

    renderPage()

    const editor = await screen.findByLabelText('yaml-editor')
    const preview = within(screen.getByTestId('resolved-preview'))
    expect(await preview.findByText(/nginx:previous-preview/)).toBeInTheDocument()

    fireEvent.change(editor, {
      target: { value: 'services:\n  app:\n    image: nginx:saved\n' },
    })
    fireEvent.click(screen.getByTestId('editor-save'))
    expect(await screen.findByText('job-save-refresh')).toBeInTheDocument()

    fireEvent.change(editor, {
      target: { value: 'services:\n  app:\n    image: nginx:newer-draft\n' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Finish job-save-refresh' }))

    await waitFor(() => expect(mockGetResolvedConfig).toHaveBeenCalledTimes(2))
    expect(preview.getByRole('status')).toHaveTextContent('Refreshing resolved preview...')
    expect(preview.getByText(/nginx:previous-preview/)).toBeInTheDocument()

    await act(async () => {
      refresh.reject(new Error('Docker restarted during preview refresh'))
      await Promise.allSettled([refresh.promise])
      await Promise.resolve()
    })

    expect(preview.getByRole('alert')).toHaveTextContent(
      'Failed to refresh resolved preview: Docker restarted during preview refresh',
    )
    expect(preview.getByRole('alert')).toHaveTextContent('Showing the last successfully loaded preview.')
    expect(preview.getByText(/nginx:previous-preview/)).toBeInTheDocument()

    fireEvent.click(preview.getByRole('button', { name: 'Retry resolved preview' }))

    expect(await preview.findByText(/nginx:refreshed-preview/)).toBeInTheDocument()
    expect(preview.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByText('Preview current changes before deploy')).toBeInTheDocument()
    expect(editor).toHaveValue('services:\n  app:\n    image: nginx:newer-draft\n')
    expect(mockSaveDefinition).toHaveBeenCalledTimes(1)
    expect(mockInvokeAction).not.toHaveBeenCalled()
  })

  it('continues Save & Deploy when the post-save preview refresh fails', async () => {
    mockGetResolvedConfig
      .mockResolvedValueOnce({
        stack_id: 'demo',
        valid: true,
        content: 'services:\n  app:\n    image: nginx:previous-preview\n',
        warnings: [],
      })
      .mockRejectedValueOnce(new Error('preview endpoint unavailable'))
    mockResolveConfigDraft.mockResolvedValue({
      stack_id: 'demo',
      valid: true,
      content: 'services:\n  app:\n    image: nginx:next\n',
      warnings: [],
    })
    mockSaveDefinition.mockResolvedValue({
      job: { id: 'job-save-deploy', stack_id: 'demo', action: 'save_definition', state: 'running', requested_at: '2026-07-09T08:00:00Z' },
      definition,
    })
    mockInvokeAction.mockResolvedValue({
      job: { id: 'job-deploy', stack_id: 'demo', action: 'up', state: 'running', requested_at: '2026-07-09T08:00:01Z' },
    })

    renderPage()

    await screen.findByText('✓ Config valid')
    fireEvent.change(screen.getByLabelText('yaml-editor'), {
      target: { value: 'services:\n  app:\n    image: nginx:next\n' },
    })
    fireEvent.click(screen.getByTestId('editor-save-deploy'))
    expect(await screen.findByText('job-save-deploy')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Finish job-save-deploy' }))

    await waitFor(() => expect(mockInvokeAction).toHaveBeenCalledWith('demo', 'up'))
    expect(await screen.findByText('job-deploy')).toBeInTheDocument()
    expect(within(screen.getByTestId('resolved-preview')).getByRole('alert')).toHaveTextContent(
      'Failed to refresh resolved preview: preview endpoint unavailable',
    )
    expect(mockSaveDefinition).toHaveBeenCalledTimes(1)
    expect(mockInvokeAction).toHaveBeenCalledTimes(1)
  })
})
