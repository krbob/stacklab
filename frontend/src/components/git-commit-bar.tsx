import { useCallback, useState, type FormEvent } from 'react'
import { commitGitWorkspace, pushGitWorkspace } from '@/lib/api-client'
import { cn } from '@/lib/cn'

interface GitCommitBarProps {
  selectedPaths: Set<string>
  hasUpstream: boolean
  aheadCount: number
  onCommitted: () => void
  onPushed: () => void
}

export function GitCommitBar({ selectedPaths, hasUpstream, aheadCount, onCommitted, onPushed }: GitCommitBarProps) {
  const [showCommitInput, setShowCommitInput] = useState(false)
  const [commitMessage, setCommitMessage] = useState('')
  const [committing, setCommitting] = useState(false)
  const [pushing, setPushing] = useState(false)
  const [result, setResult] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  const handleCommit = useCallback(async (e: FormEvent) => {
    e.preventDefault()
    if (!commitMessage.trim() || selectedPaths.size === 0) return

    setCommitting(true)
    setResult(null)
    try {
      const res = await commitGitWorkspace({
        message: commitMessage.trim(),
        paths: Array.from(selectedPaths),
      })
      setResult({ type: 'success', text: `Committed ${res.commit.slice(0, 8)}: ${res.summary}` })
      setCommitMessage('')
      setShowCommitInput(false)
      onCommitted()
    } catch (err) {
      setResult({ type: 'error', text: err instanceof Error ? err.message : 'Commit failed' })
    } finally {
      setCommitting(false)
    }
  }, [commitMessage, selectedPaths, onCommitted])

  const handlePush = useCallback(async () => {
    setPushing(true)
    setResult(null)
    try {
      const res = await pushGitWorkspace()
      if (res.pushed) {
        setResult({ type: 'success', text: `Pushed to ${res.upstream_name}` })
      } else {
        setResult({ type: 'success', text: 'Already up to date' })
      }
      onPushed()
    } catch (err) {
      setResult({ type: 'error', text: err instanceof Error ? err.message : 'Push failed' })
    } finally {
      setPushing(false)
    }
  }, [onPushed])

  return (
    <div className="border-t border-[var(--panel-border)] pt-3">
      {/* Result message */}
      {result && (
        <div className={cn('mb-2 text-xs', result.type === 'success' ? 'text-emerald-400' : 'text-red-400')}>
          {result.text}
        </div>
      )}

      {/* Commit input */}
      {showCommitInput && (
        <form onSubmit={handleCommit} className="mb-2 flex gap-2">
          <input
            type="text"
            value={commitMessage}
            onChange={(e) => setCommitMessage(e.target.value)}
            placeholder="Commit message..."
            autoFocus
            disabled={committing}
            data-testid="git-commit-message"
            className="flex-1 rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-2 py-1.5 text-xs text-[var(--text)] outline-none focus:border-[rgba(34,197,94,0.35)] disabled:opacity-50"
          />
          <button
            type="submit"
            disabled={committing || !commitMessage.trim() || selectedPaths.size === 0}
            data-testid="git-commit-submit"
            className="rounded-lg border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-3 py-1.5 text-xs text-[var(--text)] disabled:opacity-40"
          >
            {committing ? '...' : 'Commit'}
          </button>
          <button
            type="button"
            onClick={() => { setShowCommitInput(false); setCommitMessage('') }}
            className="rounded-lg border border-[var(--panel-border)] px-2 py-1.5 text-xs text-[var(--muted)]"
          >
            Cancel
          </button>
        </form>
      )}

      {/* Action buttons */}
      <div className="flex items-center gap-2">
        <span className="text-xs text-[var(--muted)]">
          {selectedPaths.size} file{selectedPaths.size !== 1 ? 's' : ''} selected
        </span>

        {!showCommitInput && (
          <button
            onClick={() => setShowCommitInput(true)}
            disabled={selectedPaths.size === 0 || committing}
            className="ml-auto rounded-full border border-[rgba(34,197,94,0.35)] bg-[rgba(34,197,94,0.14)] px-3 py-1 text-xs text-[var(--text)] disabled:opacity-40"
          >
            Commit
          </button>
        )}

        {hasUpstream && aheadCount > 0 && (
          <button
            onClick={handlePush}
            disabled={pushing}
            data-testid="git-push"
            className="rounded-full border border-amber-400/30 bg-amber-400/10 px-3 py-1 text-xs text-amber-400 disabled:opacity-40"
          >
            {pushing ? 'Pushing...' : `Push (${aheadCount} ahead)`}
          </button>
        )}
      </div>
    </div>
  )
}
