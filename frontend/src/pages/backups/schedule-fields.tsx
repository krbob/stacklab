import { useEffect, useState } from 'react'
import { previewBackupSchedule, type BackupSchedule } from '@/lib/backup-api'
import { BackupDate, Field, inputClass } from './backup-primitives'

export function ScheduleFields({
  label,
  value,
  onChange,
}: {
  label: string
  value: BackupSchedule
  onChange: (value: BackupSchedule) => void
}) {
  const [preview, setPreview] = useState<{ next_occurrences: string[]; timezone: string } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const serialized = JSON.stringify(value)
  useEffect(() => {
    let alive = true
    const controller = new AbortController()
    setPreview(null)
    setError(null)
    if (JSON.parse(serialized).frequency === 'manual') return
    const timer = window.setTimeout(() => {
      setLoading(true)
      setError(null)
      void previewBackupSchedule(JSON.parse(serialized) as BackupSchedule, controller.signal)
        .then((result) => {
          if (alive) setPreview(result)
        })
        .catch((cause) => {
          if (alive) setError(cause instanceof Error ? cause.message : 'Unable to preview schedule')
        })
        .finally(() => {
          if (alive) setLoading(false)
        })
    }, 350)
    return () => {
      alive = false
      controller.abort()
      window.clearTimeout(timer)
    }
  }, [serialized])
  return (
    <div className="space-y-2">
      <p className="text-xs text-[var(--muted)]">
        New or changed schedules start at the next future occurrence. Existing schedules catch up once after missed
        runs.
      </p>
      <div className="grid gap-3 sm:grid-cols-3">
        <Field label={label}>
          <select
            className={inputClass}
            value={value.frequency}
            onChange={(event) =>
              onChange({
                ...value,
                frequency: event.target.value as BackupSchedule['frequency'],
                time: value.time || '02:00',
                interval_hours: value.interval_hours || 6,
              })
            }
          >
            <option value="manual">Manual only</option>
            <option value="interval">Every few hours</option>
            <option value="daily">Daily</option>
            <option value="weekly">Weekly</option>
          </select>
        </Field>
        {value.frequency === 'interval' && (
          <Field label={`Interval hours (${label})`} hint="Fixed UTC cadence, unaffected by daylight saving.">
            <input
              className={inputClass}
              type="number"
              min={1}
              max={168}
              required
              value={value.interval_hours ?? 6}
              onChange={(event) => onChange({ ...value, interval_hours: Number(event.target.value) })}
            />
          </Field>
        )}
        {['daily', 'weekly'].includes(value.frequency) && (
          <>
            <Field label={`Time (${label})`}>
              <input
                className={inputClass}
                type="time"
                required
                value={value.time || '02:00'}
                onChange={(event) => onChange({ ...value, time: event.target.value })}
              />
            </Field>
            <Field label={`Timezone (${label})`} hint="Leave empty for the host timezone.">
              <input
                className={inputClass}
                value={value.timezone ?? ''}
                placeholder="Europe/Warsaw"
                onChange={(event) => onChange({ ...value, timezone: event.target.value })}
              />
            </Field>
          </>
        )}
        {value.frequency === 'weekly' && (
          <Field label={`Weekday (${label})`}>
            <select
              className={inputClass}
              value={value.weekday ?? 0}
              onChange={(event) => onChange({ ...value, weekday: Number(event.target.value) })}
            >
              {['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'].map((day, index) => (
                <option key={day} value={index}>
                  {day}
                </option>
              ))}
            </select>
          </Field>
        )}
      </div>
      {value.frequency !== 'manual' && (
        <div aria-live="polite" className="text-xs leading-5 text-[var(--muted)]">
          {loading ? (
            'Previewing schedule…'
          ) : error ? (
            <span className="text-[var(--warning)]">{error}</span>
          ) : (
            preview && (
              <>
                <span>Next occurrences ({preview.timezone}): </span>
                {preview.next_occurrences.map((date, index) => (
                  <span key={date}>
                    {index > 0 && ' · '}
                    <BackupDate value={date} />
                  </span>
                ))}
              </>
            )
          )}
        </div>
      )}
    </div>
  )
}
