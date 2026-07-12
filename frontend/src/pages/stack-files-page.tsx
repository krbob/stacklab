import { useCallback, useState } from 'react'
import { useOutletContext, useNavigate } from 'react-router-dom'
import { File, FileQuestion, FileWarning, Folder, Hammer, Plus } from 'lucide-react'
import { getStackWorkspaceTree, getStackWorkspaceFile, saveStackWorkspaceFile, repairStackWorkspacePermissions } from '@/lib/api-client'
import type { StackDetailResponse, StackWorkspaceTreeEntry, StackWorkspaceFileResponse } from '@/lib/api-types'
import { useApi } from '@/hooks/use-api'
import { YamlEditor } from '@/components/yaml-editor'
import { BlockedFileCard } from '@/components/blocked-file-card'
import { cn } from '@/lib/cn'
import { UnsavedChangesGuard } from '@/components/unsaved-changes-guard'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { usePendingAction } from '@/hooks/use-pending-action'
import { StatusMessage } from '@/components/status-message'
import { AsyncState } from '@/components/async-state'

const RESERVED_ROOT_FILES = ['compose.yaml', '.env']

function isDockerfile(name: string): boolean {
  return name === 'Dockerfile' || name.startsWith('Dockerfile.')
}

const entryIcons: Record<string, typeof File> = {
  directory: Folder,
  text_file: File,
  binary_file: FileWarning,
  unknown_file: FileQuestion,
}

