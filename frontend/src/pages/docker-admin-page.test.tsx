import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DockerAdminPage } from './docker-admin-page'
import type { DockerAdminOverviewResponse, DockerDaemonConfigResponse, DockerRegistryStatusResponse } from '@/lib/api-types'

const mockUseApi = vi.fn()
const mockValidateDockerDaemonConfig = vi.fn()
const mockApplyDockerDaemonConfig = vi.fn()
const mockLoginDockerRegistry = vi.fn()
const mockLogoutDockerRegistry = vi.fn()
const mockUseJobStream = vi.fn()

vi.mock('@/hooks/use-api', () => ({
  useApi: (...args: unknown[]) => mockUseApi(...args),
}))

vi.mock('@/lib/api-client', () => ({
  getDockerAdminOverview: vi.fn(),
  getDockerDaemonConfig: vi.fn(),
  getDockerRegistryStatus: vi.fn(),
  validateDockerDaemonConfig: (...args: unknown[]) => mockValidateDockerDaemonConfig(...args),
  applyDockerDaemonConfig: (...args: unknown[]) => mockApplyDockerDaemonConfig(...args),
  loginDockerRegistry: (...args: unknown[]) => mockLoginDockerRegistry(...args),
  logoutDockerRegistry: (...args: unknown[]) => mockLogoutDockerRegistry(...args),
}))

vi.mock('@/hooks/use-job-stream', () => ({
  useJobStream: (...args: unknown[]) => mockUseJobStream(...args),
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
    write_capability: {
      supported: false,
      reason: 'Managed Docker daemon apply is not configured yet.',
      managed_keys: ['dns', 'registry_mirrors', 'insecure_registries', 'live_restore'],
    },
  },
  write_capability: {
    supported: false,
    reason: 'Managed Docker daemon apply is not configured yet.',
    managed_keys: ['dns', 'registry_mirrors', 'insecure_registries', 'live_restore'],
  },
}

const daemonConfig: DockerDaemonConfigResponse = {
  ...overview.daemon_config,
  content: '{\n  "dns": ["192.168.1.2"],\n  "log-driver": "json-file",\n  "live-restore": true\n}\n',
}

const writeCapableOverview: DockerAdminOverviewResponse = {
  ...overview,
  daemon_config: {
    ...overview.daemon_config,
    write_capability: {
      supported: true,
      managed_keys: ['dns', 'registry_mirrors', 'insecure_registries', 'live_restore'],
    },
  },
  write_capability: {
    supported: true,
    managed_keys: ['dns', 'registry_mirrors', 'insecure_registries', 'live_restore'],
  },
}

const writeCapableDaemonConfig: DockerDaemonConfigResponse = {
  ...daemonConfig,
  write_capability: {
    supported: true,
    managed_keys: ['dns', 'registry_mirrors', 'insecure_registries', 'live_restore'],
  },
}

const registryStatus: DockerRegistryStatusResponse = {
  docker_config_path: '/var/lib/stacklab/docker/config.json',
  exists: true,
  valid_json: true,
  items: [
    {
      registry: 'ghcr.io',
      configured: true,
      username: 'bob',
      source: 'docker_config',
      last_error: '',
    },
  ],
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
    write_capability: {
      supported: false,
      reason: 'Managed Docker daemon apply is not configured yet.',
      managed_keys: ['dns', 'registry_mirrors', 'insecure_registries', 'live_restore'],
    },
  },
}

