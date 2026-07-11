import { afterEach, describe, expect, it, vi } from 'vitest'
import type { LogEntry } from '@/lib/ws-types'
import { copyLogText, downloadLogFile, logExportFilename, serializeLogEntries } from './log-export'

const entries: LogEntry[] = [
  {
    timestamp: '2026-07-12T08:30:00Z',
    service_name: 'api',
    container_id: 'container-api',
    stream: 'stdout',
    line: 'server started',
  },
  {
    timestamp: '2026-07-12T08:30:01Z',
    service_name: 'worker',
    container_id: 'container-worker',
    stream: 'stderr',
    line: 'retry scheduled',
  },
]

describe('log export', () => {
  afterEach(() => vi.restoreAllMocks())

  it('serializes visible entries with timestamp, service, and stream context', () => {
    expect(serializeLogEntries(entries)).toBe(
      '[2026-07-12T08:30:00Z] [api] [stdout] server started\n' +
      '[2026-07-12T08:30:01Z] [worker] [stderr] retry scheduled\n',
    )
    expect(serializeLogEntries([])).toBe('')
  })

  it('creates filesystem-safe timestamped filenames', () => {
    expect(logExportFilename('demo', new Date('2026-07-12T08:30:01.234Z')))
      .toBe('demo-logs-2026-07-12T08-30-01-234Z.log')
  })

  it('downloads content through a temporary object URL', () => {
    const createObjectURL = vi.fn(() => 'blob:stacklab-logs')
    const revokeObjectURL = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createObjectURL })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeObjectURL })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    downloadLogFile('demo.log', 'line\n')

    expect(createObjectURL).toHaveBeenCalledWith(expect.any(Blob))
    expect(click).toHaveBeenCalledTimes(1)
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:stacklab-logs')
  })

  it('uses the Clipboard API when available', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })

    await copyLogText('line\n')

    expect(writeText).toHaveBeenCalledWith('line\n')
  })
})
