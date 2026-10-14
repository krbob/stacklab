export function formatBackupDate(value: string, timeZone?: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'Unknown time'
  try {
    return date.toLocaleString(undefined, { timeZone, timeZoneName: 'short' })
  } catch {
    return `${date.toLocaleString(undefined, { timeZone: 'UTC', timeZoneName: 'short' })} (requested zone: ${timeZone})`
  }
}

export function backupDuration(seconds?: number) {
  if (seconds === undefined) return 'Not measured'
  const rounded = Math.round(seconds)
  return rounded < 60 ? `${Math.min(seconds, 59.9).toFixed(1)} s` : `${Math.floor(rounded / 60)} min ${rounded % 60} s`
}
