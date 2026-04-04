import { test, expect } from '@playwright/test'
import { createStackViaApi, deleteStackViaApi, invokeStackActionViaApi, login } from './helpers'

const STACK_ID = 'e2e-maintenance'
const COMPOSE = `services:
  worker:
    image: alpine:3.20
    command: ["sh", "-c", "sleep 600"]
`

test.describe('Maintenance', () => {
  test.afterEach(async ({ page }) => {
    await deleteStackViaApi(page, STACK_ID)
  })

  test('shows image inventory and runs cleanup workflow', async ({ page }) => {
    await login(page)
    await createStackViaApi(page, STACK_ID, COMPOSE)
    await invokeStackActionViaApi(page, STACK_ID, 'up')

    await page.goto('/maintenance')
    await expect(page).toHaveURL(/\/maintenance/)

    await page.getByRole('button', { name: 'Images' }).click()
    await expect(page.getByRole('heading', { name: 'Images' })).toBeVisible()
    await expect(page.getByText(STACK_ID)).toBeVisible({ timeout: 20_000 })

    await page.getByRole('button', { name: 'Cleanup' }).click()
    await expect(page.getByRole('heading', { name: 'Cleanup' })).toBeVisible()
    await expect(page.getByText('Total reclaimable:')).toBeVisible({ timeout: 20_000 })

    await page.getByTestId('maintenance-prune').click()
    await expect(page.getByText(/Running|Succeeded/)).toBeVisible({ timeout: 20_000 })
    await expect(page.getByRole('heading', { name: 'Progress' })).toBeVisible()
  })
})
