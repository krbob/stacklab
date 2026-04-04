import { test, expect } from '@playwright/test'
import { login, createStackViaApi, deleteStackViaApi, waitForAuditEntry } from './helpers'

const AUDIT_STACK = 'e2e-audit'
const COMPOSE = `services:
  svc:
    image: alpine:latest
`

test.describe('Global Audit', () => {
  test('shows audit page', async ({ page }) => {
    await login(page)
    await page.getByRole('link', { name: 'Audit' }).click()
    await expect(page).toHaveURL(/\/audit/)
    await expect(page.getByText('Audit log')).toBeVisible()
  })

  test('shows audit entry after stack action', async ({ page }) => {
    await login(page)

    // Create a dedicated stack for audit test via API
    await createStackViaApi(page, AUDIT_STACK, COMPOSE)

    try {
      // Save definition to generate audit entry
      const cookies = await page.context().cookies()
      const sessionCookie = cookies.find((c) => c.name.startsWith('stacklab'))
      const baseURL = process.env.STACKLAB_E2E_URL ?? 'http://127.0.0.1:18081'

      await page.request.put(`${baseURL}/api/stacks/${AUDIT_STACK}/definition`, {
        data: { compose_yaml: COMPOSE, env: '', validate_after_save: true },
        headers: sessionCookie ? { Cookie: `${sessionCookie.name}=${sessionCookie.value}` } : {},
      })

      // Wait for audit entry to exist via API — deterministic
      await waitForAuditEntry(page, AUDIT_STACK, 'save_definition')

      // Now verify it's visible in global audit UI
      await page.goto('/audit')
      await expect(page.getByTestId('audit-row').first()).toBeVisible()
      await expect(page.getByText('save_definition')).toBeVisible()
    } finally {
      await deleteStackViaApi(page, AUDIT_STACK)
    }
  })
})
