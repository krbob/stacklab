import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DockerAdminPage } from './docker-admin-page'
import type { DockerAdminOverviewResponse, DockerDaemonConfigResponse } from '@/lib/api-types'

const mockUseApi = vi.fn()

vi.mock('@/hooks/use-api', () => ({
  useApi: (...args: unknown[]) => mockUseApi(...args),
}))

vi.mock('@/lib/api-client', () => ({
  getDockerAdminOverview: vi.fn(),
  getDockerDaemonConfig: vi.fn(),
}))

const overview: DockerAdminOverviewResponse = {
  service: {
    manager: 'systemd',
    supported: true,
    unit_name: 'docker.service',
    load_state: 'loaded',
    active_state: 'active',
    sub_state: 'running',
    unit_file_state: 'enabled',
    fragment_path: '/lib/systemd/system/docker.service',
    started_at: '2026-04-01T10:00:00Z',
  },
  engine: {
    available: true,
    version: '29.3.1',
    api_version: '1.54',
    compose_version: '5.1.1',
    root_dir: '/var/lib/docker',
    driver: 'overlay2',
    logging_driver: 'json-file',
    cgroup_driver: 'cgroupfs',
  },
  daemon_config: {
    path: '/etc/docker/daemon.json',
    exists: true,
    permissions: {
      owner_uid: 0,
      owner_name: 'root',
      group_gid: 0,
      group_name: 'root',
      mode: '0644',
      readable: true,
      writable: false,
    },
    size_bytes: 130,
    modified_at: '2026-04-01T08:00:00Z',
    valid_json: true,
    configured_keys: ['dns', 'log-driver', 'live-restore'],
    summary: {
      dns: ['192.168.1.2'],
      registry_mirrors: ['https://mirror.local'],
      insecure_registries: [],
      log_driver: 'json-file',
      data_root: '',
      live_restore: true,
    },
  },
}

const daemonConfig: DockerDaemonConfigResponse = {
  ...overview.daemon_config,
  content: '{\n  "dns": ["192.168.1.2"],\n  "log-driver": "json-file",\n  "live-restore": true\n}\n',
}

const unsupportedServiceOverview: DockerAdminOverviewResponse = {
  ...overview,
  service: {
    manager: 'systemd',
    supported: false,
    unit_name: 'docker.service',
    load_state: '',
    active_state: '',
    sub_state: '',
    unit_file_state: '',
    fragment_path: '',
    message: 'systemctl not found',
  },
}

const unavailableEngineOverview: DockerAdminOverviewResponse = {
  ...overview,
  engine: {
    available: false,
    version: '',
    api_version: '',
    compose_version: '',
    root_dir: '',
    driver: '',
    logging_driver: '',
    cgroup_driver: '',
    message: 'Cannot connect to Docker daemon',
  },
}

const noDaemonOverview: DockerAdminOverviewResponse = {
  ...overview,
  daemon_config: {
    path: '/etc/docker/daemon.json',
    exists: false,
    valid_json: false,
    configured_keys: [],
    summary: {
      dns: [],
      registry_mirrors: [],
      insecure_registries: [],
      log_driver: '',
      data_root: '',
    },
  },
}

describe('DockerAdminPage', () => {
  beforeEach(() => {
    mockUseApi.mockReset()
  })

  function mockOverviewAndConfig(ov: DockerAdminOverviewResponse, cfg: DockerDaemonConfigResponse) {
    let callIndex = 0
    mockUseApi.mockImplementation(() => {
      const idx = callIndex++
      if (idx === 0) return { data: ov, error: null, loading: false, refetch: vi.fn() }
      return { data: cfg, error: null, loading: false, refetch: vi.fn() }
    })
  }

  it('renders service, engine, and daemon config cards', () => {
    mockOverviewAndConfig(overview, daemonConfig)
    render(<DockerAdminPage />)

    expect(screen.getByText('active')).toBeInTheDocument()
    expect(screen.getByText('29.3.1')).toBeInTheDocument()
    expect(screen.getByText('Valid JSON')).toBeInTheDocument()
    expect(screen.getByText(/DNS: 192\.168\.1\.2/)).toBeInTheDocument()
  })

  it('shows engine metadata in mono', () => {
    mockOverviewAndConfig(overview, daemonConfig)
    render(<DockerAdminPage />)

    expect(screen.getByText('API: 1.54')).toBeInTheDocument()
    expect(screen.getByText('Compose: 5.1.1')).toBeInTheDocument()
    expect(screen.getByText('Driver: overlay2')).toBeInTheDocument()
  })

  it('shows raw daemon.json content', () => {
    mockOverviewAndConfig(overview, daemonConfig)
    render(<DockerAdminPage />)

    expect(screen.getByText(/"dns"/)).toBeInTheDocument()
  })

  it('shows degraded service state on unsupported host', () => {
    mockOverviewAndConfig(unsupportedServiceOverview, daemonConfig)
    render(<DockerAdminPage />)

    expect(screen.getByText('Not available')).toBeInTheDocument()
    expect(screen.getByText('systemctl not found')).toBeInTheDocument()
  })

  it('shows unavailable engine state', () => {
    mockOverviewAndConfig(unavailableEngineOverview, daemonConfig)
    render(<DockerAdminPage />)

    expect(screen.getByText('Unavailable')).toBeInTheDocument()
    expect(screen.getByText('Cannot connect to Docker daemon')).toBeInTheDocument()
  })

  it('shows no daemon.json state', () => {
    const noDaemonConfig: DockerDaemonConfigResponse = {
      ...noDaemonOverview.daemon_config,
      content: null,
    }
    mockOverviewAndConfig(noDaemonOverview, noDaemonConfig)
    render(<DockerAdminPage />)

    expect(screen.getAllByText(/No daemon\.json found/).length).toBeGreaterThanOrEqual(1)
  })

  it('shows overview error', () => {
    let callIndex = 0
    mockUseApi.mockImplementation(() => {
      const idx = callIndex++
      if (idx === 0) return { data: null, error: new Error('Connection refused'), loading: false, refetch: vi.fn() }
      return { data: null, error: null, loading: false, refetch: vi.fn() }
    })
    render(<DockerAdminPage />)

    expect(screen.getByText(/Connection refused/)).toBeInTheDocument()
  })
})
