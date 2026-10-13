import { useState } from 'react'
import type { BackupDestination, BackupPlan, BackupRun } from '@/lib/backup-api'
import { BackupDate, BackupPanel, BackupSize, BackupStatus, buttonClass, Field, inputClass } from './backup-primitives'

const pageSize = 10
const duration = (seconds?: number) =>
  seconds === undefined
    ? 'Not measured'
    : seconds < 60
      ? `${seconds.toFixed(1)} s`
      : `${Math.floor(seconds / 60)} min ${Math.round(seconds % 60)} s`

export function RunHistory({
  runs,
  plans,
  destinations,
  clock,
  pending,
  onJob,
  onRetry,
}: {
  runs: BackupRun[]
  plans: BackupPlan[]
  destinations: BackupDestination[]
  clock: number
  pending: boolean
  onJob: (id: string) => void
  onRetry: (run: BackupRun, ids: string[]) => void
}) {
  const [filters, setFilters] = useState({ query: '', plan: '', state: '', destination: '', from: '', to: '' })
  const [page, setPage] = useState(0)
  const change = (key: keyof typeof filters, value: string) => {
    setFilters((previous) => ({ ...previous, [key]: value }))
    setPage(0)
  }
  const filtered = runs.filter(
    (run) =>
      (!filters.plan || run.plan_id === filters.plan) &&
      (!filters.state || run.state === filters.state) &&
      (!filters.destination || run.copies.some((copy) => copy.destination_id === filters.destination)) &&
      (!filters.from || run.created_at.slice(0, 10) >= filters.from) &&
      (!filters.to || run.created_at.slice(0, 10) <= filters.to) &&
      (!filters.query ||
        [
          run.plan_name,
          run.id,
          run.error,
          ...run.copies.flatMap((copy) => [
            copy.error,
            copy.snapshot_id,
            destinations.find((item) => item.id === copy.destination_id)?.name,
          ]),
        ]
          .join(' ')
          .toLowerCase()
          .includes(filters.query.toLowerCase())),
  )
  const lastPage = Math.max(0, Math.ceil(filtered.length / pageSize) - 1)
  const currentPage = Math.min(page, lastPage)
  const destinationName = (id: string) => destinations.find((item) => item.id === id)?.name ?? id
  return (
    <BackupPanel title="Backup history">
      <div className="mb-4 grid gap-3 sm:grid-cols-3">
        <Field label="Search history">
          <input
            className={inputClass}
            type="search"
            value={filters.query}
            onChange={(event) => change('query', event.target.value)}
            placeholder="Plan, destination, snapshot or error"
          />
        </Field>
        <Field label="Filter by plan">
          <select className={inputClass} value={filters.plan} onChange={(event) => change('plan', event.target.value)}>
            <option value="">All plans</option>
            {plans.map((plan) => (
              <option key={plan.id} value={plan.id}>
                {plan.name}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Filter by result">
          <select
            className={inputClass}
            value={filters.state}
            onChange={(event) => change('state', event.target.value)}
          >
            <option value="">All results</option>
            {['queued', 'running', 'succeeded', 'failed', 'cancelled', 'interrupted'].map((state) => (
              <option key={state}>{state}</option>
            ))}
          </select>
        </Field>
        <Field label="Filter by destination">
          <select
            className={inputClass}
            value={filters.destination}
            onChange={(event) => change('destination', event.target.value)}
          >
            <option value="">All destinations</option>
            {destinations.map((destination) => (
              <option key={destination.id} value={destination.id}>
                {destination.name}
              </option>
            ))}
          </select>
        </Field>
        <Field label="From date (UTC)">
          <input
            type="date"
            className={inputClass}
            value={filters.from}
            onChange={(event) => change('from', event.target.value)}
          />
        </Field>
        <Field label="Through date (UTC)">
          <input
            type="date"
            className={inputClass}
            value={filters.to}
            onChange={(event) => change('to', event.target.value)}
          />
        </Field>
      </div>
      <p className="mb-4 text-xs text-[var(--muted)]" aria-live="polite">
        {filtered.length} matching runs in retained history. The newest 200 expired runs and all active or retryable
        runs are retained.
      </p>
      {filtered.length === 0 && (
        <p className="text-sm text-[var(--muted)]">
          {runs.length
            ? 'No runs match these filters.'
            : 'No runs yet. Repository snapshots remain available in Recovery.'}
        </p>
      )}
      <div className="space-y-3">
        {filtered.slice(currentPage * pageSize, (currentPage + 1) * pageSize).map((run) => {
          const expired = new Date(run.expires_at).getTime() <= clock
          const failed = run.copies.filter((copy) => copy.state !== 'succeeded')
          const active = ['running', 'queued'].includes(run.state)
          return (
            <article key={run.id} className="space-y-3 rounded-md border border-[var(--panel-border)] p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="font-medium text-[var(--text)]">{run.plan_name}</h3>
                <BackupStatus value={run.state} />
              </div>
              <p className="text-xs text-[var(--muted)]">
                Created <BackupDate value={run.created_at} /> · Capture: {run.capture_state}
                {run.captured_at && (
                  <>
                    {' '}
                    · Captured <BackupDate value={run.captured_at} />
                  </>
                )}
              </p>
              <ol aria-label="Backup stages" className="flex flex-wrap gap-2 text-xs">
                {['capture', 'upload', 'verify', 'retention'].map((stage, index) => (
                  <li
                    key={stage}
                    className={`rounded-md border px-2 py-1 ${run.phase === stage && active ? 'border-[var(--accent)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]'}`}
                    aria-current={run.phase === stage && active ? 'step' : undefined}
                  >
                    {index + 1}. {stage === 'verify' ? 'Verification' : stage[0].toUpperCase() + stage.slice(1)}
                    {run.phase === stage && active ? ' · in progress' : ''}
                  </li>
                ))}
              </ol>
              <p className="text-xs text-[var(--muted)]">
                Capture time: {duration(run.capture_duration_seconds)} · Service downtime:{' '}
                {duration(run.manifest?.service_downtime_seconds)} · Total: {duration(run.duration_seconds)}
              </p>
              {run.error && <p className="text-sm text-[var(--danger)]">{run.error}</p>}
              <div className="space-y-2">
                {run.copies.map((copy) => (
                  <div key={copy.destination_id} className="rounded-md bg-[var(--bg)] p-3">
                    <div className="flex flex-wrap justify-between gap-2 text-sm text-[var(--text)]">
                      <span>{destinationName(copy.destination_id)}</span>
                      <BackupStatus value={copy.state} />
                    </div>
                    <p className="mt-1 text-xs text-[var(--muted)]">
                      Verification: {copy.verification.replaceAll('_', ' ')} · Retention: {copy.retention} · Uploaded{' '}
                      <BackupDate value={copy.uploaded_at} />
                    </p>
                    {(copy.bytes_processed !== undefined || copy.files_processed !== undefined) && (
                      <p className="mt-1 text-xs text-[var(--muted)]">
                        Processed <BackupSize value={copy.bytes_processed} />
                        {copy.bytes_total !== undefined && (
                          <>
                            {' '}
                            / <BackupSize value={copy.bytes_total} />
                          </>
                        )}{' '}
                        · {copy.files_processed ?? 0} repository items
                        {copy.files_total !== undefined && ` / ${copy.files_total}`} · Added{' '}
                        <BackupSize value={copy.bytes_added} /> · {duration(copy.duration_seconds)}
                      </p>
                    )}
                    {copy.bytes_total !== undefined && copy.bytes_total > 0 && copy.state === 'running' && (
                      <progress
                        aria-label={`Upload progress to ${destinationName(copy.destination_id)}`}
                        className="mt-2 w-full"
                        max={copy.bytes_total}
                        value={copy.bytes_processed ?? 0}
                      />
                    )}
                    {copy.snapshot_id && (
                      <p className="mt-1 break-all font-mono text-xs text-[var(--muted)]">{copy.snapshot_id}</p>
                    )}
                    {copy.error && <p className="mt-1 text-sm text-[var(--danger)]">{copy.error}</p>}
                  </div>
                ))}
              </div>
              {run.manifest && (
                <details>
                  <summary className="cursor-pointer text-sm text-[var(--muted)]">
                    Recovery manifest and prerequisites
                  </summary>
                  <div className="mt-2 space-y-2 text-xs text-[var(--muted)]">
                    <p>
                      Manifest v{run.manifest.version} · {run.manifest.consistency} · Stacklab{' '}
                      {run.manifest.stacklab_version}
                    </p>
                    {run.manifest.sources.map((item) => (
                      <p key={item.saved_path} className="break-all">
                        {item.source.service || item.source.kind}: {item.original_path} → {item.saved_path}
                      </p>
                    ))}
                    {run.manifest.sources.flatMap((item) =>
                      (item.consumers ?? []).map((consumer) => (
                        <p key={`${item.saved_path}:${consumer.container_id}`} className="break-all">
                          {item.source.mount || item.source.kind} consumer: {consumer.stack_id || 'external'}/
                          {consumer.service || consumer.container_id} · {consumer.read_only ? 'read only' : 'writer'} ·{' '}
                          {consumer.running ? 'running at capture' : 'stopped at capture'}
                        </p>
                      )),
                    )}
                    {run.manifest.images?.map((image) => (
                      <p key={image} className="break-all">
                        Required image: {image}
                      </p>
                    ))}
                    {run.manifest.warnings?.map((warning) => (
                      <p key={warning} className="text-[var(--warning)]">
                        {warning}
                      </p>
                    ))}
                  </div>
                </details>
              )}
              <div className="flex flex-wrap gap-2">
                <button className={buttonClass} onClick={() => onJob(run.job_id)}>
                  Job details
                </button>
                {failed.length > 0 && !active && (
                  <button
                    className={buttonClass}
                    disabled={pending || expired || run.capture_state !== 'succeeded'}
                    onClick={() =>
                      onRetry(
                        run,
                        failed.map((copy) => copy.destination_id),
                      )
                    }
                  >
                    Retry failed copies
                  </button>
                )}
              </div>
              {failed.length > 0 && !active && (
                <p className="text-xs text-[var(--muted)]">
                  {expired || run.capture_state !== 'succeeded' ? (
                    'A new capture is required; start the plan again.'
                  ) : (
                    <>
                      Prepared data available for retries until <BackupDate value={run.expires_at} />. Successful copies
                      are preserved.
                    </>
                  )}
                </p>
              )}
            </article>
          )
        })}
      </div>
      {filtered.length > pageSize && (
        <nav aria-label="History pages" className="mt-4 flex items-center justify-between gap-2">
          <button className={buttonClass} disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}>
            Previous page
          </button>
          <span className="text-xs text-[var(--muted)]">
            Page {currentPage + 1} of {lastPage + 1}
          </span>
          <button className={buttonClass} disabled={currentPage === lastPage} onClick={() => setPage(currentPage + 1)}>
            Next page
          </button>
        </nav>
      )}
    </BackupPanel>
  )
}
