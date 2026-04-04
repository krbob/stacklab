import { test, expect } from '@playwright/test'
import { login } from './helpers'

test.describe('Create and Delete Stack', () => {
  const testStackName = 'e2e-test-stack'

  test('creates a new stack and deletes it', async ({ page }) => {
    await login(page)

    // Navigate to create
    await page.getByRole('link', { name: 'New stack' }).click()
    await expect(page).toHaveURL(/\/stacks\/new/)

    // Fill in stack name
    await page.getByTestId('create-stack-name').fill(testStackName)

    // Should show the canonical path preview
    await expect(page.getByText(`/opt/stacklab/stacks/${testStackName}/compose.yaml`)).toBeVisible()

    // Submit
    await page.getByTestId('create-stack-submit').click()

    // Should navigate to the new stack detail
    await expect(page).toHaveURL(new RegExp(`/stacks/${testStackName}`), { timeout: 15_000 })

    // Stack should be visible — check heading
    await expect(page.getByText(testStackName)).toBeVisible()

    // Go back to dashboard to verify it appears in the list
    await page.getByRole('link', { name: 'Stacks' }).first().click()
    await expect(page.getByTestId(`stack-card-${testStackName}`)).toBeVisible()

    // Navigate back to the stack to delete it
    await page.getByTestId(`stack-card-${testStackName}`).click()

    // Click Remove
    await page.getByRole('button', { name: 'Remove' }).click()

    // Check "Delete stack definition" checkbox in the dialog
    await page.getByLabel('Delete stack definition').check()

    // Confirm deletion
    await page.getByTestId('delete-confirm').click()

    // Should redirect to stacks list after definition deletion
    await expect(page).toHaveURL(/\/stacks$/, { timeout: 15_000 })

    // The deleted stack should no longer appear
    await expect(page.getByTestId(`stack-card-${testStackName}`)).not.toBeVisible()
  })
})
