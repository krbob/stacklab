import { test, expect } from '@playwright/test'
import { login } from './helpers'

test.describe('Editor', () => {
  test.beforeEach(async ({ page }) => {
    await login(page)
    await page.getByTestId('stack-card-demo').click()
    await page.getByRole('link', { name: 'Editor' }).click()
    await expect(page).toHaveURL(/\/stacks\/demo\/editor/)
  })

  test('loads compose.yaml in editor', async ({ page }) => {
    // CodeMirror renders the content — look for service name from fixture
    await expect(page.locator('.cm-content')).toBeVisible()
    await expect(page.locator('.cm-content')).toContainText('nginx')
  })

  test('shows resolved config preview', async ({ page }) => {
    await expect(page.getByText('Resolved config')).toBeVisible()
  })

  test('save button is visible', async ({ page }) => {
    await expect(page.getByTestId('editor-save')).toBeVisible()
    await expect(page.getByTestId('editor-save-deploy')).toBeVisible()
  })

  test('saving creates audit entry', async ({ page }) => {
    await page.getByTestId('editor-save').click()

    // Navigate to stack history
    await page.getByRole('link', { name: 'History' }).click()
    await expect(page).toHaveURL(/\/stacks\/demo\/audit/)

    // Should see save_definition action in audit
    await expect(page.getByText('save_definition')).toBeVisible({ timeout: 10_000 })
  })
})
