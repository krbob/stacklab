import { describe, expect, it } from 'vitest'
import {
  cleanedExcludedServices,
  describeSchedule,
  filteredExcludedServices,
  hasExcludedServices,
} from '@/pages/settings/maintenance-schedule-utils'

describe('maintenance schedule model', () => {
  it('describes daily and weekly schedules', () => {
    expect(describeSchedule('daily', '03:30', [])).toBe('daily at 03:30')
    expect(describeSchedule('weekly', '04:30', ['mon', 'sat'])).toBe('weekly on Mon, Sat at 04:30')
    expect(describeSchedule('weekly', '04:30', [])).toBe('weekly on no selected days at 04:30')
  })

  it('deduplicates, sorts, and removes empty service exclusions', () => {
    expect(cleanedExcludedServices({
      demo: ['worker', '', 'app', 'worker'],
      empty: [],
    })).toEqual({ demo: ['app', 'worker'] })
    expect(cleanedExcludedServices({ demo: ['', ''] })).toBeUndefined()
  })

  it('keeps exclusions only for selected stacks', () => {
    expect(filteredExcludedServices({
      alpha: ['web'],
      beta: ['worker'],
    }, ['beta'])).toEqual({ beta: ['worker'] })
    expect(filteredExcludedServices({ alpha: ['web'] }, ['beta'])).toBeUndefined()
  })

  it('detects whether any service is excluded', () => {
    expect(hasExcludedServices({ alpha: [], beta: ['worker'] })).toBe(true)
    expect(hasExcludedServices({ alpha: [], beta: [] })).toBe(false)
  })
})
