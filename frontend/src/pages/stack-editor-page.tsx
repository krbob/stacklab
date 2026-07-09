import { useCallback, useEffect, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import type { StackDetailResponse } from '@/lib/api-types'
import { getDefinition, getResolvedConfig, invokeAction, resolveConfigDraft, saveDefinition } from '@/lib/api-client'
import { YamlEditor } from '@/components/yaml-editor'
import { ProgressPanel } from '@/components/progress-panel'
import { cn } from '@/lib/cn'
import { UnsavedChangesGuard } from '@/components/unsaved-changes-guard'

type ActiveTab = 'compose' | 'env'
type DraftValidationState = 'valid' | 'invalid' | 'stale'

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

  const [resolvedContent, setResolvedContent] = useState('')
  const [resolvedSource, setResolvedSource] = useState<'current' | 'draft' | 'last_valid'>('current')
  const [warnings, setWarnings] = useState<import('@/lib/api-types').ComposeWarning[]>([])
  const [resolvedError, setResolvedError] = useState('')
  const [draftValidationState, setDraftValidationState] = useState<DraftValidationState>('stale')
  const [draftValidationMessage, setDraftValidationMessage] = useState('Preview current changes before deploy')

  const [saving, setSaving] = useState(false)
  const [activeJobId, setActiveJobId] = useState<string | null>(null)
  const [pendingDeploy, setPendingDeploy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loadingDef, setLoadingDef] = useState(true)

  const isDirty = composeYaml !== savedCompose || envContent !== savedEnv
  const canDeployDraft = draftValidationState === 'valid'

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

  // Load definition
  useEffect(() => {
    let cancelled = false
    setLoadingDef(true)
    Promise.all([
      getDefinition(stack.id),
      getResolvedConfig(stack.id),
    ]).then(([def, resolved]) => {
      if (cancelled) return
      setComposeYaml(def.files.compose_yaml.content)
      setSavedCompose(def.files.compose_yaml.content)
      setEnvContent(def.files.env.content)
      setSavedEnv(def.files.env.content)
      setEnvExists(def.files.env.exists)
      if (resolved.valid && resolved.content) {
        setResolvedContent(resolved.content)
        setResolvedSource('current')
        setResolvedError('')
        setWarnings(resolved.warnings ?? [])
        setDraftValidationState('valid')
        setDraftValidationMessage('')
      } else if (resolved.error) {
        setResolvedContent('')
        setResolvedSource('current')
        setResolvedError(resolved.error.message)
        setDraftValidationState('invalid')
        setDraftValidationMessage(resolved.error.message)
      }
    }).catch((err) => {
      if (cancelled) return
      setError(err.message)
    }).finally(() => {
      if (!cancelled) setLoadingDef(false)
    })
    return () => { cancelled = true }
  }, [stack.id])

  // Preview draft
  const handlePreview = useCallback(async () => {
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
      } else if (result.error) {
        setResolvedContent('')
        setResolvedSource('draft')
        setResolvedError(result.error.message)
        setDraftValidationState('invalid')
        setDraftValidationMessage(result.error.message)
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Preview failed'
      setResolvedError(message)
      setResolvedSource('draft')
      setDraftValidationState('invalid')
      setDraftValidationMessage(message)
    }
  }, [stack.id, composeYaml, envContent])

  const handleLastValid = useCallback(async () => {
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
    setSaving(true)
    setError(null)
    setActiveJobId(null)
    setPendingDeploy(deploy)
    try {
      const result = await saveDefinition(stack.id, {
        compose_yaml: composeYaml,
        env: envContent,
        validate_after_save: true,
      })
      setSavedCompose(composeYaml)
      setSavedEnv(envContent)
      setActiveJobId(result.job.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed')
      setPendingDeploy(false)
    } finally {
      setSaving(false)
    }
  }, [stack.id, composeYaml, envContent])

  const handleDiscard = useCallback(() => {
    setComposeYaml(savedCompose)
    setEnvContent(savedEnv)
    markDraftStale()
  }, [savedCompose, savedEnv, markDraftStale])

  const handleJobDone = useCallback(async (state: string) => {
    if (state === 'succeeded') {
      refetch()
      // Refresh resolved config
      getResolvedConfig(stack.id).then((resolved) => {
        if (resolved.valid && resolved.content) {
          setResolvedContent(resolved.content)
          setResolvedSource('current')
          setResolvedError('')
          setWarnings(resolved.warnings ?? [])
          setDraftValidationState('valid')
          setDraftValidationMessage('')
        } else if (resolved.error) {
          setResolvedContent('')
          setResolvedSource('current')
          setResolvedError(resolved.error.message)
          setDraftValidationState('invalid')
          setDraftValidationMessage(resolved.error.message)
        }
      }).catch(() => {})

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
  }, [refetch, stack.id, pendingDeploy])

  if (loadingDef) {
    return (
      <div className="animate-pulse space-y-3">
        <div className="h-8 w-40 rounded bg-[rgba(255,255,255,0.05)]" />
        <div className="h-96 rounded bg-[rgba(255,255,255,0.03)]" />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      <UnsavedChangesGuard when={isDirty && !saving} />

      {error && (
        <div className="rounded-lg border border-[var(--danger)]/20 bg-[var(--danger)]/5 px-4 py-3 text-sm text-[var(--danger)]">
          {error}
        </div>
      )}

      {/* Tab selector + actions */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex gap-1">
          <button
            onClick={() => setActiveTab('compose')}
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
            onClick={() => setActiveTab('env')}
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
            className="rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
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
              onClick={handleDiscard}
              className="rounded-md border border-[var(--panel-border)] px-3 py-1 text-xs text-[var(--muted)] hover:text-[var(--text)]"
            >
              Discard
            </button>
          )}
          <button
            data-testid="editor-save"
            onClick={() => handleSave(false)}
            disabled={saving || stack.activity_state === 'locked'}
            className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-3 py-1 text-xs text-[var(--text)] disabled:opacity-40"
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
          <button
            data-testid="editor-save-deploy"
            onClick={() => handleSave(true)}
            disabled={saving || !canDeployDraft || stack.activity_state === 'locked'}
            className="rounded-md border border-[rgba(245,165,36,0.35)] bg-[rgba(245,165,36,0.14)] px-3 py-1 text-xs text-[var(--text)] disabled:opacity-40"
          >
            Save & Deploy
          </button>
        </div>
      </div>

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
      <div className="grid gap-3 xl:grid-cols-2" style={{ height: '500px' }}>
        <div className="min-h-0 min-w-0">
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
        <div className="min-h-0 min-w-0 overflow-auto rounded border border-[var(--panel-border)] bg-[rgba(0,0,0,0.3)] p-3 font-mono text-xs text-[var(--muted)]">
          <div className="mb-2 text-[var(--accent)] text-xs uppercase tracking-wider">
            {resolvedSource === 'last_valid' ? 'Last deployed config' : resolvedSource === 'draft' ? 'Draft resolved config' : 'Resolved config'}
          </div>
          {resolvedContent ? (
            <pre className="whitespace-pre-wrap break-words text-[var(--text)]">{resolvedContent}</pre>
          ) : resolvedError ? (
            <pre className="text-[var(--danger)]">{resolvedError}</pre>
          ) : (
            <span>Click "Preview" to resolve current editor contents.</span>
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
