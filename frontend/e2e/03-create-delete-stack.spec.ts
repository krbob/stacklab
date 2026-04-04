import { test, expect } from '@playwright/test'
import { login, deleteStackViaApi } from './helpers'

test.describe('Create and Delete Stack', () => {
  const TEST_STACK = 'e2e-crud'

  test.afterEach(async ({ page }) => {
    // Cleanup in case test failed before delete
    await deleteStackViaApi(page, TEST_STACK)
  })

  test('creates a new stack and deletes it', async ({ page }) => {
    await login(page)

    // Navigate to create
    await page.getByRole('link', { name: 'New stack' }).click()
    await expect(page).toHaveURL(/\/stacks\/new/)

    // Fill in stack name
    await page.getByTestId('create-stack-name').fill(TEST_STACK)

    // Should show the canonical path preview
    await expect(page.getByText(`/opt/stacklab/stacks/${TEST_STACK}/compose.yaml`)).toBeVisible()

    // Submit
    await page.getByTestId('create-stack-submit').click()

    // Should navigate to the new stack detail
    await expect(page).toHaveURL(new RegExp(`/stacks/${TEST_STACK}`), { timeout: 15_000 })
    await expect(page.getByText(TEST_STACK)).toBeVisible()

    // Go back to dashboard to verify it appears
    await page.getByRole('link', { name: 'Stacks' }).first().click()
    await expect(page.getByTestId(`stack-card-${TEST_STACK}`)).toBeVisible()

    // Navigate back to delete
    await page.getByTestId(`stack-card-${TEST_STACK}`).click()
    await page.getByRole('button', { name: 'Remove' }).click()

    // Check definition deletion checkbox
    await page.getByLabel('Delete stack definition').check()

    // Confirm
    await page.getByTestId('delete-confirm').click()

    // Should redirect to stacks list
    await expect(page).toHaveURL(/\/stacks$/, { timeout: 15_000 })

    // Deleted stack should be gone
    await expect(page.getByTestId(`stack-card-${TEST_STACK}`)).not.toBeVisible()
  })
})
