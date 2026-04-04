import { test, expect } from '@playwright/test'
import { login, createStackViaApi, deleteStackViaApi, waitForAuditEntry } from './helpers'

const EDITOR_STACK = 'e2e-editor'
const COMPOSE = `services:
  web:
    image: nginx:alpine
`

test.describe('Editor', () => {
  test.beforeEach(async ({ page }) => {
    await login(page)
    // Create a dedicated stack for editor tests via API
    await createStackViaApi(page, EDITOR_STACK, COMPOSE)
    await page.goto(`/stacks/${EDITOR_STACK}/editor`)
  })

  test.afterEach(async ({ page }) => {
    await deleteStackViaApi(page, EDITOR_STACK)
  })

  test('loads compose.yaml in editor', async ({ page }) => {
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

    // Wait for audit entry via API — deterministic, no timing guesswork
    await waitForAuditEntry(page, EDITOR_STACK, 'save_definition')

    // Now verify it's visible in the UI
    await page.goto(`/stacks/${EDITOR_STACK}/audit`)
    await expect(page.getByText('save_definition')).toBeVisible()
  })
})
