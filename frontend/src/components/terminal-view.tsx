import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

interface TerminalViewProps {
  onData: (data: string) => void
  onResize: (cols: number, rows: number) => void
  writeRef: React.MutableRefObject<((data: string) => void) | null>
}

export function TerminalView({ onData, onResize, writeRef }: TerminalViewProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)

  useEffect(() => {
    if (!containerRef.current) return

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'var(--font-mono)',
      theme: {
        background: 'rgba(0, 0, 0, 0.3)',
        foreground: '#e8f0f2',
        cursor: '#4fd1c5',
        selectionBackground: 'rgba(79, 209, 197, 0.3)',
      },
    })

    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(containerRef.current)

    // Initial fit
    requestAnimationFrame(() => {
      fitAddon.fit()
      onResize(term.cols, term.rows)
    })

    // Forward user input
    term.onData(onData)

    // Expose write function to parent
    writeRef.current = (data: string) => term.write(data)

    termRef.current = term

    // Resize observer
    const observer = new ResizeObserver(() => {
      fitAddon.fit()
      onResize(term.cols, term.rows)
    })
    observer.observe(containerRef.current)

    return () => {
      observer.disconnect()
      writeRef.current = null
      term.dispose()
      termRef.current = null
    }
    // These callbacks are stable refs, only run once
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div
      ref={containerRef}
      className="h-[500px] overflow-hidden rounded border border-[var(--panel-border)]"
    />
  )
}
