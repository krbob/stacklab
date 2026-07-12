import type { ScheduleFrequency, ScheduleWeekday } from '@/lib/api-types'

export const ALL_WEEKDAYS: ScheduleWeekday[] = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun']
export const WEEKDAY_LABELS: Record<ScheduleWeekday, string> = { mon: 'Mon', tue: 'Tue', wed: 'Wed', thu: 'Thu', fri: 'Fri', sat: 'Sat', sun: 'Sun' }

export function describeSchedule(frequency: ScheduleFrequency, time: string, weekdays: ScheduleWeekday[]): string {
  if (frequency === 'daily') return `daily at ${time}`
  const days = weekdays.length > 0 ? weekdays.map((day) => WEEKDAY_LABELS[day]).join(', ') : 'no selected days'
  return `weekly on ${days} at ${time}`
}

export function cleanedExcludedServices(excluded: Record<string, string[]>): Record<string, string[]> | undefined {
  const result: Record<string, string[]> = {}
  for (const [stackId, services] of Object.entries(excluded)) {
    const unique = Array.from(new Set(services.filter(Boolean))).sort()
    if (unique.length > 0) {
      result[stackId] = unique
    }
  }
  return Object.keys(result).length > 0 ? result : undefined
}

export function filteredExcludedServices(excluded: Record<string, string[]>, stackIds: string[]): Record<string, string[]> | undefined {
  const allowed = new Set(stackIds)
  const filtered: Record<string, string[]> = {}
  for (const [stackId, services] of Object.entries(excluded)) {
    if (allowed.has(stackId)) {
      filtered[stackId] = services
    }
  }
  return cleanedExcludedServices(filtered)
}

export function hasExcludedServices(excluded: Record<string, string[]>): boolean {
  return Object.values(excluded).some((services) => services.length > 0)
}
