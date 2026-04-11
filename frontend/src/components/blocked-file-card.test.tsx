import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { BlockedFileCard } from './blocked-file-card'
import type { FilePermissions } from '@/lib/api-types'

const permissions: FilePermissions = {
  owner_uid: 0,
  owner_name: 'root',
  group_gid: 0,
  group_name: 'root',
  mode: '0600',
  readable: false,
  writable: false,
}

describe('BlockedFileCard', () => {
  it('shows not_readable message and permission grid', () => {
    render(<BlockedFileCard blockedReason="not_readable" permissions={permissions} />)

    expect(screen.getByText('File access blocked')).toBeInTheDocument()
    expect(screen.getByText(/not readable by the Stacklab service user/)).toBeInTheDocument()
    expect(screen.getAllByText('root')).toHaveLength(2) // owner + group
    expect(screen.getByText('0600')).toBeInTheDocument()
    expect(screen.getAllByText('No')).toHaveLength(2)
  })

  it('shows generic message for unknown reason', () => {
    render(<BlockedFileCard blockedReason="something_else" permissions={null} />)

    expect(screen.getByText('File access blocked')).toBeInTheDocument()
    expect(screen.getByText(/cannot access this file/)).toBeInTheDocument()
  })

  it('shows readable Yes when file is readable but not writable', () => {
    const readOnly: FilePermissions = { ...permissions, readable: true, writable: false, mode: '0444' }
    render(<BlockedFileCard blockedReason="not_writable" permissions={readOnly} />)

    expect(screen.getByText('Yes')).toBeInTheDocument()
    expect(screen.getByText('No')).toBeInTheDocument()
  })

  it('does not show recursive repair by default for blocked files', () => {
    const onRepair = vi.fn().mockResolvedValue({
      repaired: true,
      changed_items: 1,
      target_permissions_before: permissions,
      target_permissions_after: { ...permissions, owner_name: 'stacklab', readable: true, writable: true, mode: '0600' },
      warnings: [],
    })

    render(
      <BlockedFileCard
        stateKey="demo/secret.conf"
        blockedReason="not_readable"
        permissions={permissions}
        repairCapability={{ supported: true, recursive: true }}
        onRepair={onRepair}
      />,
    )

    expect(screen.getByRole('button', { name: 'Repair access' })).toBeInTheDocument()
    expect(screen.queryByLabelText('Repair recursively')).not.toBeInTheDocument()
  })

  it('resets repair result when switching to another blocked file', async () => {
    const onRepair = vi.fn().mockResolvedValue({
      repaired: true,
      changed_items: 1,
      target_permissions_before: permissions,
      target_permissions_after: { ...permissions, owner_name: 'stacklab', readable: true, writable: true, mode: '0600' },
      warnings: [],
    })

    const { rerender } = render(
      <BlockedFileCard
        stateKey="demo/secret-a.conf"
        blockedReason="not_readable"
        permissions={permissions}
        repairCapability={{ supported: true, recursive: true }}
        onRepair={onRepair}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Repair access' }))
    expect(await screen.findByText('Repaired (1 item changed)')).toBeInTheDocument()

    rerender(
      <BlockedFileCard
        stateKey="demo/secret-b.conf"
        blockedReason="not_readable"
        permissions={permissions}
        repairCapability={{ supported: true, recursive: true }}
        onRepair={onRepair}
      />,
    )

    expect(screen.queryByText('Repaired (1 item changed)')).not.toBeInTheDocument()
  })
})
