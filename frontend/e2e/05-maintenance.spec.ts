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

    await page.getByRole('button', { name: 'Images' }).click()
    const imagesSection = page.getByRole('heading', { name: 'Images' }).locator('xpath=ancestor::section[1]')
    await expect(imagesSection).toBeVisible()
    await expect(imagesSection.getByRole('link', { name: STACK_ID })).toBeVisible({ timeout: 20_000 })

    await page.getByRole('button', { name: 'Networks' }).click()
    await expect(page.getByRole('heading', { name: 'Networks' })).toBeVisible()
    await expect(page.getByText(`${STACK_ID}_default`)).toBeVisible({ timeout: 20_000 })

    await page.getByRole('button', { name: 'Volumes' }).click()
    await expect(page.getByRole('heading', { name: 'Volumes' })).toBeVisible()
    await expect(page.getByText(`${STACK_ID}_worker-data`, { exact: true })).toBeVisible({ timeout: 20_000 })

    await page.getByRole('button', { name: 'Cleanup' }).click()
    await expect(page.getByRole('heading', { name: 'Cleanup' })).toBeVisible()
    await expect(page.getByText('Total reclaimable:')).toBeVisible({ timeout: 20_000 })

    const pruneResponse = page.waitForResponse((response) =>
      response.url().endsWith('/api/maintenance/prune') && response.request().method() === 'POST',
    )

    await page.getByTestId('maintenance-prune').click()
    await expect(page.getByTestId('maintenance-prune')).toHaveText('Cleaning...', { timeout: 5_000 })

    const pruneJob = await (await pruneResponse).json()
    await waitForJobById(page, pruneJob.job.id)
    await expect(page.getByTestId('maintenance-prune')).toHaveText('Run cleanup', { timeout: 20_000 })
  })
})
