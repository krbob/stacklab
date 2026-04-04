import { test, expect } from '@playwright/test'
import { login } from './helpers'

test.describe('Global Audit', () => {
  test('shows audit page with entries', async ({ page }) => {
    await login(page)

    // Navigate to audit
    await page.getByRole('link', { name: 'Audit' }).click()
    await expect(page).toHaveURL(/\/audit/)
    await expect(page.getByText('Audit log')).toBeVisible()
  })

  test('shows audit entries after performing an action', async ({ page }) => {
    await login(page)

    // First, perform an action to generate audit entry — save editor
    await page.getByTestId('stack-card-demo').click()
    await page.getByRole('link', { name: 'Editor' }).click()
    await page.getByTestId('editor-save').click()

    // Wait for save to complete
    await page.waitForTimeout(2000)

    // Go to global audit
    await page.getByRole('link', { name: 'Audit' }).click()
    await expect(page).toHaveURL(/\/audit/)

    // Should have at least one audit row
    await expect(page.getByTestId('audit-row').first()).toBeVisible({ timeout: 10_000 })
  })
})
