import { RoutePlaceholder } from '@/components/route-placeholder'

export function CreateStackPage() {
  return (
    <RoutePlaceholder
      title="Create stack"
      summary="Scaffold for stack creation flow with canonical directory preview, validation, and optional deploy-after-create workflow."
      contract="POST /api/stacks"
      aside={
        <div className="rounded-2xl border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] p-4 text-sm text-[var(--muted)]">
          Canonical paths:
          <ul className="mt-3 space-y-2 font-mono text-xs text-[var(--text)]">
            <li>/opt/stacklab/stacks/&lt;stack&gt;/compose.yaml</li>
            <li>/opt/stacklab/config/&lt;stack&gt;/</li>
            <li>/opt/stacklab/data/&lt;stack&gt;/</li>
          </ul>
        </div>
      }
    />
  )
}
