import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { ResticVersion } from './restic-version'

describe('Restic compatibility guidance', () => {
  it.each(['0.12.1', '0.13.9'])('warns below the minimum: %s', version => {
    render(<ResticVersion version={version} />)
    expect(screen.getByRole('alert')).toHaveTextContent('below the supported minimum (0.14.0)')
  })
  it.each(['0.14.0', '0.16.4'])('explains legacy limitations: %s', version => {
    render(<ResticVersion version={version} />)
    expect(screen.getByText(/limited retry diagnostics/)).toBeVisible()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.queryByText(/Cancellation during lock creation/)).toBe(version === '0.14.0' ? screen.getByText(/Cancellation during lock creation/) : null)
  })
  it.each(['0.17.3', '0.18.0', '1.0.0'])('does not mislabel supported versions: %s', version => {
    render(<ResticVersion version={version} />)
    expect(screen.getByText(`Restic ${version}.`)).toBeVisible()
    expect(screen.queryByText(/limited retry diagnostics|below the supported minimum/)).not.toBeInTheDocument()
  })
  it.each([undefined, '', 'invalid', '0.17.3-dev'])('shows unknown version honestly: %s', version => {
    render(<ResticVersion version={version} />)
    expect(screen.getByText('Restic version unknown.')).toBeVisible()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
