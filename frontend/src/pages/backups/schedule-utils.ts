import type { BackupSchedule } from '@/lib/backup-api'

export const manualSchedule: BackupSchedule = { frequency: 'manual' }

export function scheduleDescription(schedule: BackupSchedule) {
  if (schedule.frequency === 'manual') return 'Manual only'
  if (schedule.frequency === 'interval') {
    const hours = schedule.interval_hours
    return hours ? `Every ${hours} ${hours === 1 ? 'hour' : 'hours'} (UTC)` : 'Interval not configured'
  }
  return `${schedule.frequency === 'weekly' ? ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'][schedule.weekday ?? -1] || 'Unspecified day' : 'Daily'} at ${schedule.time || 'unspecified time'} · ${schedule.timezone || 'host timezone'}`
}
