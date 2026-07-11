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
  const controlClass = 'rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-3 py-2 text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]'

  return (
    <div className="mt-4 rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.015)] p-3">
      <div className="grid gap-3 md:grid-cols-[minmax(12rem,1fr)_12rem_10rem_10rem_auto] md:items-end">
        <label className="flex min-w-0 flex-col gap-1 text-xs text-[var(--muted)]">
          Search
          <input
            data-testid="audit-filter"
            type="search"
            value={filters.search}
            maxLength={200}
            onChange={(event) => onChange({ search: event.target.value })}
            placeholder="Action or stack ID…"
            className={`${controlClass} min-w-0 font-mono`}
          />
        </label>

        <label className="flex flex-col gap-1 text-xs text-[var(--muted)]">
          Result
          <select
            aria-label="Audit result"
            value={filters.result}
            onChange={(event) => onChange({ result: event.target.value as AuditResultFilter })}
            className={controlClass}
          >
            {resultOptions.map((option) => (
              <option key={option.value} value={option.value}>{option.label}</option>
            ))}
          </select>
        </label>

        <label className="flex flex-col gap-1 text-xs text-[var(--muted)]">
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

        <label className="flex flex-col gap-1 text-xs text-[var(--muted)]">
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
          className="rounded-md border border-[var(--panel-border)] px-3 py-2 text-xs text-[var(--muted)] transition hover:text-[var(--text)] disabled:cursor-not-allowed disabled:opacity-40"
        >
          Clear filters
        </button>
      </div>

      {rangeError && <p className="mt-2 text-xs text-[var(--danger)]" role="alert">{rangeError}</p>}
    </div>
  )
}
