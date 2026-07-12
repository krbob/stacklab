import { test, expect } from '@playwright/test'
import { login } from './helpers'

test.describe('Docker Admin', () => {
  test('shows live engine, daemon config, and registry status', async ({ page }) => {
    await login(page)

    const overviewResponse = page.waitForResponse((response) =>
      response.url().endsWith('/api/docker/admin/overview')
      && response.request().method() === 'GET',
    )
    const daemonConfigResponse = page.waitForResponse((response) =>
      response.url().endsWith('/api/docker/admin/daemon-config')
      && response.request().method() === 'GET',
    )
    const registryResponse = page.waitForResponse((response) =>
      response.url().endsWith('/api/docker/registries')
      && response.request().method() === 'GET',
    )

    await page.goto('/docker')

    const [overviewResult, daemonConfigResult, registryResult] = await Promise.all([
      overviewResponse,
      daemonConfigResponse,
      registryResponse,
    ])

    expect(overviewResult.ok()).toBeTruthy()
    expect(daemonConfigResult.ok()).toBeTruthy()
    expect(registryResult.ok()).toBeTruthy()

    const overview = await overviewResult.json() as {
      engine: { available: boolean; version: string }
    }
    expect(overview.engine.available).toBe(true)

    await expect(page.getByRole('heading', { level: 1, name: 'Docker' })).toBeVisible()
    await expect(page.getByText(overview.engine.version, { exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { level: 2, name: 'daemon.json' })).toBeVisible()
    await expect(page.getByRole('heading', { level: 2, name: 'Managed settings' })).toBeVisible()
    await expect(page.getByRole('heading', { level: 2, name: 'Registry auth' })).toBeVisible()
  })
})
