import { type Page, expect } from '@playwright/test'

const PASSWORD = process.env.STACKLAB_E2E_PASSWORD ?? 'stacklab-e2e'
const BASE_URL = process.env.STACKLAB_E2E_URL ?? 'http://127.0.0.1:18081'

export async function login(page: Page) {
  await page.goto('/login')
  await page.getByTestId('login-password').fill(PASSWORD)
  await page.getByTestId('login-submit').click()
  await page.waitForURL('**/stacks')
}

/**
 * Create a stack via API and return its ID.
 * Uses the REST API directly to avoid depending on UI for setup.
 */
export async function createStackViaApi(page: Page, stackId: string, composeYaml: string): Promise<void> {
  const cookies = await page.context().cookies()
  const sessionCookie = cookies.find((c) => c.name.startsWith('stacklab'))

  const res = await page.request.post(`${BASE_URL}/api/stacks`, {
    data: {
      stack_id: stackId,
      compose_yaml: composeYaml,
      env: '',
      create_config_dir: true,
      create_data_dir: true,
      deploy_after_create: false,
    },
    headers: sessionCookie ? { Cookie: `${sessionCookie.name}=${sessionCookie.value}` } : {},
  })

  expect(res.ok()).toBeTruthy()
}

/**
 * Delete a stack via API (runtime + definition).
 */
export async function deleteStackViaApi(page: Page, stackId: string): Promise<void> {
  const cookies = await page.context().cookies()
  const sessionCookie = cookies.find((c) => c.name.startsWith('stacklab'))

  const res = await page.request.delete(`${BASE_URL}/api/stacks/${stackId}`, {
    data: {
      remove_runtime: true,
      remove_definition: true,
      remove_config: true,
      remove_data: true,
    },
    headers: sessionCookie ? { Cookie: `${sessionCookie.name}=${sessionCookie.value}` } : {},
  })

  // Ignore 404 — stack may already be gone
  if (res.status() !== 404) {
    expect(res.ok()).toBeTruthy()
  }
}

/**
 * Wait for a specific audit action to appear for a stack by polling the API.
 * More reliable than waiting for UI refresh.
 */
export async function waitForAuditEntry(
  page: Page,
  stackId: string,
  action: string,
  timeoutMs = 10_000,
): Promise<void> {
  const cookies = await page.context().cookies()
  const sessionCookie = cookies.find((c) => c.name.startsWith('stacklab'))
  const headers = sessionCookie ? { Cookie: `${sessionCookie.name}=${sessionCookie.value}` } : {}

  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const res = await page.request.get(`${BASE_URL}/api/stacks/${stackId}/audit?limit=10`, { headers })
    if (res.ok()) {
      const body = await res.json()
      const found = body.items?.some((e: { action: string }) => e.action === action)
      if (found) return
    }
    await page.waitForTimeout(500)
  }
  throw new Error(`Audit entry "${action}" for stack "${stackId}" not found within ${timeoutMs}ms`)
}
