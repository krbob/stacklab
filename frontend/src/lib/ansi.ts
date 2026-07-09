// Container log lines carry ANSI escape sequences (colour SGR codes, cursor
// moves) from apps that emit colour. The log viewer preserves colour — it
// conveys real signal (log levels: red errors, green ok, yellow warnings) — by
// parsing SGR runs into styled spans. Non-SGR escapes (cursor moves, OSC) are
// dropped. `stripAnsi` yields the plain text used for filtering.
//
// All escape bytes are written as `\\u001B` string escapes (never literal
// control bytes) so the source stays clean.

// Canonical ansi-regex pattern (Chalk). Matches CSI (e.g. `ESC[0m`) and OSC
// (`ESC]...BEL`) sequences.
const ANSI_PATTERN =
  '[\\u001B\\u009B][[\\]()#;?]*(?:(?:(?:(?:;[-a-zA-Z\\d/#&.:=?%@~_]+)*|[a-zA-Z\\d]+(?:;[-a-zA-Z\\d/#&.:=?%@~_]*)*)?\\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PR-TZcf-nq-uy=><~]))'

// Foreground SGR sequence (`ESC[ … m`), capturing its parameter list.
const SGR_PATTERN = '^\\u001B\\[([0-9;]*)m$'

export function stripAnsi(input: string): string {
  return input.replace(new RegExp(ANSI_PATTERN, 'g'), '')
}

export interface AnsiSpan {
  text: string
  color?: string
  bold?: boolean
  dim?: boolean
}

// The 16 base ANSI colours mapped to values that read well on the near-black
// console background. Status hues reuse the app palette (red→danger, green→ok,
// yellow→accent) so log levels feel native; the rest are tuned to match.
const ANSI_16: Record<number, string> = {
  0: '#6E6757', 1: '#F26D5B', 2: '#55D47F', 3: '#F5A524',
  4: '#6FA8DC', 5: '#C58AF0', 6: '#56C7D6', 7: '#C9C2B2',
  8: '#8A8372', 9: '#FF8A7A', 10: '#7BE39B', 11: '#FFC15A',
  12: '#93C2F0', 13: '#D6A8F5', 14: '#7FDCE8', 15: '#F2EADB',
}

function color256(n: number): string {
  if (n < 16) return ANSI_16[n] ?? '#F2EADB'
  if (n <= 231) {
    const c = n - 16
    const to = (v: number) => (v === 0 ? 0 : 55 + v * 40)
    return `rgb(${to(Math.floor(c / 36))}, ${to(Math.floor((c % 36) / 6))}, ${to(c % 6)})`
  }
  const v = 8 + (n - 232) * 10
  return `rgb(${v}, ${v}, ${v})`
}

interface SgrState {
  color?: string
  bold: boolean
  dim: boolean
}

function applySgr(params: string, state: SgrState): void {
  const codes = params === '' ? [0] : params.split(';').map((p) => Number.parseInt(p, 10))
  for (let i = 0; i < codes.length; i++) {
    const code = codes[i]
    if (code === 0) {
      state.color = undefined
      state.bold = false
      state.dim = false
    } else if (code === 1) state.bold = true
    else if (code === 2) state.dim = true
    else if (code === 22) { state.bold = false; state.dim = false }
    else if (code === 39) state.color = undefined
    else if ((code >= 30 && code <= 37) || (code >= 90 && code <= 97)) {
      state.color = ANSI_16[code >= 90 ? code - 90 + 8 : code - 30]
    } else if (code === 38) {
      // Extended foreground: 38;5;n (256) or 38;2;r;g;b (truecolor).
      if (codes[i + 1] === 5) { state.color = color256(codes[i + 2] ?? 0); i += 2 }
      else if (codes[i + 1] === 2) { state.color = `rgb(${codes[i + 2] ?? 0}, ${codes[i + 3] ?? 0}, ${codes[i + 4] ?? 0})`; i += 4 }
    } else if (code === 48) {
      // Background — ignored, but consume its params so they aren't misread.
      if (codes[i + 1] === 5) i += 2
      else if (codes[i + 1] === 2) i += 4
    }
    // Other codes (background 40-47, blink, etc.) are ignored.
  }
}

function parseAnsiWithState(input: string, state: SgrState): AnsiSpan[] {
  const re = new RegExp(ANSI_PATTERN, 'g')
  const sgrRe = new RegExp(SGR_PATTERN)
  const spans: AnsiSpan[] = []
  let last = 0
  const push = (text: string) => {
    if (!text) return
    const span: AnsiSpan = { text }
    if (state.color) span.color = state.color
    if (state.bold) span.bold = true
    if (state.dim) span.dim = true
    spans.push(span)
  }
  let match: RegExpExecArray | null
  while ((match = re.exec(input)) !== null) {
    push(input.slice(last, match.index))
    last = match.index + match[0].length
    const sgr = sgrRe.exec(match[0])
    if (sgr) applySgr(sgr[1], state)
  }
  push(input.slice(last))
  return spans.length > 0 ? spans : [{ text: stripAnsi(input) }]
}

// Parse a log line into styled runs. Always returns at least one span.
export function parseAnsi(input: string): AnsiSpan[] {
  return parseAnsiWithState(input, { bold: false, dim: false })
}

export function createAnsiParser() {
  const state: SgrState = { bold: false, dim: false }
  return {
    parse(input: string): AnsiSpan[] {
      return parseAnsiWithState(input, state)
    },
    reset(): void {
      state.color = undefined
      state.bold = false
      state.dim = false
    },
  }
}
