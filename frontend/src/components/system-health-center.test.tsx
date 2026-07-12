import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { SystemHealthCenter } from './system-health-center'
import { getDockerAdminOverview, getReadiness } from '@/lib/api-client'
import type { DockerAdminOverviewResponse } from '@/lib/api-types'

const mockReconnect = vi.fn()
const mockUseWs = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getReadiness: vi.fn(),
  getDockerAdminOverview: vi.fn(),
}))

vi.mock('@/hooks/use-ws', () => ({
  useWs: () => mockUseWs(),
}))

const healthyDocker: DockerAdminOverviewResponse = {
  service: {
    manager: 'systemd',
    supported: true,
    unit_name: 'docker.service',
    load_state: 'loaded',
    active_state: 'active',
    sub_state: 'running',
    unit_file_state: 'enabled',
    fragment_path: '/lib/systemd/system/docker.service',
  },
  engine: {
    available: true,
    version: '29.3.1',
    api_version: '1.54',
    compose_version: '5.1.1',
    root_dir: '/var/lib/docker',
    driver: 'overlay2',
    logging_driver: 'json-file',
    cgroup_driver: 'systemd',
  },
  daemon_config: {
    path: '/etc/docker/daemon.json',
    exists: true,
    valid_json: true,
    configured_keys: [],
    summary: {
      dns: [],
      registry_mirrors: [],
      insecure_registries: [],
      log_driver: 'json-file',
      data_root: '/var/lib/docker',
    },
    write_capability: { supported: false, managed_keys: [] },
  },
  write_capability: { supported: false, managed_keys: [] },
}

