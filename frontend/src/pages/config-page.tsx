import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { File, FileQuestion, FileWarning, Folder, FolderKanban, Plus } from 'lucide-react'
import { getConfigTree, getConfigFile, saveConfigFile } from '@/lib/api-client'
import type { ConfigTreeEntry, ConfigFileResponse } from '@/lib/api-types'
import { YamlEditor } from '@/components/yaml-editor'
import { cn } from '@/lib/cn'

const entryIcons: Record<string, typeof File> = {
  directory: Folder,
  text_file: File,
  binary_file: FileWarning,
  unknown_file: FileQuestion,
}

export function ConfigPage() {
  const [treePath, setTreePath] = useState('')
  const [treeEntries, setTreeEntries] = useState<ConfigTreeEntry[]>([])
  const [parentPath, setParentPath] = useState<string | null>(null)
  const [treeLoading, setTreeLoading] = useState(true)
  const [treeError, setTreeError] = useState<string | null>(null)

  const [selectedFile, setSelectedFile] = useState<ConfigFileResponse | null>(null)
  const [fileLoading, setFileLoading] = useState(false)
  const [fileError, setFileError] = useState<string | null>(null)

  const [editContent, setEditContent] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveMessage, setSaveMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  const [creatingFile, setCreatingFile] = useState(false)
  const [newFileName, setNewFileName] = useState('')

  const isDirty = selectedFile?.type === 'text_file' && editContent !== (selectedFile.content ?? '')

  // Load tree
  const loadTree = useCallback(async (path: string) => {
    setTreeLoading(true)
    setTreeError(null)
    try {
      const result = await getConfigTree(path || undefined)
      setTreeEntries(result.items)
      setParentPath(result.parent_path)
      setTreePath(result.current_path)
    } catch (err) {
      setTreeError(err instanceof Error ? err.message : 'Failed to load tree')
    } finally {
      setTreeLoading(false)
    }
  }, [])

  useEffect(() => { loadTree('') }, [loadTree])

  // Open file
  const openFile = useCallback(async (path: string) => {
    setFileLoading(true)
    setFileError(null)
    setSaveMessage(null)
    setCreatingFile(false)
    try {
      const file = await getConfigFile(path)
      setSelectedFile(file)
      setEditContent(file.content ?? '')
    } catch (err) {
      setFileError(err instanceof Error ? err.message : 'Failed to load file')
      setSelectedFile(null)
    } finally {
      setFileLoading(false)
    }
  }, [])

  // Navigate into directory
  const navigateDir = useCallback((path: string) => {
    loadTree(path)
    setSelectedFile(null)
    setFileError(null)
    setSaveMessage(null)
    setCreatingFile(false)
  }, [loadTree])

  // Save file
  const handleSave = useCallback(async () => {
    if (!selectedFile) return
    setSaving(true)
    setSaveMessage(null)
    try {
      await saveConfigFile(selectedFile.path, editContent)
      setSaveMessage({ type: 'success', text: 'Saved' })
      // Reload file to get updated modified_at
      const updated = await getConfigFile(selectedFile.path)
      setSelectedFile(updated)
      setEditContent(updated.content ?? '')
    } catch (err) {
      setSaveMessage({ type: 'error', text: err instanceof Error ? err.message : 'Save failed' })
    } finally {
      setSaving(false)
    }
  }, [selectedFile, editContent])

  // Create new file
  const handleCreateFile = useCallback(async () => {
    if (!newFileName.trim()) return
    const path = treePath ? `${treePath}/${newFileName.trim()}` : newFileName.trim()
    setSaving(true)
    try {
      await saveConfigFile(path, '', false)
      setCreatingFile(false)
      setNewFileName('')
      await loadTree(treePath)
      await openFile(path)
    } catch (err) {
      setSaveMessage({ type: 'error', text: err instanceof Error ? err.message : 'Create failed' })
    } finally {
      setSaving(false)
    }
  }, [treePath, newFileName, loadTree, openFile])

  const handleDiscard = useCallback(() => {
    if (selectedFile) setEditContent(selectedFile.content ?? '')
  }, [selectedFile])

  return (
    <div className="flex gap-4" style={{ minHeight: 'calc(100vh - 120px)' }}>
      {/* Tree panel */}
      <div className="hidden w-64 shrink-0 flex-col rounded-[28px] border border-[var(--panel-border)] bg-[var(--panel)] p-4 shadow-[var(--shadow)] lg:flex">
        <div className="mb-3 text-xs uppercase tracking-wider text-[var(--accent)]">Config workspace</div>

        {treeLoading && (
          <div className="space-y-2">
            {[1, 2, 3].map((i) => (
              <div key={i} className="h-6 animate-pulse rounded bg-[rgba(255,255,255,0.05)]" />
            ))}
          </div>
        )}

        {treeError && <p className="text-xs text-red-400">{treeError}</p>}

        {!treeLoading && !treeError && (
          <nav className="flex-1 space-y-0.5 overflow-y-auto">
            {/* Breadcrumb / go up */}
            {parentPath !== null && (
              <button
                onClick={() => navigateDir(parentPath)}
                className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs text-[var(--muted)] transition hover:bg-[rgba(255,255,255,0.05)] hover:text-[var(--text)]"
              >
                <Folder className="size-3.5" />
                <span>.. (up)</span>
              </button>
            )}

            {treeEntries.map((entry) => {
              const Icon = entryIcons[entry.type] ?? File
              const isDir = entry.type === 'directory'
              const isSelected = selectedFile?.path === entry.path

              return (
                <button
                  key={entry.path}
                  onClick={() => isDir ? navigateDir(entry.path) : openFile(entry.path)}
                  className={cn(
                    'flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs transition',
                    isSelected
                      ? 'bg-[rgba(79,209,197,0.14)] text-[var(--text)]'
                      : 'text-[var(--muted)] hover:bg-[rgba(255,255,255,0.05)] hover:text-[var(--text)]',
                  )}
                >
                  {entry.stack_id && isDir && treePath === '' ? (
                    <FolderKanban className="size-3.5 text-[var(--accent)]" />
                  ) : (
                    <Icon className="size-3.5" />
                  )}
                  <span className="truncate">{entry.name}</span>
                </button>
              )
            })}

            {treeEntries.length === 0 && (
              <p className="px-2 py-4 text-xs text-[var(--muted)]">Empty directory</p>
            )}

            {/* New file button */}
            {!creatingFile && (
              <button
                onClick={() => { setCreatingFile(true); setNewFileName('') }}
                className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs text-[var(--muted)] transition hover:bg-[rgba(255,255,255,0.05)] hover:text-[var(--text)]"
              >
                <Plus className="size-3.5" />
                <span>New file</span>
              </button>
            )}

            {creatingFile && (
              <div className="flex items-center gap-1 px-2 py-1">
                <input
                  type="text"
                  value={newFileName}
                  onChange={(e) => setNewFileName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') handleCreateFile()
                    if (e.key === 'Escape') setCreatingFile(false)
                  }}
                  placeholder="filename"
                  autoFocus
                  className="w-full rounded border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] px-2 py-1 text-xs text-[var(--text)] outline-none focus:border-[rgba(79,209,197,0.35)]"
                />
              </div>
            )}
          </nav>
        )}
      </div>

      {/* Editor panel */}
      <div className="flex min-w-0 flex-1 flex-col rounded-[28px] border border-[var(--panel-border)] bg-[var(--panel)] p-5 shadow-[var(--shadow)]">
        {!selectedFile && !fileLoading && !fileError && (
          <div className="flex flex-1 items-center justify-center">
            <div className="text-center">
              <p className="text-lg text-[var(--text)]">Select a file to view or edit</p>
              <p className="mt-1 text-sm text-[var(--muted)]">
                Browse the config workspace in the tree on the left.
              </p>
            </div>
          </div>
        )}

        {fileLoading && (
          <div className="flex flex-1 items-center justify-center">
            <div className="text-sm text-[var(--muted)]">Loading file...</div>
          </div>
        )}

        {fileError && (
          <div className="rounded-2xl border border-red-400/20 bg-red-400/5 px-4 py-3 text-sm text-red-400">
            {fileError}
          </div>
        )}

        {selectedFile && (
          <>
            {/* File header */}
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <div className="flex items-center gap-2">
                  <h3 className="text-lg font-medium text-[var(--text)]">{selectedFile.name}</h3>
                  <span className="rounded-full border border-[var(--panel-border)] px-2 py-0.5 text-xs text-[var(--muted)]">
                    {selectedFile.type.replace('_', ' ')}
                  </span>
                </div>
                <div className="mt-1 flex items-center gap-3 text-xs text-[var(--muted)]">
                  <span>{selectedFile.path}</span>
                  {selectedFile.stack_id && (
                    <Link to={`/stacks/${selectedFile.stack_id}`} className="text-[var(--accent)] hover:underline">
                      {selectedFile.stack_id}
                    </Link>
                  )}
                  <span>{new Date(selectedFile.modified_at).toLocaleString()}</span>
                </div>
              </div>

              {selectedFile.type === 'text_file' && selectedFile.writable && (
                <div className="flex items-center gap-2">
                  {isDirty && (
                    <span className="text-xs text-amber-400">Unsaved changes</span>
                  )}
                  {isDirty && (
                    <button
                      onClick={handleDiscard}
                      className="rounded-full border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
                    >
                      Discard
                    </button>
                  )}
                  <button
                    data-testid="config-save"
                    onClick={handleSave}
                    disabled={saving || !isDirty}
                    className="rounded-full border border-[rgba(79,209,197,0.35)] bg-[rgba(79,209,197,0.14)] px-3 py-1 text-xs text-[var(--text)] disabled:opacity-40"
                  >
                    {saving ? 'Saving...' : 'Save'}
                  </button>
                </div>
              )}
            </div>

            {/* Save feedback */}
            {saveMessage && (
              <div className={cn(
                'mt-2 text-xs',
                saveMessage.type === 'success' ? 'text-emerald-400' : 'text-red-400',
              )}>
                {saveMessage.text}
              </div>
            )}

            {/* Editor / read-only content */}
            <div className="mt-3 flex-1" style={{ minHeight: '400px' }}>
              {selectedFile.type === 'text_file' ? (
                <YamlEditor
                  value={editContent}
                  onChange={setEditContent}
                  readOnly={!selectedFile.writable}
                />
              ) : (
                <div className="flex h-full items-center justify-center rounded-[16px] border border-[var(--panel-border)] bg-[rgba(0,0,0,0.2)]">
                  <div className="text-center">
                    <FileWarning className="mx-auto size-8 text-[var(--muted)]" />
                    <p className="mt-2 text-sm text-[var(--text)]">
                      {selectedFile.type === 'binary_file' ? 'Binary file' : 'Unknown file type'}
                    </p>
                    <p className="mt-1 text-xs text-[var(--muted)]">
                      This file cannot be edited in the browser. Size: {(selectedFile.size_bytes / 1024).toFixed(1)} KB
                    </p>
                  </div>
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  )
}
