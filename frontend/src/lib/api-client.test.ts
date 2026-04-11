import { describe, expect, it, vi, beforeEach } from 'vitest'
import { getStacks, getStack, login, updateStacksMaintenance, commitGitWorkspace, pushGitWorkspace, getMaintenanceSchedules, updateMaintenanceSchedules, ApiClientError } from './api-client'

const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

beforeEach(() => {
  mockFetch.mockReset()
})

function jsonResponse(data: unknown, status = 200) {
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    statusText: 'OK',
    json: () => Promise.resolve(data),
  })
}

function errorResponse(status: number, error: { code: string; message: string }) {
  return Promise.resolve({
    ok: false,
    status,
    statusText: 'Error',
    json: () => Promise.resolve({ error }),
  })
}

describe('api-client', () => {
  describe('getStacks', () => {
    it('fetches stacks list', async () => {
      const data = { items: [], summary: { stack_count: 0 } }
      mockFetch.mockReturnValueOnce(jsonResponse(data))

      const result = await getStacks()
      expect(result).toEqual(data)
      expect(mockFetch).toHaveBeenCalledWith('/api/stacks', expect.objectContaining({
        credentials: 'same-origin',
      }))
    })

    it('passes query params', async () => {
      mockFetch.mockReturnValueOnce(jsonResponse({ items: [], summary: {} }))

      await getStacks({ q: 'traefik', sort: 'name' })
      const url = mockFetch.mock.calls[0][0] as string
      expect(url).toContain('q=traefik')
      expect(url).toContain('sort=name')
    })
  })

  describe('getStack', () => {
    it('encodes stack ID in URL', async () => {
      mockFetch.mockReturnValueOnce(jsonResponse({ stack: {} }))

      await getStack('my-stack')
      expect(mockFetch.mock.calls[0][0]).toBe('/api/stacks/my-stack')
    })
  })

  describe('login', () => {
    it('sends password in body', async () => {
      mockFetch.mockReturnValueOnce(jsonResponse({ authenticated: true }))

      await login('secret')
      const call = mockFetch.mock.calls[0]
      expect(call[1].method).toBe('POST')
      expect(JSON.parse(call[1].body)).toEqual({ password: 'secret' })
    })
  })

  describe('updateStacksMaintenance', () => {
    it('posts maintenance workflow payload', async () => {
      mockFetch.mockReturnValueOnce(jsonResponse({
        job: { id: 'job_123', stack_id: null, action: 'update_stacks', state: 'running' },
      }))

      await updateStacksMaintenance({
        target: { mode: 'selected', stack_ids: ['demo'] },
        options: {
          pull_images: true,
          build_images: true,
          remove_orphans: true,
          prune_after: { enabled: false, include_volumes: false },
        },
      })

      const call = mockFetch.mock.calls[0]
      expect(call[0]).toBe('/api/maintenance/update-stacks')
      expect(call[1].method).toBe('POST')
      expect(JSON.parse(call[1].body)).toEqual({
        target: { mode: 'selected', stack_ids: ['demo'] },
        options: {
          pull_images: true,
          build_images: true,
          remove_orphans: true,
          prune_after: { enabled: false, include_volumes: false },
        },
      })
    })
  })

  describe('git workspace write flow', () => {
    it('posts commit payload for selected paths', async () => {
      mockFetch.mockReturnValueOnce(jsonResponse({
        committed: true,
        commit: 'abc1234',
        summary: 'Update demo stack',
        paths: ['config/demo/app.conf'],
        remaining_changes: 1,
      }))

      await commitGitWorkspace({
        message: 'Update demo stack',
        paths: ['config/demo/app.conf'],
      })

      const call = mockFetch.mock.calls[0]
      expect(call[0]).toBe('/api/git/workspace/commit')
      expect(call[1].method).toBe('POST')
      expect(JSON.parse(call[1].body)).toEqual({
        message: 'Update demo stack',
        paths: ['config/demo/app.conf'],
      })
    })

    it('posts push request without body', async () => {
      mockFetch.mockReturnValueOnce(jsonResponse({
        pushed: true,
        remote: 'origin',
        branch: 'main',
        upstream_name: 'origin/main',
        head_commit: 'abc1234',
        ahead_count: 0,
        behind_count: 0,
      }))

      await pushGitWorkspace()

      const call = mockFetch.mock.calls[0]
      expect(call[0]).toBe('/api/git/workspace/push')
      expect(call[1].method).toBe('POST')
      expect(call[1].body).toBeUndefined()
    })
  })

  describe('maintenance schedules', () => {
    it('fetches maintenance schedules', async () => {
      mockFetch.mockReturnValueOnce(jsonResponse({
        timezone: 'host_local',
        update: { enabled: false, frequency: 'weekly', time: '03:30', weekdays: ['sat'], target: { mode: 'all' }, options: { pull_images: true, build_images: true, remove_orphans: true, prune_after: false, include_volumes: false }, status: {} },
        prune: { enabled: false, frequency: 'weekly', time: '04:30', weekdays: ['sun'], scope: { images: true, build_cache: true, stopped_containers: true, volumes: false }, status: {} },
      }))

      await getMaintenanceSchedules()
      expect(mockFetch.mock.calls[0][0]).toBe('/api/settings/maintenance-schedules')
    })

    it('updates maintenance schedules', async () => {
      mockFetch.mockReturnValueOnce(jsonResponse({
        timezone: 'host_local',
        update: { enabled: true, frequency: 'daily', time: '03:30', target: { mode: 'all' }, options: { pull_images: true, build_images: true, remove_orphans: true, prune_after: false, include_volumes: false }, status: {} },
        prune: { enabled: false, frequency: 'weekly', time: '04:30', weekdays: ['sun'], scope: { images: true, build_cache: true, stopped_containers: true, volumes: false }, status: {} },
      }))

      await updateMaintenanceSchedules({
        update: {
          enabled: true,
          frequency: 'daily',
          time: '03:30',
          target: { mode: 'all' },
          options: { pull_images: true, build_images: true, remove_orphans: true, prune_after: false, include_volumes: false },
        },
        prune: {
          enabled: false,
          frequency: 'weekly',
          time: '04:30',
          weekdays: ['sun'],
          scope: { images: true, build_cache: true, stopped_containers: true, volumes: false },
        },
      })

      const call = mockFetch.mock.calls[0]
      expect(call[0]).toBe('/api/settings/maintenance-schedules')
      expect(call[1].method).toBe('PUT')
      expect(JSON.parse(call[1].body)).toEqual({
        update: {
          enabled: true,
          frequency: 'daily',
          time: '03:30',
          target: { mode: 'all' },
          options: { pull_images: true, build_images: true, remove_orphans: true, prune_after: false, include_volumes: false },
        },
        prune: {
          enabled: false,
          frequency: 'weekly',
          time: '04:30',
          weekdays: ['sun'],
          scope: { images: true, build_cache: true, stopped_containers: true, volumes: false },
        },
      })
    })
  })

  describe('error handling', () => {
    it('throws ApiClientError on 409', async () => {
      mockFetch.mockReturnValueOnce(errorResponse(409, {
        code: 'stack_locked',
        message: 'A mutating job is already running.',
      }))

      try {
        await getStack('locked-stack')
        expect.fail('should throw')
      } catch (err) {
        expect(err).toBeInstanceOf(ApiClientError)
        const apiErr = err as ApiClientError
        expect(apiErr.status).toBe(409)
        expect(apiErr.code).toBe('stack_locked')
        expect(apiErr.message).toBe('A mutating job is already running.')
      }
    })

    it('throws ApiClientError on 401', async () => {
      mockFetch.mockReturnValueOnce(errorResponse(401, {
        code: 'unauthorized',
        message: 'Authentication required.',
      }))

      try {
        await getStacks()
        expect.fail('should throw')
      } catch (err) {
        expect(err).toBeInstanceOf(ApiClientError)
        expect((err as ApiClientError).status).toBe(401)
      }
    })

    it('handles non-JSON error response', async () => {
      mockFetch.mockReturnValueOnce(Promise.resolve({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
        json: () => Promise.reject(new Error('not json')),
      }))

      try {
        await getStacks()
        expect.fail('should throw')
      } catch (err) {
        expect(err).toBeInstanceOf(ApiClientError)
        expect((err as ApiClientError).status).toBe(500)
      }
    })
  })
})
