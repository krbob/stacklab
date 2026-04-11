import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from '@playwright/test'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const frontendDir = path.resolve(__dirname, '..')
const repoRoot = path.resolve(frontendDir, '..')

const BASE_URL = process.env.STACKLAB_URL ?? 'http://127.0.0.1:18080'
const PASSWORD = process.env.STACKLAB_PASSWORD ?? 'stacktesting'
const OUTPUT_DIR = process.env.STACKLAB_SCREENSHOT_DIR ?? path.join(repoRoot, 'docs', 'images', 'readme')
const DEMO_STACK_ID = process.env.STACKLAB_SCREENSHOT_STACK_ID ?? 'readme-stack'
const DEMO_CONFIG_DIR = process.env.STACKLAB_SCREENSHOT_CONFIG_DIR ?? 'readme-shared'
const VIEWPORT = { width: 1440, height: 960 }

const capturePlan = [
  { name: 'stacks-overview', path: '/stacks', waitFor: waitForStacksPage },
  { name: 'stack-editor', path: `/stacks/${DEMO_STACK_ID}/editor`, waitFor: waitForStackEditorPage },
  { name: 'host-overview', path: '/host', waitFor: waitForHostPage },
  { name: 'config-workspace', path: '/config', waitFor: waitForConfigPage, prepare: openConfigFile },
  { name: 'maintenance-update', path: '/maintenance', waitFor: waitForMaintenancePage },
  { name: 'docker-admin', path: '/docker', waitFor: waitForDockerPage },
]

const staticCaptureCss = `
  *, *::before, *::after {
    transition: none !important;
    animation-duration: 0s !important;
    animation-delay: 0s !important;
    caret-color: transparent !important;
  }
`

async function run() {
  await fs.mkdir(OUTPUT_DIR, { recursive: true })

  const browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({
    viewport: VIEWPORT,
    colorScheme: 'dark',
    reducedMotion: 'reduce',
  })
  const page = await context.newPage()

  try {
    await login(page)
    await seedDemoData(context)

    for (const capture of capturePlan) {
      await page.goto(`${BASE_URL}${capture.path}`)
      await page.addStyleTag({ content: staticCaptureCss })
      await capture.waitFor(page)
      if (capture.prepare) {
        await capture.prepare(page)
      }
      await page.waitForTimeout(700)
      const target = path.join(OUTPUT_DIR, `${capture.name}.png`)
      await page.screenshot({ path: target, fullPage: false })
      console.log(`captured ${path.relative(repoRoot, target)}`)
    }
  } finally {
    try {
      await cleanupDemoData(context)
    } catch (error) {
      console.warn(`cleanup failed: ${error instanceof Error ? error.message : String(error)}`)
    }
    await context.close()
    await browser.close()
  }
}

async function login(page) {
  await page.goto(`${BASE_URL}/login`)
  await page.getByTestId('login-password').fill(PASSWORD)
  await page.getByTestId('login-submit').click()
  await page.waitForURL('**/stacks')
}

async function openConfigFile(page) {
  const folderButton = page.getByRole('button', { name: new RegExp(`${DEMO_CONFIG_DIR}$`) })
  await folderButton.click()
  const fileButton = page.getByRole('button', { name: /app\.conf$/ })
  await fileButton.click()
  await page.getByText(`${DEMO_CONFIG_DIR}/app.conf`).waitFor({ state: 'visible', timeout: 10_000 })
}

async function waitForStacksPage(page) {
  await page.getByRole('heading', { name: 'Stacks' }).waitFor({ state: 'visible', timeout: 15_000 })
}

async function waitForStackEditorPage(page) {
  await page.getByText('Resolved config', { exact: true }).waitFor({ state: 'visible', timeout: 15_000 })
}

async function waitForHostPage(page) {
  await page.getByRole('heading', { name: 'Host' }).waitFor({ state: 'visible', timeout: 15_000 })
}

