export function PercentBar({ value, color, label }: { value: number; color: string; label: string }) {
  const normalized = Math.min(Math.max(value, 0), 100)
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Number(normalized.toFixed(1))}
      className="h-2 w-full rounded-full bg-[rgba(255,255,255,0.06)]"
    >
      <div className={`h-2 rounded-full ${color}`} style={{ width: `${normalized}%` }} aria-hidden="true" />
    </div>
  )
}
