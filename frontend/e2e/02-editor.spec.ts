import { test, expect } from '@playwright/test'
import { login, createStackViaApi, deleteStackViaApi, waitForAuditEntry, waitForJobById } from './helpers'

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
    const editor = page.locator('.cm-content')
    await editor.click()
    await page.keyboard.press('ControlOrMeta+A')
    await page.keyboard.insertText(`${COMPOSE}# saved by browser E2E\n`)

    const save = page.getByTestId('editor-save')
    await expect(save).toBeEnabled()

    const saveResponse = page.waitForResponse((response) =>
      response.url().endsWith(`/api/stacks/${EDITOR_STACK}/definition`) && response.request().method() === 'PUT',
    )
    await save.click()
    const saveResult = await saveResponse
    expect(saveResult.ok()).toBeTruthy()
    const saved = await saveResult.json()
    await waitForJobById(page, saved.job.id)

    // Wait for audit entry via API — deterministic, no timing guesswork
    await waitForAuditEntry(page, EDITOR_STACK, 'save_definition')

    // Now verify it's visible in the UI
    await page.goto(`/stacks/${EDITOR_STACK}/audit`)
    await expect(page.getByTestId('audit-row').filter({ hasText: 'save_definition' }).first()).toBeVisible()
  })
})
