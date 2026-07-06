import { describe, it, expect } from 'vitest'
import { stripAnsi, parseAnsi } from './ansi'

const ESC = String.fromCharCode(27)

describe('stripAnsi', () => {
  it('removes SGR colour codes', () => {
    expect(stripAnsi(`${ESC}[34mINFO${ESC}[0m hello`)).toBe('INFO hello')
    expect(stripAnsi(`${ESC}[1;32mok${ESC}[0m`)).toBe('ok')
  })

  it('leaves plain text untouched', () => {
    expect(stripAnsi('no escapes here 123')).toBe('no escapes here 123')
  })
})

describe('parseAnsi', () => {
  it('splits coloured runs and preserves plain text via concatenation', () => {
    const spans = parseAnsi(`${ESC}[34mINFO${ESC}[0m scheduled task`)
    expect(spans.map((s) => s.text).join('')).toBe('INFO scheduled task')
    expect(spans[0]).toMatchObject({ text: 'INFO', color: expect.any(String) })
    expect(spans[1]).toEqual({ text: ' scheduled task' })
  })

  it('resets style after ESC[0m', () => {
    const spans = parseAnsi(`${ESC}[31mERR${ESC}[0m ok`)
    expect(spans[0].color).toBeTruthy()
    expect(spans[1].color).toBeUndefined()
  })

  it('handles bold and 256-colour foreground', () => {
    const bold = parseAnsi(`${ESC}[1mbold${ESC}[22m`)
    expect(bold[0]).toMatchObject({ text: 'bold', bold: true })
    const ext = parseAnsi(`${ESC}[38;5;208mx`)
    expect(ext[0].color).toMatch(/^rgb\(/)
  })

  it('drops non-SGR escapes (cursor moves) without colouring', () => {
    const spans = parseAnsi(`a${ESC}[2Kb`)
    expect(spans.map((s) => s.text).join('')).toBe('ab')
    expect(spans.every((s) => s.color === undefined)).toBe(true)
  })

  it('returns a single plain span when there are no escapes', () => {
    expect(parseAnsi('plain line')).toEqual([{ text: 'plain line' }])
  })
})
