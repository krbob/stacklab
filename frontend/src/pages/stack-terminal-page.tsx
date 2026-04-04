import { useCallback, useEffect, useRef, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import type { StackDetailResponse } from '@/lib/api-types'
import { useTerminal } from '@/hooks/use-terminal'
import { useWs } from '@/hooks/use-ws'
import { TerminalView } from '@/components/terminal-view'
import { cn } from '@/lib/cn'

const EXIT_REASONS: Record<string, string> = {
  process_exit: 'Shell exited',
  idle_timeout: 'Session timed out due to inactivity',
  client_close: 'Session closed',
  server_cleanup: 'Session closed by server',
  connection_replaced: 'Session taken over by another connection',
}

export function StackTerminalPage() {
  const { stack } = useOutletContext<{ stack: StackDetailResponse['stack'] }>()
  const { connected } = useWs()

  const runningContainers = stack.containers.filter((c) => c.status === 'running')

  const [selectedContainerId, setSelectedContainerId] = useState(runningContainers[0]?.id ?? '')
  const [shell, setShell] = useState('/bin/sh')

  const terminal = useTerminal({
    stackId: stack.id,
    containerId: selectedContainerId,
    shell,
  })

  // Ref that TerminalView will call to write WS output into XTerm.js
  const writeToXtermRef = useRef<((data: string) => void) | null>(null)

  // Connect WS terminal output → XTerm.js
  useEffect(() => {
    terminal.onData((data) => {
      writeToXtermRef.current?.(data)
    })
  }, [terminal])

  // XTerm.js user input → WS
  const handleUserInput = useCallback((data: string) => {
    terminal.write(data)
  }, [terminal])

  const handleResize = useCallback((cols: number, rows: number) => {
    terminal.resize(cols, rows)
  }, [terminal])

  if (runningContainers.length === 0) {
    return (
      <div className="rounded-md border border-[var(--panel-border)] bg-[rgba(255,255,255,0.02)] px-5 py-10 text-center">
        <p className="text-[var(--text)]">No containers available for shell access</p>
        <p className="mt-1 text-sm text-[var(--muted)]">Start the stack to open a shell session.</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      {/* Controls */}
      <div className="flex flex-wrap items-center gap-2">
        <label className="flex items-center gap-2 text-sm text-[var(--muted)]">
          Container:
          <select
            value={selectedContainerId}
            onChange={(e) => setSelectedContainerId(e.target.value)}
            disabled={terminal.state === 'connected'}
            className="rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-2 py-1 text-xs text-[var(--text)] outline-none"
          >
            {runningContainers.map((c) => (
              <option key={c.id} value={c.id}>{c.service_name} ({c.name})</option>
            ))}
          </select>
        </label>

        <label className="flex items-center gap-2 text-sm text-[var(--muted)]">
          Shell:
          <select
            value={shell}
            onChange={(e) => setShell(e.target.value)}
            disabled={terminal.state === 'connected'}
            className="rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-2 py-1 text-xs text-[var(--text)] outline-none"
          >
            <option value="/bin/sh">/bin/sh</option>
            <option value="/bin/bash">/bin/bash</option>
          </select>
        </label>

        {terminal.state === 'idle' || terminal.state === 'ended' || terminal.state === 'error' ? (
          <button
            onClick={terminal.open}
            disabled={!connected || !selectedContainerId}
            className="rounded-full border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-3 py-1 text-xs text-[var(--text)] transition hover:bg-[rgba(34,197,94,0.2)] disabled:opacity-40"
          >
            {terminal.state === 'ended' || terminal.state === 'error' ? 'New session' : 'Connect'}
          </button>
        ) : terminal.state === 'connecting' ? (
          <span className="text-xs text-[var(--muted)]">Connecting...</span>
        ) : (
          <button
            onClick={terminal.close}
            className="rounded-full border border-red-400/30 px-3 py-1 text-xs text-red-400 transition hover:bg-red-400/10"
          >
            Disconnect
          </button>
        )}
      </div>

      {/* Status messages */}
      {terminal.state === 'ended' && terminal.exitInfo && (
        <div className="text-xs text-amber-400">
          {EXIT_REASONS[terminal.exitInfo.reason] ?? terminal.exitInfo.reason}
          {terminal.exitInfo.reason === 'process_exit' && ` (code ${terminal.exitInfo.exit_code})`}
        </div>
      )}
      {terminal.state === 'error' && terminal.errorMessage && (
        <div className="text-xs text-red-400">{terminal.errorMessage}</div>
      )}
      {!connected && terminal.state === 'connected' && (
        <div className="text-xs text-amber-400">Connection lost. Attempting to reattach...</div>
      )}

      {/* Connection indicator */}
      <div className="flex items-center gap-2 text-xs text-[var(--muted)]">
        <span className={cn(
          'inline-block size-2 rounded-full',
          terminal.state === 'connected' ? 'bg-emerald-400' : 'bg-zinc-600',
        )} />
        {terminal.state === 'connected' ? 'Connected' : terminal.state === 'connecting' ? 'Connecting...' : 'Disconnected'}
      </div>

      {/* Terminal */}
      {terminal.state === 'connected' && (
        <TerminalView
          onData={handleUserInput}
          onResize={handleResize}
          writeRef={writeToXtermRef}
        />
      )}

      {/* Tablet hint */}
      <div className="hidden text-xs text-[var(--muted)] md:max-lg:block">
        Best experience on desktop.
      </div>
    </div>
  )
}
