import { describe, it, expect } from 'vitest'
import { stripAnsi } from './ansi'

const ESC = String.fromCharCode(27) // 

describe('stripAnsi', () => {
  it('removes SGR colour codes', () => {
    expect(stripAnsi(`${ESC}[34mINFO${ESC}[0m hello`)).toBe('INFO hello')
    expect(stripAnsi(`${ESC}[1;32mok${ESC}[0m`)).toBe('ok')
  })

  it('cleans a real container log line', () => {
    const raw = `2026-07-06T03:00:21.288+0200 ${ESC}[34mINFO${ESC}[0m scheduled task`
    expect(stripAnsi(raw)).toBe('2026-07-06T03:00:21.288+0200 INFO scheduled task')
  })

  it('leaves plain text untouched', () => {
    expect(stripAnsi('no escapes here 123')).toBe('no escapes here 123')
  })
})
