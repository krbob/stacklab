import { RoutePlaceholder } from '@/components/route-placeholder'

export function SettingsPage() {
  return (
    <RoutePlaceholder
      title="Settings"
      summary="Authentication settings, feature flags, and environment metadata live here."
      contract="GET /api/meta · POST /api/settings/password"
    />
  )
}
