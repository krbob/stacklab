import { cloneElement, isValidElement, useId, type ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { formatBackupDate } from './backup-formatters'

export const inputClass =
  'w-full rounded-md border border-[var(--panel-border)] bg-[var(--bg)] px-3 py-2 text-sm text-[var(--text)] outline-none focus:border-[var(--accent)] disabled:opacity-50'
export const buttonClass =
  'rounded-md border border-[var(--panel-border)] px-3 py-2 text-sm text-[var(--text)] transition hover:border-[var(--accent)] disabled:cursor-not-allowed disabled:opacity-50'
export const primaryClass =
  'rounded-md border border-[var(--accent)]/40 bg-[var(--accent)]/10 px-3 py-2 text-sm text-[var(--text)] transition hover:border-[var(--accent)] disabled:cursor-not-allowed disabled:opacity-50'

export function BackupPanel({ title, children, actions }: { title: string; children: ReactNode; actions?: ReactNode }) {
  return (
    <section className="min-w-0 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-4">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-base font-semibold text-[var(--text)]">{title}</h2>
        {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
      </div>
      {children}
    </section>
  )
}

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  const id = useId()
  return (
    <div className="flex min-w-0 flex-col gap-1.5 text-sm text-[var(--text)]">
      <label htmlFor={id}>{label}</label>
      {isValidElement<{ id?: string; 'aria-describedby'?: string }>(children)
        ? cloneElement(children, { id, 'aria-describedby': hint ? `${id}-hint` : undefined })
        : children}
      {hint && (
        <span id={`${id}-hint`} className="text-xs leading-5 text-[var(--muted)]">
          {hint}
        </span>
      )}
    </div>
  )
}

export function BackupStatus({ value }: { value: string }) {
  return (
    <span
      className={cn(
        'rounded border px-2 py-0.5 text-xs',
        ['succeeded', 'covered', 'healthy', 'current', 'ready', 'available'].includes(value)
          ? 'border-[var(--ok)]/30 text-[var(--ok)]'
          : [
                'failed',
                'error',
                'interrupted',
                'partial',
                'source_changed',
                'unconfigured',
                'unprotected',
                'overdue',
                'outdated',
                'attention',
                'missing',
                'integrity_failed',
                'unavailable',
                'clock_skew',
                'unverified',
              ].includes(value)
            ? 'border-[var(--warning)]/30 text-[var(--warning)]'
            : 'border-[var(--panel-border)] text-[var(--muted)]',
      )}
    >
      {value.replaceAll('_', ' ')}
    </span>
  )
}

export function BackupError({ error }: { error: string | null }) {
  return error ? (
    <p
      role="alert"
      className="rounded-md border border-[var(--danger)]/30 bg-[var(--danger)]/5 p-3 text-sm text-[var(--danger)]"
    >
      {error}
    </p>
  ) : null
}

export function BackupDate({ value, timeZone }: { value?: string | null; timeZone?: string }) {
  if (!value) return <>Never</>
  return <time dateTime={value}>{formatBackupDate(value, timeZone)}</time>
}

export function BackupSize({ value }: { value?: number | null }) {
  if (value === undefined || value === null) return <>Not measured</>
  const unit = value > 0 ? Math.min(4, Math.floor(Math.log(value) / Math.log(1024))) : 0
  return (
    <>
      {(value / 1024 ** unit).toLocaleString(undefined, { maximumFractionDigits: 1 })}{' '}
      {['B', 'KiB', 'MiB', 'GiB', 'TiB'][unit]}
    </>
  )
}
