import { describe, expect, it, vi, beforeEach } from 'vitest'
import { getStacks, getStack, login, ApiClientError } from './api-client'

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
