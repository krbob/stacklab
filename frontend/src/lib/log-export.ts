import type { LogEntry } from '@/lib/ws-types'

export function serializeLogEntries(entries: LogEntry[]): string {
  if (entries.length === 0) return ''
  return `${entries.map((entry) => (
    `[${entry.timestamp}] [${entry.service_name}] [${entry.stream}] ${entry.line}`
  )).join('\n')}\n`
}

export function logExportFilename(stackID: string, now = new Date()): string {
  const timestamp = now.toISOString().replace(/[:.]/g, '-')
  return `${stackID}-logs-${timestamp}.log`
}

export function downloadLogFile(filename: string, content: string): void {
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.hidden = true
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}

export async function copyLogText(content: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(content)
    return
  }

  const textarea = document.createElement('textarea')
  textarea.value = content
  textarea.readOnly = true
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.append(textarea)
  textarea.select()
  const copied = document.execCommand('copy')
  textarea.remove()
  if (!copied) throw new Error('Clipboard copy is unavailable')
}
