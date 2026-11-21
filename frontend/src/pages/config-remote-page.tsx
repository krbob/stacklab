import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { getGitIncomingDiff, getStacks, invokeAction, pullGitWorkspace } from '@/lib/api-client'
import type { GitIncomingDiffResponse, GitPullReceipt, StackListItem } from '@/lib/api-types'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { DiffView } from '@/components/diff-view'
import { ProgressPanel } from '@/components/progress-panel'
import { StatusMessage } from '@/components/status-message'
import { useJobStream } from '@/hooks/use-job-stream'
import { useGitSync } from '@/hooks/use-git-sync'
import { GitReconcilePanel } from '@/components/git-reconcile-panel'

const button = 'rounded-md border border-[var(--panel-border)] px-3 py-2 text-sm text-[var(--text)] transition hover:bg-[rgba(245,165,36,0.08)] disabled:opacity-40'
const panel = 'min-w-0 space-y-4 rounded-lg border border-[var(--panel-border)] bg-[var(--panel)] p-4 sm:p-5'
const explanations: Record<string, string> = {
  not_a_git_repository: 'This workspace is not a Git repository. Configure its checkout on the host first.',
  git_not_installed: 'Git is not installed on this host.',
  upstream_not_configured: 'Configure a remote upstream for this branch on the host before checking updates.',
  detached_head: 'The checkout is not on a branch. Select a branch on the host before downloading updates.',
  up_to_date: 'No incoming commits. The checkout already contains the fetched version.',
  diverged: 'Local and remote commits have diverged. Prepare a reconciliation draft below. Existing commits will be preserved.',
  local_changes: 'There are local edits or untracked files in this repository. Commit or resolve them before downloading. This includes files outside config/ and stacks/.',
  operation_in_progress: 'Finish the current Git operation on the host, then check again.',
  submodules: 'This checkout contains submodules. Update it outside Stacklab to manage their revisions explicitly.',
  index_flags: 'This checkout hides tracked changes with skip-worktree or assume-unchanged flags. Resolve these flags on the host before downloading.',
}

