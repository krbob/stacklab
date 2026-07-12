import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createStack, getTemplates } from '@/lib/api-client'
import { CreateStackPage } from './create-stack-page'

vi.mock('@/lib/api-client', () => ({
  createStack: vi.fn(),
  getTemplates: vi.fn(),
}))

vi.mock('@/components/yaml-editor', () => ({
  YamlEditor: ({ value, onChange, readOnly }: { value: string; onChange: (value: string) => void; readOnly?: boolean }) => (
    <textarea
      aria-label="yaml-editor"
      readOnly={readOnly}
      value={value}
      onChange={(event) => onChange(event.target.value)}
    />
  ),
}))

vi.mock('@/components/progress-panel', () => ({
  ProgressPanel: ({ jobId }: { jobId: string | null }) => <div data-testid="progress-panel">{jobId}</div>,
}))

const mockGetTemplates = vi.mocked(getTemplates)
const mockCreateStack = vi.mocked(createStack)

describe('CreateStackPage', () => {
  beforeEach(() => {
    mockGetTemplates.mockReset()
    mockCreateStack.mockReset()
    mockGetTemplates.mockResolvedValue({
      items: [
        {
          id: 'web-service',
          name: 'Generic web service',
          description: 'Single HTTP container.',
          icon: 'globe',
          compose_yaml: `services:
  app:
    image: \${IMAGE}
    ports:
      - "\${HOST_PORT}:\${CONTAINER_PORT}"
`,
          built_in: true,
          variables: [
            { name: 'IMAGE', label: 'Image', default: 'nginx:stable-alpine', required: true },
            { name: 'HOST_PORT', label: 'Host port', default: '8080', required: true },
            { name: 'CONTAINER_PORT', label: 'Container port', default: '80', required: true },
          ],
        },
      ],
    })
    mockCreateStack.mockResolvedValue({
      job: { id: 'job_create', stack_id: 'demo-web', action: 'create_stack', state: 'running', requested_at: '2026-07-09T08:00:00Z' },
    })
  })

  it('renders a selected template preview and submits template variables', async () => {
    render(
      <MemoryRouter>
        <CreateStackPage />
      </MemoryRouter>,
    )

    fireEvent.click(await screen.findByTestId('template-option-web-service'))
    const editor = screen.getByLabelText('yaml-editor') as HTMLTextAreaElement
    expect(editor.value).toContain('nginx:stable-alpine')
    expect(editor.value).toContain('"8080:80"')

    fireEvent.change(screen.getByTestId('template-variable-HOST_PORT'), { target: { value: '9090' } })
    expect(editor.value).toContain('"9090:80"')

    fireEvent.change(screen.getByTestId('create-stack-name'), { target: { value: 'demo-web' } })
    fireEvent.click(screen.getByTestId('create-stack-submit'))

    await waitFor(() => {
      expect(mockCreateStack).toHaveBeenCalledWith(expect.objectContaining({
        stack_id: 'demo-web',
        template_id: 'web-service',
        variables: expect.objectContaining({
          IMAGE: 'nginx:stable-alpine',
          HOST_PORT: '9090',
          CONTAINER_PORT: '80',
        }),
      }))
    })
  })

  it('blocks submit when a required template variable is empty', async () => {
    render(
      <MemoryRouter>
        <CreateStackPage />
      </MemoryRouter>,
    )

    fireEvent.click(await screen.findByTestId('template-option-web-service'))
    fireEvent.change(screen.getByTestId('create-stack-name'), { target: { value: 'demo-web' } })
    fireEvent.change(screen.getByTestId('template-variable-IMAGE'), { target: { value: '' } })

    expect(screen.getByText('Image is required.')).toBeInTheDocument()
    expect(screen.getByTestId('create-stack-submit')).toBeDisabled()
    fireEvent.click(screen.getByTestId('create-stack-submit'))
    expect(mockCreateStack).not.toHaveBeenCalled()
  })

  it('keeps template mode when the editor echoes the rendered compose value', async () => {
    render(
      <MemoryRouter>
        <CreateStackPage />
      </MemoryRouter>,
    )

    fireEvent.click(await screen.findByTestId('template-option-web-service'))
    const editor = screen.getByLabelText('yaml-editor') as HTMLTextAreaElement
    fireEvent.change(editor, { target: { value: editor.value } })

    expect(screen.getByTestId('template-variable-HOST_PORT')).toBeInTheDocument()
    expect(screen.getByText('Rendered compose preview')).toBeInTheDocument()
  })

  it('detaches from template mode when the rendered compose is edited', async () => {
    render(
      <MemoryRouter>
        <CreateStackPage />
      </MemoryRouter>,
    )

    fireEvent.click(await screen.findByTestId('template-option-web-service'))
    const editor = screen.getByLabelText('yaml-editor') as HTMLTextAreaElement
    fireEvent.change(editor, {
      target: {
        value: `services:
  app:
    image: caddy:2
`,
      },
    })
    fireEvent.change(screen.getByTestId('create-stack-name'), { target: { value: 'demo-web' } })
    fireEvent.click(screen.getByTestId('create-stack-submit'))

    await waitFor(() => {
      expect(mockCreateStack).toHaveBeenCalledWith(expect.objectContaining({
        stack_id: 'demo-web',
        compose_yaml: expect.stringContaining('image: caddy:2'),
      }))
    })
    expect(mockCreateStack).toHaveBeenCalledWith(expect.not.objectContaining({
      template_id: expect.any(String),
      variables: expect.any(Object),
    }))
  })
})
