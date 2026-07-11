import { describe, expect, it } from 'vitest'
import {
  clearAuditFilters,
  hasAuditFilters,
  readAuditFilters,
  toAuditQuery,
  validateAuditDateRange,
  writeAuditFilters,
} from './audit-filters'

describe('audit filters', () => {
  it('reads, writes, and clears URL state without dropping unrelated parameters', () => {
    const initial = new URLSearchParams('view=compact&q=pull&result=failed&from=2026-07-01&to=2026-07-02')
    expect(readAuditFilters(initial)).toEqual({
      search: 'pull',
      result: 'failed',
      fromDate: '2026-07-01',
      toDate: '2026-07-02',
    })

    const updated = writeAuditFilters(initial, { search: 'restart stack', result: 'all', toDate: '' })
    expect(updated.get('view')).toBe('compact')
    expect(updated.get('q')).toBe('restart stack')
    expect(updated.has('result')).toBe(false)
    expect(updated.has('to')).toBe(false)

    const cleared = clearAuditFilters(updated)
    expect(cleared.toString()).toBe('view=compact')
  })

  it('converts inclusive local calendar dates to RFC3339 request boundaries', () => {
    const query = toAuditQuery({
      search: '  deploy  ',
      result: 'succeeded',
      fromDate: '2026-07-01',
      toDate: '2026-07-02',
    })

    expect(query).toEqual({
      q: 'deploy',
      result: 'succeeded',
      from: new Date(2026, 6, 1).toISOString(),
      to: new Date(2026, 6, 3).toISOString(),
    })
  })

  it('normalizes invalid URL values and validates the selected range', () => {
    const filters = readAuditFilters(new URLSearchParams('result=unknown&from=2026-02-30&to=2026-02-01'))
    expect(filters.result).toBe('all')
    expect(filters.fromDate).toBe('')
    expect(filters.toDate).toBe('2026-02-01')
    expect(hasAuditFilters(filters)).toBe(true)

    expect(validateAuditDateRange({
      search: '',
      result: 'all',
      fromDate: '2026-07-03',
      toDate: '2026-07-02',
    })).toBe('The start date must not be after the end date.')
  })
})
