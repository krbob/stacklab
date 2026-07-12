import { expect, test } from '@playwright/test'
import { login } from './helpers'

const STACK_ID = 'demo'
const FILES_CHUNK = '**/assets/stack-files-page-*.js'

test.use({ serviceWorkers: 'block' })

test.describe('Route error recovery', () => {
  test('recovers from a failed lazy route import with a full-page retry', async ({ page }) => {
    await login(page)
    await page.goto(`/stacks/${STACK_ID}`)
    await expect(page.getByRole('heading', { level: 1, name: STACK_ID })).toBeVisible()

    let blockedRequests = 0
    await page.route(FILES_CHUNK, async (route) => {
      blockedRequests += 1
      await route.abort('failed')
    })

    await page.getByRole('link', { name: 'Files', exact: true }).click()

    await expect(page).toHaveURL(new RegExp(`/stacks/${STACK_ID}/files$`))
    await expect(page.getByRole('heading', { level: 1, name: 'This view could not be displayed' })).toBeVisible()
    await expect(page.getByRole('alert')).toContainText('An unexpected application error occurred.')
    await expect(page.getByText('Unexpected Application Error!')).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'Retry' })).toHaveAttribute('href', `/stacks/${STACK_ID}/files`)
    expect(blockedRequests).toBeGreaterThan(0)

    await page.unroute(FILES_CHUNK)
    await page.getByRole('link', { name: 'Retry' }).click()

    await expect(page).toHaveURL(new RegExp(`/stacks/${STACK_ID}/files$`))
    await expect(page.getByRole('heading', { level: 1, name: STACK_ID })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Files', exact: true })).toHaveAttribute('aria-current', 'page')
    await expect(page.getByRole('alert')).toHaveCount(0)
  })
})