describe('DockerAdminPage', () => {
  beforeEach(() => {
    mockUseApi.mockReset()
    mockValidateDockerDaemonConfig.mockReset()
    mockApplyDockerDaemonConfig.mockReset()
    mockLoginDockerRegistry.mockReset()
    mockLogoutDockerRegistry.mockReset()
    mockUseJobStream.mockReset()
    mockUseJobStream.mockReturnValue({ events: [], state: null, clear: vi.fn() })
  })

  function mockDockerPage(
    ov: DockerAdminOverviewResponse,
    cfg: DockerDaemonConfigResponse,
    registry: DockerRegistryStatusResponse = registryStatus,
    refetch?: {
      overview: ReturnType<typeof vi.fn>
      config: ReturnType<typeof vi.fn>
      registry: ReturnType<typeof vi.fn>
    },
  ) {
    let callIndex = 0
    mockUseApi.mockImplementation(() => {
      const idx = callIndex++
      if (idx === 0) return { data: ov, error: null, loading: false, refetch: refetch?.overview ?? vi.fn() }
      if (idx === 1) return { data: cfg, error: null, loading: false, refetch: refetch?.config ?? vi.fn() }
      return { data: registry, error: null, loading: false, refetch: refetch?.registry ?? vi.fn() }
    })
  }

  it('renders service, engine, and daemon config cards', () => {
    mockDockerPage(overview, daemonConfig)
    render(<DockerAdminPage />)

    expect(screen.getByText('active')).toBeInTheDocument()
    expect(screen.getByText('29.3.1')).toBeInTheDocument()
    expect(screen.getByText('Valid JSON')).toBeInTheDocument()
    expect(screen.getByText(/DNS: 192\.168\.1\.2/)).toBeInTheDocument()
  })

  it('shows engine metadata in mono', () => {
    mockDockerPage(overview, daemonConfig)
    render(<DockerAdminPage />)

    expect(screen.getByText('API: 1.54')).toBeInTheDocument()
    expect(screen.getByText('Compose: 5.1.1')).toBeInTheDocument()
    expect(screen.getByText('Driver: overlay2')).toBeInTheDocument()
  })

  it('shows raw daemon.json content', () => {
    mockDockerPage(overview, daemonConfig)
    render(<DockerAdminPage />)

    expect(screen.getByText(/"dns"/)).toBeInTheDocument()
  })

  it('shows degraded service state on unsupported host', () => {
    mockDockerPage(unsupportedServiceOverview, daemonConfig)
    render(<DockerAdminPage />)

    expect(screen.getByText('Not available')).toBeInTheDocument()
    expect(screen.getByText('systemctl not found')).toBeInTheDocument()
  })

  it('shows unavailable engine state', () => {
    mockDockerPage(unavailableEngineOverview, daemonConfig)
    render(<DockerAdminPage />)

    expect(screen.getByText('Unavailable')).toBeInTheDocument()
    expect(screen.getByText('Cannot connect to Docker daemon')).toBeInTheDocument()
  })

  it('shows no daemon.json state', () => {
    const noDaemonConfig: DockerDaemonConfigResponse = {
      ...noDaemonOverview.daemon_config,
      content: null,
    }
    mockDockerPage(noDaemonOverview, noDaemonConfig)
    render(<DockerAdminPage />)

    expect(screen.getAllByText(/Docker is using defaults|Docker is currently using built-in defaults\./).length).toBeGreaterThanOrEqual(1)
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

  it('validates managed settings and shows preview', async () => {
    mockDockerPage(overview, daemonConfig)
    mockValidateDockerDaemonConfig.mockResolvedValue({
      write_capability: overview.write_capability,
      changed_keys: ['dns'],
      requires_restart: true,
      warnings: ['Applying Docker daemon settings requires a Docker restart.'],
      preview: {
        path: '/etc/docker/daemon.json',
        content: '{\n  "dns": ["1.1.1.1"]\n}\n',
        configured_keys: ['dns'],
        summary: {
          dns: ['1.1.1.1'],
          registry_mirrors: [],
          insecure_registries: [],
          log_driver: 'json-file',
          data_root: '',
          live_restore: true,
        },
      },
    })

    render(<DockerAdminPage />)

    fireEvent.change(screen.getByLabelText(/DNS servers/i), { target: { value: '1.1.1.1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Validate' }))

    await waitFor(() => {
      expect(mockValidateDockerDaemonConfig).toHaveBeenCalledWith({
        settings: {
          dns: ['1.1.1.1'],
          registry_mirrors: ['https://mirror.local'],
          insecure_registries: [],
          live_restore: true,
        },
        remove_keys: ['insecure_registries'],
      })
    })

    expect(screen.getByText(/Validation passed with warnings/)).toBeInTheDocument()
    expect(screen.getByText(/Requires Docker restart/)).toBeInTheDocument()
    expect(screen.getByText(/"dns": \["1.1.1.1"\]/)).toBeInTheDocument()
  })

  it('sends remove_keys when list fields are cleared', async () => {
    mockDockerPage(overview, daemonConfig)
    mockValidateDockerDaemonConfig.mockResolvedValue({
      write_capability: overview.write_capability,
      changed_keys: ['dns', 'registry_mirrors', 'insecure_registries'],
      requires_restart: true,
      warnings: [],
      preview: {
        path: '/etc/docker/daemon.json',
        content: '{}\n',
        configured_keys: [],
        summary: {
          dns: [],
          registry_mirrors: [],
          insecure_registries: [],
          log_driver: '',
          data_root: '',
          live_restore: false,
        },
      },
    })

    render(<DockerAdminPage />)

    fireEvent.change(screen.getByLabelText(/DNS servers/i), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText(/Registry mirrors/i), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText(/Insecure registries/i), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Validate' }))

    await waitFor(() => {
      expect(mockValidateDockerDaemonConfig).toHaveBeenCalledWith({
        settings: {
          dns: [],
          registry_mirrors: [],
          insecure_registries: [],
          live_restore: true,
        },
        remove_keys: ['dns', 'registry_mirrors', 'insecure_registries'],
      })
    })
  })

  it('shows inline validation error', async () => {
    mockDockerPage(overview, daemonConfig)
    mockValidateDockerDaemonConfig.mockRejectedValue(new Error('Docker daemon config contains invalid JSON and cannot be managed safely.'))

    render(<DockerAdminPage />)
    fireEvent.click(screen.getByRole('button', { name: 'Validate' }))

    expect(await screen.findByText(/cannot be managed safely/i)).toBeInTheDocument()
  })

  it('enables apply after successful validate even with warnings', async () => {
    mockDockerPage(writeCapableOverview, writeCapableDaemonConfig)
    mockValidateDockerDaemonConfig.mockResolvedValue({
      write_capability: writeCapableOverview.write_capability,
      changed_keys: ['dns'],
      requires_restart: true,
      warnings: ['Applying Docker daemon settings requires a Docker restart.'],
      preview: {
        path: '/etc/docker/daemon.json',
        content: '{\n  "dns": ["1.1.1.1"]\n}\n',
        configured_keys: ['dns'],
        summary: {
          dns: ['1.1.1.1'],
          registry_mirrors: [],
          insecure_registries: [],
          log_driver: 'json-file',
          data_root: '',
          live_restore: true,
        },
      },
    })

    render(<DockerAdminPage />)

    fireEvent.change(screen.getByLabelText(/DNS servers/i), { target: { value: '1.1.1.1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Validate' }))

    await screen.findByText(/Validation passed with warnings/i)
    expect(screen.getByRole('button', { name: 'Apply & Restart' })).toBeEnabled()
  })

  it('invalidates preview when managed settings change after validate', async () => {
    mockDockerPage(writeCapableOverview, writeCapableDaemonConfig)
    mockValidateDockerDaemonConfig.mockResolvedValue({
      write_capability: writeCapableOverview.write_capability,
      changed_keys: ['dns'],
      requires_restart: true,
      warnings: [],
      preview: {
        path: '/etc/docker/daemon.json',
        content: '{\n  "dns": ["1.1.1.1"]\n}\n',
        configured_keys: ['dns'],
        summary: {
          dns: ['1.1.1.1'],
          registry_mirrors: [],
          insecure_registries: [],
          log_driver: 'json-file',
          data_root: '',
          live_restore: true,
        },
      },
    })

    render(<DockerAdminPage />)

    fireEvent.change(screen.getByLabelText(/DNS servers/i), { target: { value: '1.1.1.1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Validate' }))

    await screen.findByText(/Validation passed/i)
    expect(screen.getByRole('button', { name: 'Apply & Restart' })).toBeEnabled()

    fireEvent.change(screen.getByLabelText(/DNS servers/i), { target: { value: '9.9.9.9' } })

    await waitFor(() => {
      expect(screen.queryByText(/Validation passed/i)).not.toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: 'Apply & Restart' })).toBeDisabled()
  })

  it('shows apply failure rollback details and refetches after terminal state', async () => {
    const overviewRefetch = vi.fn()
    const configRefetch = vi.fn()
    mockDockerPage(writeCapableOverview, writeCapableDaemonConfig, registryStatus, {
      overview: overviewRefetch,
      config: configRefetch,
      registry: vi.fn(),
    })
    mockValidateDockerDaemonConfig.mockResolvedValue({
      write_capability: writeCapableOverview.write_capability,
      changed_keys: ['dns'],
      requires_restart: true,
      warnings: [],
      preview: {
        path: '/etc/docker/daemon.json',
        content: '{\n  "dns": ["1.1.1.1"]\n}\n',
        configured_keys: ['dns'],
        summary: {
          dns: ['1.1.1.1'],
          registry_mirrors: [],
          insecure_registries: [],
          log_driver: 'json-file',
          data_root: '',
          live_restore: true,
        },
      },
    })
    mockApplyDockerDaemonConfig.mockResolvedValue({
      job: {
        id: 'job-1',
        stack_id: null,
        action: 'apply_docker_daemon_config',
        state: 'running',
      },
    })
    mockUseJobStream.mockImplementation(({ jobId }: { jobId: string | null }) => {
      if (jobId !== 'job-1') {
        return { events: [], state: null, clear: vi.fn() }
      }
      return {
        state: 'failed',
        clear: vi.fn(),
        events: [
          {
            event: 'job_step_started',
            timestamp: '2026-04-09T10:00:00Z',
            message: 'Applying Docker daemon config and restarting Docker.',
            data: '',
            step: { index: 2, total: 3, action: 'apply_and_restart' },
          },
          {
            event: 'job_log',
            timestamp: '2026-04-09T10:00:01Z',
            message: 'Created Docker daemon config backup.',
            data: '/var/lib/stacklab/docker-admin-backups/daemon-20260409T100001Z.json',
            step: { index: 2, total: 3, action: 'apply_and_restart' },
          },
          {
            event: 'job_warning',
            timestamp: '2026-04-09T10:00:02Z',
            message: 'Docker restart failed; attempting rollback.',
            data: '',
            step: { index: 2, total: 3, action: 'apply_and_restart' },
          },
          {
            event: 'job_step_finished',
            timestamp: '2026-04-09T10:00:03Z',
            message: 'Apply failed.',
            data: '',
            state: 'failed',
            step: { index: 2, total: 3, action: 'apply_and_restart' },
          },
        ],
      }
    })

    render(<DockerAdminPage />)

    fireEvent.change(screen.getByLabelText(/DNS servers/i), { target: { value: '1.1.1.1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Validate' }))
    await screen.findByText(/Validation passed/i)

    fireEvent.click(screen.getByRole('button', { name: 'Apply & Restart' }))

    expect(await screen.findByText(/A rollback was attempted/i)).toBeInTheDocument()
    expect(screen.getAllByText(/daemon-20260409T100001Z\.json/).length).toBeGreaterThanOrEqual(1)
    await waitFor(() => {
      expect(overviewRefetch).toHaveBeenCalled()
      expect(configRefetch).toHaveBeenCalled()
    })
  })

  it('renders configured registries', () => {
    mockDockerPage(overview, daemonConfig)
    render(<DockerAdminPage />)

    expect(screen.getByText('Registry auth')).toBeInTheDocument()
    expect(screen.getByText('ghcr.io')).toBeInTheDocument()
    expect(screen.getByText(/Username: bob/)).toBeInTheDocument()
  })

  it('starts docker registry login and refetches status on success', async () => {
    const registryRefetch = vi.fn()
    mockDockerPage(overview, daemonConfig, registryStatus, {
      overview: vi.fn(),
      config: vi.fn(),
      registry: registryRefetch,
    })
    mockLoginDockerRegistry.mockResolvedValue({
      job: {
        id: 'job-login-1',
        stack_id: null,
        action: 'docker_registry_login',
        state: 'running',
      },
    })
    mockUseJobStream.mockImplementation(({ jobId }: { jobId: string | null }) => {
      if (jobId !== 'job-login-1') {
        return { events: [], state: null, clear: vi.fn() }
      }
      return {
        state: 'succeeded',
        clear: vi.fn(),
        events: [
          {
            event: 'job_step_started',
            timestamp: '2026-04-14T08:32:00Z',
            message: 'Starting Docker registry login.',
            data: '',
            step: { index: 1, total: 1, action: 'docker_login' },
          },
          {
            event: 'job_log',
            timestamp: '2026-04-14T08:32:01Z',
            message: 'Login Succeeded',
            data: '',
            step: { index: 1, total: 1, action: 'docker_login' },
          },
          {
            event: 'job_step_finished',
            timestamp: '2026-04-14T08:32:02Z',
            message: 'Finished Docker registry auth step.',
            data: '',
            state: 'succeeded',
            step: { index: 1, total: 1, action: 'docker_login' },
          },
        ],
      }
    })

    render(<DockerAdminPage />)

    fireEvent.change(screen.getByLabelText('Registry'), { target: { value: 'ghcr.io' } })
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'bob' } })
    fireEvent.change(screen.getByLabelText('Password or token'), { target: { value: 'secret-token' } })
    fireEvent.click(screen.getByRole('button', { name: 'Login' }))

    await waitFor(() => {
      expect(mockLoginDockerRegistry).toHaveBeenCalledWith({
        registry: 'ghcr.io',
        username: 'bob',
        password: 'secret-token',
      })
    })
    expect(await screen.findByText('Login succeeded')).toBeInTheDocument()
    await waitFor(() => {
      expect(registryRefetch).toHaveBeenCalled()
    })
  })

  it('starts docker registry logout from configured row', async () => {
    mockDockerPage(overview, daemonConfig)
    mockLogoutDockerRegistry.mockResolvedValue({
      job: {
        id: 'job-logout-1',
        stack_id: null,
        action: 'docker_registry_logout',
        state: 'running',
      },
    })
    mockUseJobStream.mockImplementation(({ jobId }: { jobId: string | null }) => {
      if (jobId !== 'job-logout-1') {
        return { events: [], state: null, clear: vi.fn() }
      }
      return {
        state: 'running',
        clear: vi.fn(),
        events: [
          {
            event: 'job_step_started',
            timestamp: '2026-04-14T08:35:00Z',
            message: 'Starting Docker registry logout.',
            data: '',
            step: { index: 1, total: 1, action: 'docker_logout' },
          },
        ],
      }
    })

    render(<DockerAdminPage />)
    fireEvent.click(screen.getByRole('button', { name: 'Logout' }))

    await waitFor(() => {
      expect(mockLogoutDockerRegistry).toHaveBeenCalledWith({ registry: 'ghcr.io' })
    })
    expect(screen.getByText('Logging out...')).toBeInTheDocument()
  })
})
