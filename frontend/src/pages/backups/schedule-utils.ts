import type { BackupSchedule } from '@/lib/backup-api'

export const manualSchedule: BackupSchedule = { frequency: 'manual' }

export function scheduleDescription(schedule: BackupSchedule) {
  if (schedule.frequency === 'manual') return 'Manual only'
  if (schedule.frequency === 'interval') return `Every ${schedule.interval_hours ?? 1} hours (UTC)`
  return `${schedule.frequency === 'weekly' ? ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'][schedule.weekday ?? 0] : 'Daily'} at ${schedule.time || '02:00'} · ${schedule.timezone || 'host timezone'}`
}
