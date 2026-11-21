import { useCallback, useState, type FormEvent } from 'react'
import { commitGitWorkspace, pushGitWorkspace } from '@/lib/api-client'
import { cn } from '@/lib/cn'
import { StatusMessage } from '@/components/status-message'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { checkGitSync } from '@/hooks/use-git-sync'

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
  const [commitReview, setCommitReview] = useState<{ message: string; paths: string[]; warning: string } | null>(null)

  const performCommit = useCallback(async (payload: { message: string; paths: string[] }) => {
    setCommitting(true)
    setResult(null)
    try {
      const res = await commitGitWorkspace({ message: payload.message, paths: payload.paths })
      setCommitReview(null)
      setResult({ type: 'success', text: `Committed ${res.commit.slice(0, 8)}: ${res.summary}` })
      setCommitMessage('')
      setShowCommitInput(false)
      onCommitted()
    } catch (err) {
      setResult({ type: 'error', text: err instanceof Error ? err.message : 'Commit failed' })
    } finally {
      setCommitting(false)
    }
  }, [onCommitted])

  const handleCommit = useCallback(async (e: FormEvent) => {
    e.preventDefault()
    if (!commitMessage.trim() || selectedPaths.size === 0 || committing || pushing) return
    const payload = { message: commitMessage.trim(), paths: Array.from(selectedPaths) }
    setCommitting(true)
    setResult(null)
    if (hasUpstream) {
      const incoming = await checkGitSync()
      if (!incoming || incoming.reason || incoming.behind_count > 0) {
        setCommitReview({ ...payload, warning: !incoming || incoming.reason ? 'The remote could not be verified. You can preserve the selected changes in a local commit and synchronize when access is restored.' : `${incoming.behind_count} incoming commits were found. Preserve your selected local changes in a commit, then use Remote updates to reconcile both versions before pushing.` })
        setCommitting(false)
        return
      }
    }
    await performCommit(payload)
  }, [commitMessage, selectedPaths, committing, pushing, hasUpstream, performCommit])

  const handlePush = useCallback(async () => {
    setPushing(true)
    setResult(null)
    try {
      const incoming = await checkGitSync()
      if (!incoming || incoming.reason) throw new Error('Remote not verified. Check remote access before pushing; your local commits are preserved.')
      if (incoming.behind_count > 0) throw new Error('The remote contains new commits. Open Remote updates to reconcile both versions before pushing.')
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
      {commitReview && <ConfirmDialog title="Preserve local changes?" message={commitReview.warning} items={commitReview.paths} confirmLabel="Commit locally" confirming={committing} error={result?.type === 'error' ? result.text : null} onConfirm={() => { void performCommit(commitReview) }} onCancel={() => setCommitReview(null)} />}
      {/* Result message */}
      {result && (
        <StatusMessage className={cn('mb-2 wrap-anywhere text-xs', result.type === 'success' ? 'text-[var(--ok)]' : 'text-[var(--danger)]')}>
          {result.text}
        </StatusMessage>
      )}

      {/* Commit input */}
      {showCommitInput && (
        <form onSubmit={handleCommit} className="mb-2 flex min-w-0 flex-col gap-2">
          <input
            type="text"
            value={commitMessage}
            onChange={(e) => setCommitMessage(e.target.value)}
            placeholder="Commit message..."
            aria-label="Commit message"
            autoFocus
            disabled={committing}
            data-testid="git-commit-message"
            className="w-full min-w-0 rounded-lg border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-2 py-1.5 text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)] disabled:opacity-50"
          />
          <div className="flex flex-wrap gap-2">
            <button
              type="submit"
              disabled={committing || pushing || !commitMessage.trim() || selectedPaths.size === 0}
              data-testid="git-commit-submit"
              className="rounded-lg border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-3 py-1.5 text-xs text-[var(--text)] disabled:opacity-40"
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
          </div>
        </form>
      )}

      {/* Action buttons */}
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-[var(--muted)]">
          {selectedPaths.size} file{selectedPaths.size !== 1 ? 's' : ''} selected
        </span>

        {!showCommitInput && (
          <button
            onClick={() => setShowCommitInput(true)}
            disabled={selectedPaths.size === 0 || committing || pushing}
            className="ml-auto rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-3 py-1 text-xs text-[var(--text)] disabled:opacity-40"
          >
            Commit
          </button>
        )}

        {hasUpstream && aheadCount > 0 && (
          <button
            onClick={handlePush}
            disabled={pushing || committing || commitReview !== null}
            data-testid="git-push"
            className="rounded-md border border-[var(--warning)]/30 bg-[var(--warning)]/10 px-3 py-1 text-xs text-[var(--warning)] disabled:opacity-40"
          >
            {pushing ? 'Pushing...' : `Push (${aheadCount} ahead)`}
          </button>
        )}
      </div>
    </div>
  )
}
