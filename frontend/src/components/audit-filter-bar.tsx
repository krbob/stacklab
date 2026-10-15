import { ChevronDown } from 'lucide-react'
import type { AuditFilterPatch, AuditFilterValues, AuditResultFilter } from '@/lib/audit-filters'

interface AuditFilterBarProps {
  filters: AuditFilterValues
  hasActiveFilters: boolean
  rangeError: string | null
  onChange: (patch: AuditFilterPatch) => void
  onClear: () => void
}

const resultOptions: Array<{ value: AuditResultFilter; label: string }> = [
  { value: 'all', label: 'All results' },
  { value: 'succeeded', label: 'Succeeded' },
  { value: 'failed', label: 'Failed (incl. timed out)' },
  { value: 'cancelled', label: 'Cancelled' },
  { value: 'timed_out', label: 'Timed out' },
]

export function AuditFilterBar({ filters, hasActiveFilters, rangeError, onChange, onClear }: AuditFilterBarProps) {
  const controlClass = 'h-10 w-full min-w-0 rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 text-base leading-5 text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]'

  return (
    <div className="@container mt-4 rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.015)] p-3">
      <div className="grid items-end gap-3 @sm:grid-cols-2 @2xl:grid-cols-4 @5xl:grid-cols-[minmax(12rem,1fr)_14rem_11rem_11rem_auto]">
        <label className="flex min-w-0 flex-col gap-1 text-xs text-[var(--muted)] @sm:col-span-2 @5xl:col-span-1">
          Search
          <input
            data-testid="audit-filter"
            type="search"
            value={filters.search}
            maxLength={200}
            onChange={(event) => onChange({ search: event.target.value })}
            placeholder="Action or stack ID…"
            className={`${controlClass} font-mono`}
          />
        </label>

        <label className="flex min-w-0 flex-col gap-1 text-xs text-[var(--muted)] @sm:col-span-2 @5xl:col-span-1">
          Result
          <span className="relative">
            <select
              aria-label="Audit result"
              value={filters.result}
              onChange={(event) => onChange({ result: event.target.value as AuditResultFilter })}
              className={`${controlClass} appearance-none pr-9`}
            >
              {resultOptions.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
            <ChevronDown aria-hidden="true" className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2" />
          </span>
        </label>

        <label className="flex min-w-0 flex-col gap-1 text-xs text-[var(--muted)]">
          From
          <input
            aria-label="Audit from date"
            type="date"
            value={filters.fromDate}
            max={filters.toDate || undefined}
            onChange={(event) => onChange({ fromDate: event.target.value })}
            className={controlClass}
          />
        </label>

        <label className="flex min-w-0 flex-col gap-1 text-xs text-[var(--muted)]">
          Through
          <input
            aria-label="Audit through date"
            type="date"
            value={filters.toDate}
            min={filters.fromDate || undefined}
            onChange={(event) => onChange({ toDate: event.target.value })}
            className={controlClass}
          />
        </label>

        <button
          type="button"
          onClick={onClear}
          disabled={!hasActiveFilters}
          className="h-10 rounded-md border border-[var(--panel-border)] px-3 py-2 text-xs whitespace-nowrap text-[var(--muted)] transition hover:text-[var(--text)] disabled:cursor-not-allowed disabled:opacity-40 @sm:col-span-2 @2xl:justify-self-end @5xl:col-span-1"
        >
          Clear filters
        </button>
      </div>

      {rangeError && <p className="mt-2 text-xs text-[var(--danger)]" role="alert">{rangeError}</p>}
    </div>
  )
}