export function StackFilesPage() {
  const { stack } = useOutletContext<{ stack: StackDetailResponse['stack'] }>()
  const navigate = useNavigate()

  const [treePath, setTreePath] = useState('')
  const [selectedFile, setSelectedFile] = useState<StackWorkspaceFileResponse | null>(null)
  const [fileLoading, setFileLoading] = useState(false)
  const [fileError, setFileError] = useState<string | null>(null)
  const [editContent, setEditContent] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveMessage, setSaveMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [creatingFile, setCreatingFile] = useState(false)
  const [newFileName, setNewFileName] = useState('')
  const [confirmDiscard, setConfirmDiscard] = useState(false)

  const { data: treeData, loading: treeLoading, error: treeError, refetch: refetchTree } = useApi(
    () => getStackWorkspaceTree(stack.id, treePath || undefined),
    [stack.id, treePath],
  )

  const isDirty = selectedFile?.type === 'text_file' && editContent !== (selectedFile.content ?? '')
  const {
    hasPendingAction,
    requestAction,
    cancelPendingAction,
    confirmPendingAction,
  } = usePendingAction(isDirty)

  const openFile = useCallback(async (path: string) => {
    setFileLoading(true)
    setFileError(null)
    setSaveMessage(null)
    setCreatingFile(false)
    try {
      const file = await getStackWorkspaceFile(stack.id, path)
      setSelectedFile(file)
      setEditContent(file.content ?? '')
    } catch (err) {
      setFileError(err instanceof Error ? err.message : 'Failed to load file')
      setSelectedFile(null)
    } finally {
      setFileLoading(false)
    }
  }, [stack.id])

  const navigateDir = useCallback((path: string) => {
    setTreePath(path)
    setSelectedFile(null)
    setFileError(null)
    setSaveMessage(null)
    setCreatingFile(false)
  }, [])

  const handleSave = useCallback(async () => {
    if (!selectedFile) return
    setSaving(true)
    setSaveMessage(null)
    try {
      await saveStackWorkspaceFile(stack.id, selectedFile.path, editContent, false, selectedFile.modified_at)
      setSaveMessage({ type: 'success', text: 'Saved' })
      const updated = await getStackWorkspaceFile(stack.id, selectedFile.path)
      setSelectedFile(updated)
      setEditContent(updated.content ?? '')
    } catch (err) {
      setSaveMessage({ type: 'error', text: err instanceof Error ? err.message : 'Save failed' })
    } finally {
      setSaving(false)
    }
  }, [stack.id, selectedFile, editContent])

  const handleCreateFile = useCallback(async () => {
    if (!newFileName.trim()) return
    const path = treePath ? `${treePath}/${newFileName.trim()}` : newFileName.trim()
    setSaving(true)
    try {
      await saveStackWorkspaceFile(stack.id, path, '', false)
      setCreatingFile(false)
      setNewFileName('')
      refetchTree()
      openFile(path)
    } catch (err) {
      setSaveMessage({ type: 'error', text: err instanceof Error ? err.message : 'Create failed' })
    } finally {
      setSaving(false)
    }
  }, [stack.id, treePath, newFileName, refetchTree, openFile])

  const requestDiscardingAction = useCallback((action: () => void) => {
    requestAction(() => {
      if (selectedFile) setEditContent(selectedFile.content ?? '')
      action()
    })
  }, [requestAction, selectedFile])

  const requestOpenFile = useCallback((path: string) => {
    if (selectedFile?.path === path) return
    requestDiscardingAction(() => { void openFile(path) })
  }, [openFile, requestDiscardingAction, selectedFile?.path])

  const requestNavigateDir = useCallback((path: string) => {
    if (treePath === path) return
    requestDiscardingAction(() => navigateDir(path))
  }, [navigateDir, requestDiscardingAction, treePath])

  const requestCreateFile = useCallback(() => {
    if (!newFileName.trim()) return
    requestDiscardingAction(() => { void handleCreateFile() })
  }, [handleCreateFile, newFileName, requestDiscardingAction])

  const currentTree = treeData?.current_path === treePath ? treeData : null
  const treeEntries = currentTree?.items ?? []
  const parentPath = currentTree?.parent_path ?? null
  const treeLoadError = treeError
    ? new Error(`Failed to load file tree: ${treeError.message}`)
    : null

  return (
    <div aria-busy={treeLoading || fileLoading || saving} className="flex flex-col gap-4 lg:flex-row" style={{ minHeight: '400px' }}>
      <UnsavedChangesGuard when={isDirty} />

      {/* Tree panel */}
      <div className="w-full shrink-0 overflow-y-auto lg:w-56">
        <AsyncState
          loading={treeLoading}
          error={treeLoadError}
          hasData={currentTree !== null}
          isEmpty={false}
          loadingLabel="Loading file tree."
          emptyMessage="File tree unavailable."
          onRetry={refetchTree}
          retryLabel="Retry file tree"
          loadingFallback={(
            <div className="space-y-2">
              {[1, 2, 3].map((i) => <div key={i} className="h-6 animate-pulse rounded bg-[rgba(255,255,255,0.05)]" />)}
            </div>
          )}
        >
          <nav className="space-y-0.5">
            {parentPath !== null && (
              <button onClick={() => requestNavigateDir(parentPath)} className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs text-[var(--muted)] transition hover:bg-[rgba(255,255,255,0.05)] hover:text-[var(--text)]">
                <Folder className="size-3.5" /><span>.. (up)</span>
              </button>
            )}
            {treeEntries.map((entry) => (
              <TreeRow
                key={entry.path}
                entry={entry}
                isRoot={treePath === ''}
                isSelected={selectedFile?.path === entry.path}
                onOpenFile={requestOpenFile}
                onNavigateDir={requestNavigateDir}
                onGoToEditor={() => navigate('../editor', { relative: 'path' })}
              />
            ))}
            {treeEntries.length === 0 && <p className="px-2 py-4 text-xs text-[var(--muted)]">Empty directory</p>}
            {!creatingFile && (
              <button onClick={() => { setCreatingFile(true); setNewFileName('') }} className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs text-[var(--muted)] transition hover:bg-[rgba(255,255,255,0.05)] hover:text-[var(--text)]">
                <Plus className="size-3.5" /><span>New file</span>
              </button>
            )}
            {creatingFile && (
              <div className="px-2 py-1">
                <input type="text" value={newFileName} onChange={(e) => setNewFileName(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') requestCreateFile(); if (e.key === 'Escape') setCreatingFile(false) }} placeholder="filename" autoFocus className="w-full rounded border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-2 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(245,165,36,0.35)]" />
              </div>
            )}
          </nav>
        </AsyncState>
      </div>

      {/* Editor panel */}
      <div className="flex min-w-0 flex-1 flex-col">
        {!selectedFile && !fileLoading && !fileError && (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-sm text-[var(--muted)]">Select a file to view or edit.</p>
          </div>
        )}
        {fileLoading && <div className="flex flex-1 items-center justify-center"><p className="text-sm text-[var(--muted)]" role="status" aria-live="polite">Loading file...</p></div>}
        {fileError && <div className="rounded-md border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">{fileError}</div>}
        {selectedFile && (
          <>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <div className="flex items-center gap-2">
                  <h2 className="text-lg font-medium text-[var(--text)]">{selectedFile.name}</h2>
                  {isDockerfile(selectedFile.name) && <span className="rounded border border-[var(--panel-border)] px-1.5 py-0.5 text-xs text-[var(--warning)]">build</span>}
                  <span className="rounded-md border border-[var(--panel-border)] px-2 py-0.5 text-xs text-[var(--muted)]">{selectedFile.type.replace('_', ' ')}</span>
                </div>
                <div className="mt-1 text-xs text-[var(--muted)]">
                  {selectedFile.path} · {new Date(selectedFile.modified_at).toLocaleString()}
                </div>
              </div>
              {selectedFile.type === 'text_file' && selectedFile.writable && !selectedFile.blocked_reason && (
                <div className="flex items-center gap-2">
                  {isDirty && <span className="text-xs text-[var(--warning)]">Unsaved changes</span>}
                  {isDirty && <button onClick={() => setConfirmDiscard(true)} className="rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]">Discard</button>}
                  <button onClick={handleSave} disabled={saving || !isDirty} className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-3 py-1 text-xs text-[var(--text)] disabled:opacity-40">
                    {saving ? 'Saving...' : 'Save'}
                  </button>
                </div>
              )}
            </div>
            {saveMessage && <StatusMessage className={cn('mt-2 text-xs', saveMessage.type === 'success' ? 'text-[var(--ok)]' : 'text-[var(--danger)]')}>{saveMessage.text}</StatusMessage>}
            <div className="mt-3 flex-1" style={{ minHeight: '300px' }}>
              {selectedFile.blocked_reason ? (
                <BlockedFileCard
                  stateKey={selectedFile.path}
                  blockedReason={selectedFile.blocked_reason}
                  permissions={selectedFile.permissions}
                  repairCapability={selectedFile.repair_capability}
                  onRepair={selectedFile.repair_capability?.supported ? async (recursive) => {
                    const result = await repairStackWorkspacePermissions(stack.id, { path: selectedFile.path, recursive })
                    void openFile(selectedFile.path)
                    void refetchTree()
                    return result
                  } : undefined}
                />
              ) : selectedFile.type === 'text_file' ? (
                <YamlEditor value={editContent} onChange={setEditContent} readOnly={!selectedFile.writable} />
              ) : (
                <div className="flex h-full items-center justify-center rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.2)]">
                  <div className="text-center">
                    <FileWarning className="mx-auto size-8 text-[var(--muted)]" />
                    <p className="mt-2 text-sm text-[var(--text)]">{selectedFile.type === 'binary_file' ? 'Binary file' : 'Unknown file type'}</p>
                    <p className="mt-1 text-xs text-[var(--muted)]">Size: {(selectedFile.size_bytes / 1024).toFixed(1)} KB</p>
                  </div>
                </div>
              )}
            </div>
          </>
        )}
      </div>

      {confirmDiscard && selectedFile && (
        <ConfirmDialog
          title={`Discard changes to "${selectedFile.name}"?`}
          message="This reverts the editor to the last loaded file content."
          items={[selectedFile.path]}
          confirmLabel="Discard changes"
          onCancel={() => setConfirmDiscard(false)}
          onConfirm={() => {
            setEditContent(selectedFile.content ?? '')
            setConfirmDiscard(false)
          }}
        />
      )}

      {hasPendingAction && selectedFile && (
        <ConfirmDialog
          title={`Discard changes to "${selectedFile.name}"?`}
          message="Continue with the selected action and discard this file's unsaved changes."
          items={[selectedFile.path]}
          confirmLabel="Discard and continue"
          onCancel={cancelPendingAction}
          onConfirm={confirmPendingAction}
        />
      )}
    </div>
  )
}

function TreeRow({ entry, isRoot, isSelected, onOpenFile, onNavigateDir, onGoToEditor }: {
  entry: StackWorkspaceTreeEntry
  isRoot: boolean
  isSelected: boolean
  onOpenFile: (path: string) => void
  onNavigateDir: (path: string) => void
  onGoToEditor: () => void
}) {
  const isDir = entry.type === 'directory'
  const isReserved = isRoot && RESERVED_ROOT_FILES.includes(entry.name)
  const Icon = isDockerfile(entry.name) ? Hammer : (entryIcons[entry.type] ?? File)

  if (isReserved) {
    return (
      <button
        onClick={onGoToEditor}
        title="Edit in the Editor tab"
        className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs text-[var(--muted)] transition hover:text-[var(--text)]"
      >
        <Icon className="size-3.5" />
        <span className="truncate">{entry.name}</span>
        <span className="ml-auto text-xs">→ Editor</span>
      </button>
    )
  }

  return (
    <button
      onClick={() => isDir ? onNavigateDir(entry.path) : onOpenFile(entry.path)}
      className={cn(
        'flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs transition',
        isSelected ? 'bg-[rgba(245,165,36,0.14)] text-[var(--text)]' : 'text-[var(--muted)] hover:bg-[rgba(255,255,255,0.05)] hover:text-[var(--text)]',
      )}
    >
      <Icon className={cn('size-3.5', isDockerfile(entry.name) && 'text-[var(--warning)]')} />
      <span className="truncate">{entry.name}</span>
    </button>
  )
}
