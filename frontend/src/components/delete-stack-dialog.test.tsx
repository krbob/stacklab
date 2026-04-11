import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { DeleteStackDialog } from './delete-stack-dialog'

const mockDeleteStack = vi.fn()

vi.mock('@/lib/api-client', () => ({
  deleteStack: (...args: unknown[]) => mockDeleteStack(...args),
}))

vi.mock('@/components/progress-panel', () => ({
  ProgressPanel: ({ jobId }: { jobId: string }) => <div data-testid="progress">{jobId}</div>,
}))

function renderDialog(onClose = vi.fn()) {
  return render(
    <MemoryRouter>
      <DeleteStackDialog stackId="demo" stackName="demo" onClose={onClose} />
    </MemoryRouter>,
  )
}

describe('DeleteStackDialog', () => {
  beforeEach(() => {
    mockDeleteStack.mockReset()
  })

  it('renders with runtime checkbox checked by default', () => {
    renderDialog()
    expect(screen.getByLabelText(/Stop and remove containers/)).toBeChecked()
    expect(screen.getByLabelText(/Delete stack definition/)).not.toBeChecked()
    expect(screen.getByLabelText(/Delete this stack's config directory/)).not.toBeChecked()
    expect(screen.getByLabelText(/Delete this stack's data directory/)).not.toBeChecked()
  })

  it('shows confirm button enabled with default selection', () => {
    renderDialog()
    expect(screen.getByTestId('delete-confirm')).not.toBeDisabled()
  })

  it('disables confirm when nothing is selected', () => {
    renderDialog()
    fireEvent.click(screen.getByLabelText(/Stop and remove containers/))
    expect(screen.getByTestId('delete-confirm')).toBeDisabled()
  })

  it('shows data deletion warning when data checkbox is checked', () => {
    renderDialog()
    fireEvent.click(screen.getByLabelText(/Delete this stack's data directory/))
    expect(screen.getByText(/Deleting data is irreversible/)).toBeInTheDocument()
  })

  it('calls deleteStack with correct flags', async () => {
    mockDeleteStack.mockResolvedValue({ job: { id: 'job_del', stack_id: 'demo', action: 'remove_stack_definition', state: 'running' } })

    renderDialog()
    fireEvent.click(screen.getByLabelText(/Delete stack definition/))
    fireEvent.click(screen.getByTestId('delete-confirm'))

    await waitFor(() => {
      expect(mockDeleteStack).toHaveBeenCalledWith('demo', {
        remove_runtime: true,
        remove_definition: true,
        remove_config: false,
        remove_data: false,
      })
    })
  })

  it('shows progress panel after successful delete request', async () => {
    mockDeleteStack.mockResolvedValue({ job: { id: 'job_del_123', stack_id: 'demo', action: 'remove_stack_definition', state: 'running' } })

    renderDialog()
    fireEvent.click(screen.getByTestId('delete-confirm'))

    expect(await screen.findByTestId('progress')).toHaveTextContent('job_del_123')
  })

  it('shows error on delete failure', async () => {
    mockDeleteStack.mockRejectedValue(new Error('Stack is locked'))

    renderDialog()
    fireEvent.click(screen.getByTestId('delete-confirm'))

    expect(await screen.findByText('Stack is locked')).toBeInTheDocument()
  })

  it('calls onClose when Cancel is clicked', () => {
    const onClose = vi.fn()
    renderDialog(onClose)
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onClose).toHaveBeenCalled()
  })
})
