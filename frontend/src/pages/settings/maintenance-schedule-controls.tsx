import type { MaintenanceSchedulesResponse, ScheduleFrequency, ScheduleWeekday } from '@/lib/api-types'
import { cn } from '@/lib/cn'
import { ALL_WEEKDAYS, WEEKDAY_LABELS } from '@/pages/settings/maintenance-schedule-utils'

export function FrequencyToggle({ value, onChange }: { value: ScheduleFrequency; onChange: (v: ScheduleFrequency) => void }) {
  return (
    <div className="flex gap-1">
      {(['daily', 'weekly'] as const).map((f) => (
        <button key={f} onClick={() => onChange(f)} className={cn('rounded-md border px-2.5 py-1 text-xs transition', value === f ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}>
          {f === 'daily' ? 'Daily' : 'Weekly'}
        </button>
      ))}
    </div>
  )
}

export function WeekdayPicker({ value, onChange }: { value: ScheduleWeekday[]; onChange: (v: ScheduleWeekday[]) => void }) {
  return (
    <div className="flex flex-wrap gap-1">
      {ALL_WEEKDAYS.map((d) => (
        <button
          key={d}
          onClick={() => onChange(value.includes(d) ? value.filter((w) => w !== d) : [...value, d])}
          className={cn('rounded-md border px-2 py-1 text-xs transition', value.includes(d) ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'border-[var(--panel-border)] text-[var(--muted)]')}
        >
          {WEEKDAY_LABELS[d]}
        </button>
      ))}
    </div>
  )
}

export function ScheduleStatusFooter({ status, onOpenJob }: { status?: MaintenanceSchedulesResponse['update']['status']; onOpenJob: (id: string) => void }) {
  if (!status) return null

  const resultColors: Record<string, string> = { succeeded: 'text-[var(--ok)]', failed: 'text-[var(--danger)]', skipped: 'text-[var(--warning)]', running: 'text-[var(--run)]' }

  return (
    <div className="border-t border-[var(--panel-border)] pt-2 font-mono text-xs text-[var(--muted)]">
      {status.next_run_at && <div>Next: {new Date(status.next_run_at).toLocaleString()}</div>}
      {status.last_result && (
        <div className="flex items-center gap-2">
          <span>Last: <span className={resultColors[status.last_result] ?? ''}>{status.last_result}</span></span>
          {status.last_scheduled_for && <span>{new Date(status.last_scheduled_for).toLocaleString()}</span>}
          {status.last_job_id && (
            <button onClick={() => onOpenJob(status.last_job_id!)} className="text-[var(--accent)] hover:underline">View job</button>
          )}
        </div>
      )}
      {status.last_message && <div className="text-[var(--warning)]">{status.last_message}</div>}
    </div>
  )
}
