import { describe, expect, it } from 'vitest'
import { backupDuration, formatBackupDate } from './backup-formatters'

describe('Backup display fallbacks', () => {
  it('shows UTC and the requested zone when the browser cannot format a host timezone', () => {
    const formatted = formatBackupDate('2026-10-14T09:00:00Z', '/usr/share/zoneinfo/Europe/Warsaw')
    expect(formatted).toContain('UTC')
    expect(formatted).toContain('requested zone: /usr/share/zoneinfo/Europe/Warsaw')
  })
  it('rounds duration across minute boundaries without impossible second values', () => {
    expect(backupDuration(59.99)).toBe('1 min 0 s')
    expect(backupDuration(119.8)).toBe('2 min 0 s')
    expect(backupDuration(1.24)).toBe('1.2 s')
  })
})
