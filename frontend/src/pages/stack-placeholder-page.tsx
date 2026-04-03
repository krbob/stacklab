import { useParams } from 'react-router-dom'

import { RoutePlaceholder } from '@/components/route-placeholder'

type StackPlaceholderPageProps = {
  title: string
  summary: string
  contract: string
}

export function StackPlaceholderPage({
  title,
  summary,
  contract,
}: StackPlaceholderPageProps) {
  const { stackId = 'stack' } = useParams()

  return (
    <RoutePlaceholder
      title={`${title} · ${stackId}`}
      summary={summary}
      contract={contract}
      aside={
        <div className="rounded-2xl border border-[var(--panel-border)] bg-[rgba(255,255,255,0.03)] p-4 text-sm text-[var(--muted)]">
          This placeholder route is nested under the stack layout and already receives the route param
          <code className="mx-1 rounded bg-[rgba(255,255,255,0.06)] px-2 py-1 font-mono text-[var(--text)]">stackId</code>.
        </div>
      }
    />
  )
}