describe('SystemHealthCenter', () => {
  beforeEach(() => {
    vi.spyOn(Date, 'now').mockReturnValue(Date.parse('2026-07-12T08:30:00Z'))
    vi.mocked(getReadiness).mockReset()
    vi.mocked(getDockerAdminOverview).mockReset()
    mockReconnect.mockReset()
    mockUseWs.mockReset()
    vi.mocked(getReadiness).mockResolvedValue({
      status: 'ok',
      version: '2026.07.0',
      checks: {
        database: { status: 'ok' },
        frontend: { status: 'ok' },
        runtime: { status: 'ok' },
      },
    })
    vi.mocked(getDockerAdminOverview).mockResolvedValue(healthyDocker)
    mockUseWs.mockReturnValue({
      connected: true,
      lastConnectedAt: Date.parse('2026-07-12T08:29:00Z'),
      reconnect: mockReconnect,
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('shows healthy Backend, Docker, and WebSocket states with timestamps and diagnostics', async () => {
    renderCenter()

    expect(await screen.findByText('All core connections are healthy.')).toBeInTheDocument()
    const backend = screen.getByRole('article', { name: 'Backend health' })
    const docker = screen.getByRole('article', { name: 'Docker health' })
    const websocket = screen.getByRole('article', { name: 'WebSocket health' })
    expect(within(backend).getByText('Healthy')).toBeInTheDocument()
    expect(within(backend).getByText('2026.07.0')).toBeInTheDocument()
    expect(within(docker).getByText('Healthy')).toBeInTheDocument()
    expect(within(docker).getByText('29.3.1')).toBeInTheDocument()
    expect(within(websocket).getByText('Connected')).toBeInTheDocument()
    expect(within(websocket).queryByRole('button', { name: 'Reconnect WebSocket' })).not.toBeInTheDocument()

    expect(within(backend).getByRole('time')).toHaveAttribute('datetime', '2026-07-12T08:30:00.000Z')
    expect(within(docker).getByRole('time')).toHaveAttribute('datetime', '2026-07-12T08:30:00.000Z')
    expect(within(websocket).getByRole('time')).toHaveAttribute('datetime', '2026-07-12T08:29:00.000Z')
    expect(within(backend).getByRole('link', { name: 'View Stacklab logs' })).toHaveAttribute('href', '/host#stacklab-logs')
    expect(within(docker).getByRole('link', { name: 'Open Docker diagnostics' })).toHaveAttribute('href', '/docker')
    expect(within(websocket).getByRole('link', { name: 'Open audit log' })).toHaveAttribute('href', '/audit')
  })

  it('shows degraded component details without claiming a successful check', async () => {
    vi.mocked(getReadiness).mockResolvedValue({
      status: 'unavailable',
      version: '2026.07.0',
      checks: {
        database: { status: 'error', message: 'unavailable' },
        frontend: { status: 'ok' },
        runtime: { status: 'ok' },
      },
    })
    vi.mocked(getDockerAdminOverview).mockResolvedValue({
      ...healthyDocker,
      engine: { ...healthyDocker.engine, available: false, message: 'Cannot connect to Docker daemon' },
    })
    mockUseWs.mockReturnValue({ connected: false, lastConnectedAt: null, reconnect: mockReconnect })

    renderCenter()

    expect(await screen.findByText('Some core connections need attention.')).toBeInTheDocument()
    const backend = screen.getByRole('article', { name: 'Backend health' })
    const docker = screen.getByRole('article', { name: 'Docker health' })
    const websocket = screen.getByRole('article', { name: 'WebSocket health' })
    expect(within(backend).getByText('Degraded')).toBeInTheDocument()
    expect(within(backend).getByText('Database')).toBeInTheDocument()
    expect(within(backend).getByText('unavailable')).toBeInTheDocument()
    expect(within(backend).getByText('No successful check in this session')).toBeInTheDocument()
    expect(within(docker).getByText('Unavailable')).toBeInTheDocument()
    expect(within(docker).getByText('Cannot connect to Docker daemon')).toBeInTheDocument()
    expect(within(websocket).getByText('Connecting')).toBeInTheDocument()
    expect(within(websocket).getByText('No successful check in this session')).toBeInTheDocument()

    fireEvent.click(within(websocket).getByRole('button', { name: 'Reconnect WebSocket' }))
    expect(mockReconnect).toHaveBeenCalledTimes(1)
  })

  it('treats an unsupported service manager as diagnostic limits and keeps WebSocket recovery explicit', async () => {
    vi.mocked(getDockerAdminOverview).mockResolvedValue({
      ...healthyDocker,
      service: {
        ...healthyDocker.service,
        supported: false,
        active_state: '',
        sub_state: '',
        message: 'systemctl not found',
      },
    })
    mockUseWs.mockReturnValue({
      connected: false,
      lastConnectedAt: Date.parse('2026-07-12T08:20:00Z'),
      reconnect: mockReconnect,
    })

    renderCenter()

    const docker = screen.getByRole('article', { name: 'Docker health' })
    const websocket = screen.getByRole('article', { name: 'WebSocket health' })
    expect(await within(docker).findByText('Healthy')).toBeInTheDocument()
    expect(within(docker).getByText('Manager status not available')).toBeInTheDocument()
    expect(within(websocket).getByText('Reconnecting')).toBeInTheDocument()
    expect(within(websocket).getByRole('time')).toHaveAttribute('datetime', '2026-07-12T08:20:00.000Z')
    expect(within(websocket).getByRole('button', { name: 'Reconnect WebSocket' })).toBeEnabled()
  })

  it('retries an initial backend failure without refetching Docker', async () => {
    vi.mocked(getReadiness)
      .mockRejectedValueOnce(new Error('Readiness request failed'))
      .mockResolvedValueOnce({
        status: 'ok',
        version: '2026.07.0',
        checks: { database: { status: 'ok' }, frontend: { status: 'ok' }, runtime: { status: 'ok' } },
      })

    renderCenter()

    const backend = screen.getByRole('article', { name: 'Backend health' })
    expect(await within(backend).findByRole('alert')).toHaveTextContent('Readiness request failed')
    expect(within(backend).getByText('Unavailable')).toBeInTheDocument()
    expect(getDockerAdminOverview).toHaveBeenCalledTimes(1)

    fireEvent.click(within(backend).getByRole('button', { name: 'Retry backend readiness' }))

    await waitFor(() => expect(within(backend).getByText('Healthy')).toBeInTheDocument())
    expect(getReadiness).toHaveBeenCalledTimes(2)
    expect(getDockerAdminOverview).toHaveBeenCalledTimes(1)
  })

  it('preserves the last ready state and timestamp when a refresh fails', async () => {
    vi.mocked(getReadiness)
      .mockResolvedValueOnce({
        status: 'ok',
        version: '2026.07.0',
        checks: { database: { status: 'ok' }, frontend: { status: 'ok' }, runtime: { status: 'ok' } },
      })
      .mockRejectedValueOnce(new Error('Refresh failed'))

    renderCenter()

    const backend = screen.getByRole('article', { name: 'Backend health' })
    expect(await within(backend).findByText('Healthy')).toBeInTheDocument()
    const timestamp = within(backend).getByRole('time').getAttribute('datetime')
    fireEvent.click(within(backend).getByRole('button', { name: 'Retry backend readiness' }))

    expect(await within(backend).findByRole('alert')).toHaveTextContent('Showing the last successfully loaded state.')
    expect(within(backend).getByText('Healthy')).toBeInTheDocument()
    expect(within(backend).getByRole('time')).toHaveAttribute('datetime', timestamp)
  })
})

function renderCenter() {
  return render(
    <MemoryRouter>
      <SystemHealthCenter />
    </MemoryRouter>,
  )
}
