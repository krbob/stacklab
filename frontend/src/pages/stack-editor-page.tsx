import { useCallback, useEffect, useRef, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import type { DefinitionResponse, StackDetailResponse } from '@/lib/api-types'
import { getDefinition, getResolvedConfig, invokeAction, resolveConfigDraft, saveDefinition } from '@/lib/api-client'
import { YamlEditor } from '@/components/yaml-editor'
import { ProgressPanel } from '@/components/progress-panel'
import { cn } from '@/lib/cn'
import { UnsavedChangesGuard } from '@/components/unsaved-changes-guard'
import { ConfirmDialog } from '@/components/confirm-dialog'

type ActiveTab = 'compose' | 'env'
type DraftValidationState = 'valid' | 'invalid' | 'stale'
type DefinitionRevision = {
  compose_modified_at: string
  env_modified_at: string | null
}

function revisionFromDefinition(definition: DefinitionResponse): DefinitionRevision {
  return {
    compose_modified_at: definition.files.compose_yaml.modified_at,
    env_modified_at: definition.files.env.modified_at,
  }
}

export function StackEditorPage() {
  const { stack, refetch } = useOutletContext<{
    stack: StackDetailResponse['stack']
    refetch: () => void
  }>()

  const [activeTab, setActiveTab] = useState<ActiveTab>('compose')
  const [composeYaml, setComposeYaml] = useState('')
  const [envContent, setEnvContent] = useState('')
  const [envExists, setEnvExists] = useState(false)
  const [savedCompose, setSavedCompose] = useState('')
  const [savedEnv, setSavedEnv] = useState('')
  const [definitionRevision, setDefinitionRevision] = useState<DefinitionRevision | null>(null)

  const [resolvedContent, setResolvedContent] = useState('')
  const [resolvedSource, setResolvedSource] = useState<'current' | 'draft' | 'last_valid'>('current')
  const [warnings, setWarnings] = useState<import('@/lib/api-types').ComposeWarning[]>([])
  const [resolvedError, setResolvedError] = useState('')
  const [resolvedLoadError, setResolvedLoadError] = useState<Error | null>(null)
  const [draftValidationState, setDraftValidationState] = useState<DraftValidationState>('stale')
  const [draftValidationMessage, setDraftValidationMessage] = useState('Preview current changes before deploy')

  const [saving, setSaving] = useState(false)
  const [activeJobId, setActiveJobId] = useState<string | null>(null)
  const [pendingDeploy, setPendingDeploy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loadingDef, setLoadingDef] = useState(true)
  const [definitionError, setDefinitionError] = useState<string | null>(null)
  const [definitionLoadAttempt, setDefinitionLoadAttempt] = useState(0)
  const [loadingResolved, setLoadingResolved] = useState(true)
  const [confirmDiscard, setConfirmDiscard] = useState(false)
  const resolvedRequestIDRef = useRef(0)

  const isDirty = composeYaml !== savedCompose || envContent !== savedEnv
  const isDirtyRef = useRef(isDirty)

  useEffect(() => {
    isDirtyRef.current = isDirty
  }, [isDirty])

  const markDraftStale = useCallback(() => {
    setDraftValidationState('stale')
    setDraftValidationMessage('Preview current changes before deploy')
  }, [])

  const handleComposeChange = useCallback((value: string) => {
    setComposeYaml(value)
    markDraftStale()
  }, [markDraftStale])

  const handleEnvChange = useCallback((value: string) => {
    setEnvContent(value)
    markDraftStale()
  }, [markDraftStale])

  // The editable definition is mandatory. Never expose an editor until it has
  // loaded successfully, so a request failure cannot look like an empty file.
  useEffect(() => {
    let cancelled = false
    setLoadingDef(true)
    setDefinitionError(null)
    setDefinitionRevision(null)
    getDefinition(stack.id).then((def) => {
      if (cancelled) return
      setComposeYaml(def.files.compose_yaml.content)
      setSavedCompose(def.files.compose_yaml.content)
      setEnvContent(def.files.env.content)
      setSavedEnv(def.files.env.content)
      setEnvExists(def.files.env.exists)
      setDefinitionRevision(revisionFromDefinition(def))
    }).catch((err) => {
      if (cancelled) return
      setDefinitionError(err instanceof Error ? err.message : 'Failed to load stack definition')
    }).finally(() => {
      if (!cancelled) setLoadingDef(false)
    })
    return () => { cancelled = true }
  }, [stack.id, definitionLoadAttempt])

  const loadResolvedConfig = useCallback(async (reset: boolean) => {
    const requestID = resolvedRequestIDRef.current + 1
    resolvedRequestIDRef.current = requestID
    setLoadingResolved(true)
    setResolvedLoadError(null)
    if (reset) {
      setResolvedContent('')
      setResolvedSource('current')
      setResolvedError('')
      setWarnings([])
      setDraftValidationState('stale')
      setDraftValidationMessage('Preview current changes before deploy')
    }

    try {
      const resolved = await getResolvedConfig(stack.id)
      if (resolvedRequestIDRef.current !== requestID) return
      if (resolved.valid && resolved.content) {
        setResolvedContent(resolved.content)
        setResolvedSource('current')
        setResolvedError('')
        setWarnings(resolved.warnings ?? [])
        if (!isDirtyRef.current) {
          setDraftValidationState('valid')
          setDraftValidationMessage('')
        }
      } else if (resolved.error) {
        setResolvedContent('')
        setResolvedSource('current')
        setResolvedError(resolved.error.message)
        if (!isDirtyRef.current) {
          setDraftValidationState('invalid')
          setDraftValidationMessage(resolved.error.message)
        }
      }
    } catch (err) {
      if (resolvedRequestIDRef.current !== requestID) return
      const message = err instanceof Error ? err.message : 'Resolved preview is unavailable'
      setResolvedLoadError(new Error(`${reset ? 'Failed to load' : 'Failed to refresh'} resolved preview: ${message}`))
    } finally {
      if (resolvedRequestIDRef.current === requestID) setLoadingResolved(false)
    }
  }, [stack.id])

  // Resolved config is an optional preview. Its failure must not hide a
  // successfully loaded definition or replace it with an empty editor.
  useEffect(() => {
    void loadResolvedConfig(true)
    return () => {
      resolvedRequestIDRef.current += 1
    }
  }, [loadResolvedConfig, definitionLoadAttempt])

  const previewDraft = useCallback(async () => {
    resolvedRequestIDRef.current += 1
    setLoadingResolved(false)
    setResolvedLoadError(null)
    try {
      const result = await resolveConfigDraft(stack.id, {
        compose_yaml: composeYaml,
        env: envContent,
      })
      if (result.valid && result.content) {
        setResolvedContent(result.content)
        setResolvedSource('draft')
        setResolvedError('')
        setWarnings(result.warnings ?? [])
        setDraftValidationState('valid')
        setDraftValidationMessage('')
        return true
      } else if (result.error) {
        setResolvedContent('')
        setResolvedSource('draft')
        setResolvedError(result.error.message)
        setDraftValidationState('invalid')
        setDraftValidationMessage(result.error.message)
        return false
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Preview failed'
      setResolvedError(message)
      setResolvedSource('draft')
      setDraftValidationState('invalid')
      setDraftValidationMessage(message)
      return false
    }
    return false
  }, [stack.id, composeYaml, envContent])

  // Preview draft
  const handlePreview = useCallback(async () => {
    await previewDraft()
  }, [previewDraft])

  const handleLastValid = useCallback(async () => {
    resolvedRequestIDRef.current += 1
    setLoadingResolved(false)
    setResolvedLoadError(null)
    try {
      const result = await getResolvedConfig(stack.id, 'last_valid')
      if (result.valid && result.content) {
        setResolvedContent(result.content)
        setResolvedSource('last_valid')
        setResolvedError('')
        setWarnings(result.warnings ?? [])
      } else if (result.error) {
        setResolvedContent('')
        setResolvedSource('last_valid')
        setResolvedError(result.error.message)
      }
    } catch (err) {
      setResolvedContent('')
      setResolvedSource('last_valid')
      setResolvedError(err instanceof Error ? err.message : 'Last deployed config is unavailable')
    }
  }, [stack.id])

  // Save (and optionally deploy after save completes)
  const handleSave = useCallback(async (deploy: boolean) => {
    if (!definitionRevision || !isDirty) return
    setSaving(true)
    setError(null)
    setResolvedLoadError(null)
    setActiveJobId(null)
    setPendingDeploy(deploy)
    try {
      const submittedCompose = composeYaml
      const submittedEnv = envContent
      const result = await saveDefinition(stack.id, {
        compose_yaml: submittedCompose,
        env: submittedEnv,
        validate_after_save: true,
        expected_revision: definitionRevision ?? undefined,
      })
      if (result.definition) {
        setSavedCompose(result.definition.files.compose_yaml.content)
        setSavedEnv(result.definition.files.env.content)
        setEnvExists(result.definition.files.env.exists)
        setDefinitionRevision(revisionFromDefinition(result.definition))
        setComposeYaml((current) => current === submittedCompose ? result.definition!.files.compose_yaml.content : current)
        setEnvContent((current) => current === submittedEnv ? result.definition!.files.env.content : current)
      } else {
        setSavedCompose(submittedCompose)
        setSavedEnv(submittedEnv)
      }
      setActiveJobId(result.job.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed')
      setPendingDeploy(false)
    } finally {
      setSaving(false)
    }
  }, [stack.id, composeYaml, envContent, definitionRevision, isDirty])

  const handleSaveAndDeploy = useCallback(async () => {
    if (!definitionRevision || !isDirty) return
    if (draftValidationState !== 'valid') {
      setSaving(true)
      const valid = await previewDraft()
      setSaving(false)
      if (!valid) return
    }
    await handleSave(true)
  }, [definitionRevision, isDirty, draftValidationState, previewDraft, handleSave])

  const handleDiscard = useCallback(() => {
    setComposeYaml(savedCompose)
    setEnvContent(savedEnv)
    markDraftStale()
  }, [savedCompose, savedEnv, markDraftStale])

  const handleJobDone = useCallback(async (state: string) => {
    if (state === 'succeeded') {
      refetch()
      void loadResolvedConfig(false)

      // Chain deploy if Save & Deploy was requested
      if (pendingDeploy) {
        setPendingDeploy(false)
        try {
          const deployResult = await invokeAction(stack.id, 'up')
          setActiveJobId(deployResult.job.id)
        } catch (err) {
          setError(err instanceof Error ? err.message : 'Deploy failed after save')
        }
      }
    } else {
      // Save failed — don't chain deploy
      setPendingDeploy(false)
    }
  }, [refetch, stack.id, pendingDeploy, loadResolvedConfig])

  if (loadingDef) {
    return (
      <div className="animate-pulse space-y-3">
        <div className="h-8 w-40 rounded bg-[rgba(255,255,255,0.05)]" />
        <div className="h-96 rounded bg-[rgba(255,255,255,0.03)]" />
      </div>
    )
  }

  if (definitionError || !definitionRevision) {
    return (
      <div className="rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-4" role="alert">
        <h2 className="text-sm font-medium text-[var(--danger)]">Stack definition could not be loaded</h2>
        <p className="mt-1 text-xs text-[var(--muted)]">{definitionError ?? 'The definition response did not include a revision.'}</p>
        <button
          type="button"
          onClick={() => setDefinitionLoadAttempt((attempt) => attempt + 1)}
          className="mt-3 rounded-md border border-[var(--panel-border)] px-3 py-1.5 text-xs text-[var(--text)] hover:border-[var(--danger)]/40"
        >
          Retry
        </button>
      </div>
    )
  }

  const saveDisabled = saving || !isDirty || stack.activity_state === 'locked'

  return (
    <div className="flex flex-col gap-3">
      <UnsavedChangesGuard when={isDirty} />

      {error && (
        <div className="rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">
          {error}
        </div>
      )}

      {/* Tab selector + actions */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex gap-1" role="tablist" aria-label="Definition files">
          <button
            id="definition-tab-compose"
            role="tab"
            aria-selected={activeTab === 'compose'}
            aria-controls="definition-source-panel"
            tabIndex={activeTab === 'compose' ? 0 : -1}
            onClick={() => setActiveTab('compose')}
            onKeyDown={(event) => {
              if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight' && event.key !== 'End') return
              event.preventDefault()
              setActiveTab('env')
              document.getElementById('definition-tab-env')?.focus()
            }}
            className={cn(
              'rounded-md border px-3 py-1 text-xs transition',
              activeTab === 'compose'
                ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                : 'border-[var(--panel-border)] text-[var(--muted)]',
            )}
          >
            compose.yaml
          </button>
          <button
            id="definition-tab-env"
            role="tab"
            aria-selected={activeTab === 'env'}
            aria-controls="definition-source-panel"
            tabIndex={activeTab === 'env' ? 0 : -1}
            onClick={() => setActiveTab('env')}
            onKeyDown={(event) => {
              if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight' && event.key !== 'Home') return
              event.preventDefault()
              setActiveTab('compose')
              document.getElementById('definition-tab-compose')?.focus()
            }}
            className={cn(
              'rounded-md border px-3 py-1 text-xs transition',
              activeTab === 'env'
                ? 'border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] text-[var(--text)]'
                : 'border-[var(--panel-border)] text-[var(--muted)]',
            )}
          >
            .env {!envExists && '(new)'}
          </button>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <button
            onClick={handlePreview}
            disabled={saving}
            className="rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)] disabled:opacity-40"
          >
            Preview
          </button>
          <button
            onClick={handleLastValid}
            disabled={!stack.last_deployed_at}
            className="rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)] disabled:opacity-40"
          >
            Last deployed
          </button>
          {isDirty && (
            <button
              onClick={() => setConfirmDiscard(true)}
              className="rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
            >
              Discard
            </button>
          )}
          <button
            data-testid="editor-save"
            onClick={() => handleSave(false)}
            disabled={saveDisabled}
            className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-3 py-1 text-xs text-[var(--text)] disabled:opacity-40"
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
          <button
            data-testid="editor-save-deploy"
            onClick={handleSaveAndDeploy}
            disabled={saveDisabled}
            className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-3 py-1 text-xs text-[var(--text)] disabled:opacity-40"
          >
            Save & Deploy
          </button>
        </div>
      </div>

      {confirmDiscard && (
        <ConfirmDialog
          title="Discard unsaved changes?"
          message="This reverts compose.yaml and .env to the last loaded version."
          items={['compose.yaml', '.env']}
          confirmLabel="Discard changes"
          onCancel={() => setConfirmDiscard(false)}
          onConfirm={() => {
            handleDiscard()
            setConfirmDiscard(false)
          }}
        />
      )}

      {/* Validation status */}
      <div className="flex items-center gap-2 text-xs">
        {draftValidationState === 'valid' ? (
          <span className="text-[var(--ok)]">✓ Config valid</span>
        ) : draftValidationState === 'stale' ? (
          <span className="text-[var(--warning)]">Preview current changes before deploy</span>
        ) : (
          <span className="text-[var(--danger)]">✗ {draftValidationMessage || 'Invalid config'}</span>
        )}
        {isDirty && <span className="text-[var(--warning)]">· Unsaved changes</span>}
      </div>

      {/* Advisory lint findings — never block save/deploy (Slice E) */}
      {warnings.length > 0 && (
        <div className="flex flex-col gap-1 rounded-md border border-[var(--warning)]/25 bg-[var(--warning)]/5 px-3 py-2 text-xs text-[var(--warning)]">
          {warnings.map((warning, index) => (
            <span key={index}>⚠ {warning.message}</span>
          ))}
        </div>
      )}

      {/* Editor split */}
      <div className="grid gap-3 xl:h-[min(72vh,720px)] xl:grid-cols-2">
        <div
          id="definition-source-panel"
          role="tabpanel"
          aria-labelledby={`definition-tab-${activeTab}`}
          className="h-[min(55vh,560px)] min-h-[320px] min-w-0 xl:h-auto xl:min-h-0"
        >
          {activeTab === 'compose' ? (
            <YamlEditor
              value={composeYaml}
              onChange={handleComposeChange}
            />
          ) : (
            <YamlEditor
              value={envContent}
              onChange={handleEnvChange}
            />
          )}
        </div>
        <div data-testid="resolved-preview" aria-busy={loadingResolved} className="max-h-[min(55vh,560px)] min-h-[260px] min-w-0 overflow-auto rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.3)] p-3 font-mono text-xs text-[var(--muted)] xl:max-h-none xl:min-h-0">
          <div className="mb-2 text-[var(--accent)] text-xs uppercase tracking-wider">
            {resolvedSource === 'last_valid' ? 'Last deployed config' : resolvedSource === 'draft' ? 'Draft resolved config' : 'Resolved config'}
          </div>
          {loadingResolved && !resolvedContent && !resolvedError ? (
            <span role="status" aria-live="polite" aria-atomic="true">Loading resolved preview...</span>
          ) : (
            <>
              {loadingResolved && (
                <p className="mb-2" role="status" aria-live="polite">Refreshing resolved preview...</p>
              )}
              {resolvedLoadError && (
                <div className="mb-2 rounded-md border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-3 py-2" role="alert">
                  <p className="text-[var(--danger)]">{resolvedLoadError.message}</p>
                  {(resolvedContent || resolvedError) && (
                    <p className="mt-1 text-[var(--muted)]">Showing the last successfully loaded preview.</p>
                  )}
                  <button
                    type="button"
                    disabled={loadingResolved}
                    onClick={() => { void loadResolvedConfig(false) }}
                    className="mt-2 rounded-md border border-[var(--danger)]/30 px-2 py-1 text-[var(--danger)] hover:bg-[var(--danger)]/10 disabled:opacity-40"
                  >
                    Retry resolved preview
                  </button>
                </div>
              )}
              {resolvedContent ? (
                <pre className="whitespace-pre-wrap break-words text-[var(--text)]">{resolvedContent}</pre>
              ) : resolvedError ? (
                <pre className="text-[var(--danger)]">{resolvedError}</pre>
              ) : !resolvedLoadError ? (
                <span>Click "Preview" to resolve current editor contents.</span>
              ) : null}
            </>
          )}
        </div>
      </div>

      {/* Progress panel */}
      {activeJobId && (
        <ProgressPanel jobId={activeJobId} onDone={handleJobDone} />
      )}
    </div>
  )
}