export function ConfigRemotePage() {
  const { incoming, checking, error, refresh: checkRemote } = useGitSync(false)
  const [lastPull, setLastPull] = useState<GitPullReceipt | null>(null)
  const checkingRef = useRef(false)
  const [diff, setDiff] = useState<GitIncomingDiffResponse | null>(null)
  const [diffError, setDiffError] = useState<string | null>(null)
  const [diffLoading, setDiffLoading] = useState(false)
  const [selectedPath, setSelectedPath] = useState<string | null>(null)
  const diffRequest = useRef(0)
  const [reviewDownload, setReviewDownload] = useState(false)
  const [downloading, setDownloading] = useState(false)
  const [downloadError, setDownloadError] = useState<string | null>(null)
  const [message, setMessage] = useState<string | null>(null)
  const [stacks, setStacks] = useState<StackListItem[]>([])
  const [stackError, setStackError] = useState<string | null>(null)
  const [reloadStack, setReloadStack] = useState<string | null>(null)
  const [startingReload, setStartingReload] = useState(false)
  const [reloadError, setReloadError] = useState<string | null>(null)
  const [jobID, setJobID] = useState<string | null>(null)
  const stream = useJobStream({ jobId: jobID })
  const reloading = jobID !== null && !['succeeded', 'failed', 'cancelled', 'timed_out'].includes(stream.state ?? '')

  const loadStacks = useCallback(async () => {
    try { setStacks((await getStacks()).items); setStackError(null) }
    catch (err) { setStackError(err instanceof Error ? err.message : 'Cannot load stacks.') }
  }, [])

  const refresh = useCallback(async () => {
    if (checkingRef.current) return
    checkingRef.current = true
    setSelectedPath(null)
    setDiff(null)
    setDiffError(null)
    setDiffLoading(false)
    diffRequest.current++
    try {
      const result = await checkRemote()
      if (result) setLastPull(result.last_pull ?? null)
    } finally { checkingRef.current = false }
  }, [checkRemote])

  useEffect(() => { void refresh(); void loadStacks() }, [refresh, loadStacks])

  const openDiff = async (path: string) => {
    if (!incoming) return
    const id = ++diffRequest.current
    setSelectedPath(path)
    setDiff(null)
    setDiffError(null)
    setDiffLoading(true)
    try {
      const result = await getGitIncomingDiff(path, incoming.expected_state)
      if (id === diffRequest.current) setDiff(result)
    } catch (err) {
      if (id === diffRequest.current) setDiffError(err instanceof Error ? err.message : 'Cannot load this diff.')
    } finally { if (id === diffRequest.current) setDiffLoading(false) }
  }

  const download = async () => {
    if (!incoming) return
    setDownloading(true)
    setDownloadError(null)
    try {
      const result = await pullGitWorkspace(incoming.expected_state)
      setLastPull(result.receipt)
      setReviewDownload(false)
      setMessage(`Downloaded ${result.receipt.head_commit.slice(0, 8)}. Review the affected stacks below to apply configuration.`)
      await Promise.all([refresh(), loadStacks()])
    } catch (err) {
      setDownloadError(err instanceof Error ? err.message : 'Download failed. Check remote updates before retrying.')
    } finally { setDownloading(false) }
  }

  const reload = async () => {
    if (!reloadStack) return
    setStartingReload(true)
    setReloadError(null)
    try {
      const result = await invokeAction(reloadStack, stacks.find((stack) => stack.id === reloadStack)?.runtime_state === 'defined' ? 'up' : 'recreate')
      setJobID(result.job.id)
      setReloadStack(null)
    } catch (err) { setReloadError(err instanceof Error ? err.message : 'Cannot start configuration reload.') }
    finally { setStartingReload(false) }
  }

  const affected = [...new Set(lastPull?.files.flatMap((file) => file.stack_id ? [file.stack_id] : []) ?? [])].sort()
  const blocked = incoming?.reason || incoming?.blocked_reason
  const busy = checking || downloading || reloading || startingReload

  return (
    <div className="space-y-5" data-testid="config-remote-page">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-2">
          <Link to="/config" className="text-sm text-[var(--accent)] hover:underline">← Config workspace</Link>
          <h1 className="text-2xl font-semibold">Remote updates</h1>
          <p className="text-sm text-[var(--muted)]">Review configuration pushed from another machine, download it, then apply selected stacks.</p>
        </div>
        <button className={button} disabled={busy || reviewDownload} onClick={() => { void refresh(); void loadStacks() }}>{checking ? 'Checking remote…' : 'Check remote'}</button>
      </header>
      {message && <StatusMessage className="wrap-anywhere text-sm text-[var(--ok)]">{message}</StatusMessage>}
      {error && <StatusMessage className="wrap-anywhere text-sm text-[var(--danger)]">{error} Displayed results may be out of date; check again before downloading.</StatusMessage>}

      <section className={panel} aria-busy={checking || downloading}>
        <h2 className="text-lg font-medium">1. Review incoming changes</h2>
        {!incoming && checking && <p role="status" className="text-sm text-[var(--muted)]">Checking the configured upstream…</p>}
        {incoming && <>
          {incoming.branch && <p className="wrap-anywhere text-sm text-[var(--muted)]">{incoming.branch}{incoming.upstream_name && ` ← ${incoming.upstream_name}`} · Checked {new Date(incoming.checked_at).toLocaleString()}</p>}
          {incoming.behind_count > 0 && <p className="text-sm">{incoming.behind_count} incoming {incoming.behind_count === 1 ? 'commit' : 'commits'} · {incoming.files.length} changed files{incoming.ahead_count > 0 && ` · ${incoming.ahead_count} local commits`}</p>}
          {blocked && <p className="text-sm text-[var(--muted)]">{explanations[blocked] ?? blocked}</p>}
          {incoming.files.length > 0 && <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(15rem,1fr)_minmax(0,2fr)]">
            <div className="min-w-0 space-y-1" aria-label="Incoming files">
              {incoming.files.map((file) => <button key={file.path} className={`${button} flex w-full flex-col items-start gap-1 text-left ${selectedPath === file.path ? 'bg-[rgba(245,165,36,0.12)]' : ''}`} disabled={busy} onClick={() => { void openDiff(file.path) }}>
                <span className="w-full wrap-anywhere font-mono text-xs">{file.status} · {file.path}</span>
                <span className="text-xs text-[var(--muted)]">{file.managed ? (file.stack_id ? `Stack: ${file.stack_id}` : 'Shared configuration') : 'Other repository file'}</span>
              </button>)}
            </div>
            <div className="min-w-0 space-y-2" aria-busy={diffLoading}>
              {selectedPath && <h3 className="wrap-anywhere font-mono text-sm">{selectedPath}</h3>}
              {diffLoading && <p role="status" className="text-sm text-[var(--muted)]">Loading diff…</p>}
              {diffError && <StatusMessage className="text-sm text-[var(--danger)]">{diffError}</StatusMessage>}
              {diff && <DiffView diff={diff.diff} truncated={diff.truncated} />}
              {!selectedPath && <p className="text-sm text-[var(--muted)]">Select a file to compare the local commit with the incoming version.</p>}
            </div>
          </div>}
          {incoming.behind_count > 0 && <div className="space-y-3 border-t border-[var(--panel-border)] pt-4">
            <p className="text-sm text-[var(--muted)]">Download updates the whole repository. Applications watching mounted configuration may react as soon as files change.</p>
            <button className={button} disabled={!incoming.can_pull || busy || !!error} onClick={() => { setDownloadError(null); setReviewDownload(true) }}>Download reviewed changes</button>
          </div>}
        </>}
      </section>

      <GitReconcilePanel incoming={incoming} disabled={busy || !!error || reviewDownload} onApplied={async (receipt) => { setLastPull(receipt); setMessage(`Applied merge ${receipt.head_commit.slice(0, 8)}. Review the affected stacks below; push the merge commit from Changes when ready.`); await Promise.all([refresh(), loadStacks()]) }} />

      <section className={panel}>
        <h2 className="text-lg font-medium">2. Apply configuration</h2>
        {!lastPull && <p className="text-sm text-[var(--muted)]">After downloading, affected stacks appear here. Changes already made on this host can be applied from the <Link className="text-[var(--accent)] hover:underline" to="/stacks">Stacks</Link> view.</p>}
        {lastPull && <>
          <p className="wrap-anywhere text-sm text-[var(--muted)]">Last synchronized: {lastPull.head_commit.slice(0, 8)} · {new Date(lastPull.downloaded_at).toLocaleString()} · {lastPull.branch}. This records a download, not a deployment.</p>
          <details className="space-y-2 text-sm">
            <summary className="cursor-pointer text-[var(--muted)]">Downloaded files ({lastPull.files.length})</summary>
            <ul className="space-y-1">{lastPull.files.map((file) => <li key={file.path} className="wrap-anywhere font-mono text-xs">{file.status} · {file.path}</li>)}</ul>
          </details>
          <p className="text-sm text-[var(--muted)]">Reload recreates containers using the files currently on this host. Review shared configuration dependencies and build changed Dockerfiles before reloading.</p>
          {stackError && <StatusMessage className="text-sm text-[var(--danger)]">{stackError}</StatusMessage>}
          {affected.map((id) => {
            const stack = stacks.find((item) => item.id === id)
            return <div key={id} className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-[var(--panel-border)] p-3">
              <div className="min-w-0 space-y-1">
                <Link className="wrap-anywhere font-medium text-[var(--accent)] hover:underline" to={`/stacks/${encodeURIComponent(id)}`}>{id}</Link>
                <p className="text-xs text-[var(--muted)]">{stack ? (stack.config_state === 'invalid' ? 'Invalid definition — review before applying.' : stack.runtime_state === 'defined' ? 'Not deployed' : stack.runtime_state) : 'Definition unavailable. Review this path and its dependent stacks.'}</p>
              </div>
              <button className={button} disabled={busy || !!stackError || !stack || stack.config_state === 'invalid' || stack.runtime_state === 'orphaned' || stack.activity_state === 'locked'} onClick={() => { setReloadError(null); setReloadStack(id) }}>{stack?.runtime_state === 'defined' ? 'Deploy configuration' : 'Reload configuration'}</button>
            </div>
          })}
          <Link className="inline-block text-sm text-[var(--accent)] hover:underline" to="/stacks">Review all stacks and shared dependencies →</Link>
        </>}
        {jobID && <ProgressPanel jobId={jobID} stream={stream} onDone={() => { void loadStacks() }} onClose={() => setJobID(null)} />}
      </section>

      {reviewDownload && incoming && <ConfirmDialog title="Download configuration?" message={`Update ${incoming.branch} from ${incoming.head_commit.slice(0, 8)} to the reviewed ${incoming.target_commit.slice(0, 8)}. All listed files will change. Mounted configuration may be reloaded by applications immediately; container recreation is a separate action.`} items={incoming.files.map((file) => file.path)} confirmLabel="Download configuration" confirming={downloading} confirmingLabel="Downloading…" error={downloadError} onConfirm={() => { void download() }} onCancel={() => setReviewDownload(false)} />}
      {reloadStack && <ConfirmDialog title="Apply stack configuration?" message={`Create or recreate containers for ${reloadStack} using its current Compose, environment and mounted configuration. Running services will be interrupted and stopped services may start.`} confirmLabel="Apply configuration" confirming={startingReload} error={reloadError} onConfirm={() => { void reload() }} onCancel={() => setReloadStack(null)} />}
    </div>
  )
}
