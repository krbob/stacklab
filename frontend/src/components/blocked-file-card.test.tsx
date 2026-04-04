import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
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
})
