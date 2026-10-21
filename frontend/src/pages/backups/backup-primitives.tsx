import { cloneElement, isValidElement, useId, type ReactNode } from 'react'
import { ChevronDown } from 'lucide-react'
import { cn } from '@/lib/cn'
import { formatBackupDate } from './backup-formatters'

export const inputClass =
  'min-h-10 w-full rounded-md border border-[var(--panel-border)] bg-[var(--bg)] px-3 py-2 text-sm text-[var(--text)] outline-none focus:border-[var(--accent)] disabled:opacity-50'
export const buttonClass =
  'rounded-md border border-[var(--panel-border)] px-3 py-2 text-sm text-[var(--text)] transition hover:border-[var(--accent)] disabled:cursor-not-allowed disabled:opacity-50'
export const primaryClass =
  'rounded-md border border-[var(--accent)]/40 bg-[var(--accent)]/10 px-3 py-2 text-sm text-[var(--text)] transition hover:border-[var(--accent)] disabled:cursor-not-allowed disabled:opacity-50'

export function BackupPanel({ title, children, actions }: { title: string; children: ReactNode; actions?: ReactNode }) {
  return (
    <section className="min-w-0 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-4 [overflow-wrap:anywhere]">
      <div className="mb-4 flex flex-col items-start gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
        <h2 className="text-base font-semibold text-[var(--text)]">{title}</h2>
        {actions && <div className="flex max-w-full flex-wrap gap-2">{actions}</div>}
      </div>
      {children}
    </section>
  )
}

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  const id = useId()
  const isSelect = isValidElement(children) && children.type === 'select'
  const control = isValidElement<{ id?: string; 'aria-describedby'?: string; className?: string }>(children)
    ? cloneElement(children, {
        id,
        'aria-describedby': hint ? `${id}-hint` : undefined,
        ...(isSelect ? { className: cn(children.props.className, 'appearance-none pr-9') } : {}),
      })
    : children
  return (
    <div className="flex min-w-0 flex-col gap-1.5 text-sm text-[var(--text)]">
      <label htmlFor={id}>{label}</label>
      {isSelect ? (
        <span className="relative min-w-0">
          {control}
          <ChevronDown aria-hidden="true" className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2" />
        </span>
      ) : control}
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
        'inline-flex shrink-0 items-center whitespace-nowrap rounded border px-2 py-0.5 text-xs',
        ['succeeded', 'covered', 'healthy', 'current', 'ready', 'available', 'verified', 'complete', 'sampled'].includes(value)
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
                'incomplete',
                'invalid',
                'unsupported',
                'legacy',
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

export function RecoveryEvidence({ status, classifiedAt }: { status?: string; classifiedAt?: string }) {
  return <div className="space-y-1 text-xs">
    <p className="text-[var(--muted)]">Snapshot evidence at retrieval: <BackupStatus value={status || 'unknown'} />{classifiedAt && <> · Classified <BackupDate value={classifiedAt} /></>}</p>
    {(!status || status === 'unknown') ? <p className="text-[var(--warning)]">Snapshot completeness was not recorded. Applying data validates each selected source; an unclassified snapshot is not proof of a complete recovery set.</p> : status !== 'complete' && <p className="text-[var(--warning)]">This restore came from a snapshot classified as {status}. Applying data validates each selected source; intact sources may still be recovered even if other sources are missing.</p>}
  </div>
}
