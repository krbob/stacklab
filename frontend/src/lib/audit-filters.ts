import type { AuditQueryParams } from '@/lib/api-client'

export type AuditResultFilter = 'all' | 'succeeded' | 'failed' | 'cancelled' | 'timed_out'

export interface AuditFilterValues {
  search: string
  result: AuditResultFilter
  fromDate: string
  toDate: string
}

export type AuditFilterPatch = Partial<AuditFilterValues>

const resultFilters = new Set<AuditResultFilter>(['all', 'succeeded', 'failed', 'cancelled', 'timed_out'])
const localDatePattern = /^(\d{4})-(\d{2})-(\d{2})$/

export function readAuditFilters(searchParams: URLSearchParams): AuditFilterValues {
  const rawResult = searchParams.get('result') ?? ''
  return {
    search: searchParams.get('q') ?? '',
    result: resultFilters.has(rawResult as AuditResultFilter) ? rawResult as AuditResultFilter : 'all',
    fromDate: normalizeLocalDate(searchParams.get('from')),
    toDate: normalizeLocalDate(searchParams.get('to')),
  }
}

export function writeAuditFilters(searchParams: URLSearchParams, patch: AuditFilterPatch): URLSearchParams {
  const next = new URLSearchParams(searchParams)
  if (patch.search !== undefined) setOptional(next, 'q', patch.search)
  if (patch.result !== undefined) setOptional(next, 'result', patch.result === 'all' ? '' : patch.result)
  if (patch.fromDate !== undefined) setOptional(next, 'from', normalizeLocalDate(patch.fromDate))
  if (patch.toDate !== undefined) setOptional(next, 'to', normalizeLocalDate(patch.toDate))
  return next
}

export function clearAuditFilters(searchParams: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(searchParams)
  for (const key of ['q', 'result', 'from', 'to']) next.delete(key)
  return next
}

export function validateAuditDateRange(filters: AuditFilterValues): string | null {
  if (filters.fromDate && filters.toDate && filters.fromDate > filters.toDate) {
    return 'The start date must not be after the end date.'
  }
  return null
}

export function toAuditQuery(filters: AuditFilterValues): AuditQueryParams {
  const query: AuditQueryParams = {}
  const search = filters.search.trim()
  if (search) query.q = search
  if (filters.result !== 'all') query.result = filters.result
  if (filters.fromDate) query.from = localDateBoundary(filters.fromDate).toISOString()
  if (filters.toDate) {
    const exclusiveEnd = localDateBoundary(filters.toDate)
    exclusiveEnd.setDate(exclusiveEnd.getDate() + 1)
    query.to = exclusiveEnd.toISOString()
  }
  return query
}

export function hasAuditFilters(filters: AuditFilterValues): boolean {
  return Boolean(filters.search.trim() || filters.result !== 'all' || filters.fromDate || filters.toDate)
}

function normalizeLocalDate(value: string | null): string {
  if (!value) return ''
  const match = localDatePattern.exec(value)
  if (!match) return ''
  const [, year, month, day] = match
  const parsed = new Date(Number(year), Number(month) - 1, Number(day))
  if (
    parsed.getFullYear() !== Number(year) ||
    parsed.getMonth() !== Number(month) - 1 ||
    parsed.getDate() !== Number(day)
  ) {
    return ''
  }
  return value
}

function localDateBoundary(value: string): Date {
  const match = localDatePattern.exec(value)
  if (!match) throw new Error(`Invalid local date: ${value}`)
  return new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]))
}

function setOptional(searchParams: URLSearchParams, key: string, value: string) {
  const normalized = value.trim()
  if (normalized) searchParams.set(key, normalized)
  else searchParams.delete(key)
}