async function waitForConfigPage(page) {
  await page.getByText('Config workspace', { exact: true }).waitFor({ state: 'visible', timeout: 15_000 })
}

async function waitForMaintenancePage(page) {
  await page.getByRole('heading', { name: 'Maintenance' }).waitFor({ state: 'visible', timeout: 15_000 })
}

async function waitForDockerPage(page) {
  await page.getByRole('heading', { name: 'Docker' }).waitFor({ state: 'visible', timeout: 15_000 })
}

async function seedDemoData(context) {
  await deleteStackIfExists(context, DEMO_STACK_ID)

  await api(context, 'PUT', '/api/config/workspace/file', {
    path: `${DEMO_CONFIG_DIR}/app.conf`,
    content: [
      'server_name demo.stacklab.local;',
      'client_max_body_size 32m;',
      'add_header X-Stacklab "readme-demo";',
      '',
    ].join('\n'),
    create_parent_directories: true,
  })

  await api(context, 'PUT', '/api/config/workspace/file', {
    path: `${DEMO_CONFIG_DIR}/notes.txt`,
    content: 'README screenshot fixture\n',
    create_parent_directories: true,
  })

  const composeYaml = [
    'services:',
    '  web:',
    '    image: nginx:alpine',
    '    restart: unless-stopped',
    '    ports:',
    '      - "18090:80"',
    '    volumes:',
    `      - ../../config/${DEMO_CONFIG_DIR}/app.conf:/etc/readme-demo/app.conf:ro`,
    '  worker:',
    '    image: alpine:3.22',
    '    command: ["sh", "-c", "while true; do sleep 3600; done"]',
    '    restart: unless-stopped',
    '',
  ].join('\n')

  const createResponse = await api(context, 'POST', '/api/stacks', {
    stack_id: DEMO_STACK_ID,
    compose_yaml: composeYaml,
    env: 'APP_ENV=demo\n',
    create_config_dir: true,
    create_data_dir: true,
    deploy_after_create: true,
  })
  await waitForJob(context, createResponse.job.id)
}

async function cleanupDemoData(context) {
  await deleteStackIfExists(context, DEMO_STACK_ID)
}

async function deleteStackIfExists(context, stackId) {
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    const response = await context.request.fetch(`${BASE_URL}/api/stacks/${encodeURIComponent(stackId)}`, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      data: {
        remove_runtime: true,
        remove_definition: true,
        remove_config: true,
        remove_data: true,
      },
    })

    if (response.status() === 404) {
      return
    }
    if (response.status() === 409) {
      await wait(500)
      continue
    }
    if (!response.ok()) {
      throw new Error(`delete stack failed: ${response.status()} ${await response.text()}`)
    }

    const payload = await response.json()
    await waitForJob(context, payload.job.id)
    return
  }

  throw new Error(`delete stack ${stackId} remained locked for too long`)
}

async function waitForJob(context, jobId, timeoutMs = 60_000) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const response = await context.request.get(`${BASE_URL}/api/jobs/${jobId}`)
    if (!response.ok()) {
      throw new Error(`job ${jobId} status failed: ${response.status()} ${await response.text()}`)
    }
    const payload = await response.json()
    const state = payload.job?.state
    if (state === 'succeeded') {
      return
    }
    if (state === 'failed' || state === 'cancelled' || state === 'timed_out') {
      throw new Error(`job ${jobId} ended in unexpected state: ${state}`)
    }
    await wait(400)
  }
  throw new Error(`job ${jobId} did not complete within ${timeoutMs}ms`)
}

async function api(context, method, endpoint, data) {
  const response = await context.request.fetch(`${BASE_URL}${endpoint}`, {
    method,
    headers: { 'Content-Type': 'application/json' },
    data,
  })
  if (!response.ok()) {
    throw new Error(`${method} ${endpoint} failed: ${response.status()} ${await response.text()}`)
  }
  return response.json()
}

function wait(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

run().catch((error) => {
  console.error(error)
  process.exit(1)
})
