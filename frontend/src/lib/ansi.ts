// Container log lines often carry ANSI escape sequences (colour SGR codes,
// cursor moves) from apps that emit colour regardless of a TTY. The log viewer
// renders plain text, so the raw escapes show up as garbage (e.g. `[34m`).
// Strip them at ingestion so both the rendered line and the text filter operate
// on clean output. Colour is intentionally dropped, not rendered, to keep the
// log surface within the console's restrained palette.

// Canonical ansi-regex pattern (Chalk), built from escapes so no literal
// control bytes live in the source. Matches CSI (e.g. `ESC[0m`) and OSC
// (`ESC]...BEL`) sequences.
const ANSI_PATTERN =
  '[\\u001B\\u009B][[\\]()#;?]*(?:(?:(?:(?:;[-a-zA-Z\\d/#&.:=?%@~_]+)*|[a-zA-Z\\d]+(?:;[-a-zA-Z\\d/#&.:=?%@~_]*)*)?\\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PR-TZcf-nq-uy=><~]))'

export function stripAnsi(input: string): string {
  return input.replace(new RegExp(ANSI_PATTERN, 'g'), '')
}
