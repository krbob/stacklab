import { RoutePlaceholder } from '@/components/route-placeholder'

export function GlobalAuditPage() {
  return (
    <RoutePlaceholder
      title="Global audit"
      summary="Chronological system-wide history of mutating stack actions and terminal metadata events."
      contract="GET /api/audit"
    />
  )
}
