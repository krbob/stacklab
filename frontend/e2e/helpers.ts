import { type Page } from '@playwright/test'

const PASSWORD = process.env.STACKLAB_E2E_PASSWORD ?? 'stacklab-e2e'

export async function login(page: Page) {
  await page.goto('/login')
  await page.getByTestId('login-password').fill(PASSWORD)
  await page.getByTestId('login-submit').click()
  await page.waitForURL('**/stacks')
}
