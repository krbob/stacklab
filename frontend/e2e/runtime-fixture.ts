import { type Page } from '@playwright/test'
import {
  createStackViaApi,
  deleteStackViaApi,
  invokeStackActionViaApi,
} from './helpers'

export const RUNTIME_LOG_MARKER = 'stacklab-e2e-runtime-ready'

const RUNTIME_COMPOSE = `services:
  probe:
    image: alpine:3.20
    command:
      - sh
      - -c
      - |
        trap 'exit 0' TERM INT
        echo ${RUNTIME_LOG_MARKER}
        while :; do sleep 1; done
    stop_grace_period: 2s
`

export async function startRuntimeStack(page: Page, stackId: string): Promise<void> {
  await deleteStackViaApi(page, stackId)
  await createStackViaApi(page, stackId, RUNTIME_COMPOSE)
  await invokeStackActionViaApi(page, stackId, 'up')
}

export async function stopRuntimeStack(page: Page, stackId: string): Promise<void> {
  await deleteStackViaApi(page, stackId)
}
