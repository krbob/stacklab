import { test, expect } from '@playwright/test'
import { login } from './helpers'

test.describe('Login and Dashboard', () => {
  test('shows login page when unauthenticated', async ({ page }) => {
    await page.goto('/stacks')
    await expect(page).toHaveURL(/\/login/)
    await expect(page.getByTestId('login-password')).toBeVisible()
    await expect(page.getByTestId('login-submit')).toBeVisible()
  })

  test('rejects invalid password', async ({ page }) => {
    await page.goto('/login')
    await page.getByTestId('login-password').fill('wrong-password')
    await page.getByTestId('login-submit').click()
    await expect(page.getByText('Invalid password')).toBeVisible()
  })

  test('logs in and shows dashboard with seeded stack', async ({ page }) => {
    await login(page)
    await expect(page.getByRole('heading', { name: 'Stacks' })).toBeVisible()
    await expect(page.getByTestId('stack-card-demo')).toBeVisible()
  })

  test('navigates to stack detail from dashboard', async ({ page }) => {
    await login(page)
    await page.getByTestId('stack-card-demo').click()
    await expect(page).toHaveURL(/\/stacks\/demo/)
    await expect(page.getByText('demo')).toBeVisible()
  })
})
