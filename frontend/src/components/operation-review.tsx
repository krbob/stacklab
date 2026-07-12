export interface OperationReviewModel {
  target: string
  scope: readonly string[]
  impact: readonly string[]
  snapshot: string
  recovery: string
}

export function OperationReview({ review }: { review: OperationReviewModel }) {
  return (
    <section
      aria-label="Review operation"
      className="mt-4 rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] p-3"
    >
      <h3 className="text-xs font-semibold uppercase tracking-wide text-[var(--text)]">
        Review operation
      </h3>
      <dl className="mt-3 grid gap-3 text-xs sm:grid-cols-[7rem_minmax(0,1fr)]">
        <ReviewField label="Target" value={review.target} />
        <ReviewList label="Scope" items={review.scope} />
        <ReviewList label="Impact" items={review.impact} />
        <ReviewField label="Snapshot" value={review.snapshot} />
        <ReviewField label="Recovery" value={review.recovery} />
      </dl>
    </section>
  )
}

function ReviewField({ label, value }: { label: string; value: string }) {
  return (
    <>
      <dt className="font-medium text-[var(--muted)]">{label}</dt>
      <dd className="break-words text-[var(--text)]">{value}</dd>
    </>
  )
}

function ReviewList({ label, items }: { label: string; items: readonly string[] }) {
  return (
    <>
      <dt className="font-medium text-[var(--muted)]">{label}</dt>
      <dd>
        <ul className="space-y-1 text-[var(--text)]">
          {items.map((item) => (
            <li key={item} className="break-words before:mr-1.5 before:text-[var(--muted)] before:content-['•']">
              {item}
            </li>
          ))}
        </ul>
      </dd>
    </>
  )
}
