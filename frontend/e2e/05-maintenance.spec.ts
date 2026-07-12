import { test, expect } from '@playwright/test'
import { createStackViaApi, deleteStackViaApi, invokeStackActionViaApi, login, waitForJobById } from './helpers'

const STACK_ID = 'e2e-maintenance'
const COMPOSE = `services:
  worker:
    image: alpine:3.20
    command: ["sh", "-c", "sleep 600"]
    volumes:
      - worker-data:/data

volumes:
  worker-data:
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

    await page.getByRole('tab', { name: 'Images' }).click()
    const imagesSection = page.getByRole('heading', { name: 'Images' }).locator('xpath=ancestor::section[1]')
    await expect(imagesSection).toBeVisible()
    await expect(imagesSection.getByRole('link', { name: STACK_ID })).toBeVisible({ timeout: 20_000 })

    await page.getByRole('tab', { name: 'Networks' }).click()
    await expect(page.getByRole('heading', { name: 'Networks' })).toBeVisible()
    await expect(page.getByText(`${STACK_ID}_default`)).toBeVisible({ timeout: 20_000 })

    await page.getByRole('tab', { name: 'Volumes' }).click()
    await expect(page.getByRole('heading', { name: 'Volumes' })).toBeVisible()
    await expect(page.getByText(`${STACK_ID}_worker-data`, { exact: true })).toBeVisible({ timeout: 20_000 })

    await page.getByRole('tab', { name: 'Cleanup' }).click()
    await expect(page.getByRole('heading', { name: 'Cleanup' })).toBeVisible()
    await expect(page.getByText('Total reclaimable:')).toBeVisible({ timeout: 20_000 })

    // Image and build-cache pruning can evict runner caches and exceed the test timeout.
    await page.getByRole('checkbox', { name: /Unused images/ }).uncheck()
    await page.getByRole('checkbox', { name: /Build cache/ }).uncheck()
    await page.getByRole('checkbox', { name: /Stopped containers/ }).check()

    const pruneResponse = page.waitForResponse((response) =>
      response.url().endsWith('/api/maintenance/prune') && response.request().method() === 'POST',
    )

    await page.getByTestId('maintenance-prune').click()
    const cleanupDialog = page.getByRole('dialog', { name: 'Review Docker cleanup' })
    await expect(cleanupDialog.getByRole('region', { name: 'Review operation' })).toContainText('Stopped containers')
    await cleanupDialog.getByRole('button', { name: 'Confirm cleanup' }).click()

    const pruneResult = await pruneResponse
    expect(pruneResult.ok()).toBeTruthy()
    const pruneJob = await pruneResult.json()
    await waitForJobById(page, pruneJob.job.id)
    await expect(page.getByTestId('maintenance-prune')).toHaveText('Run cleanup', { timeout: 20_000 })
  })
})
