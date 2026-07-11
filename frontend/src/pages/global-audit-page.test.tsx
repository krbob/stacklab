import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { GlobalAuditPage } from './global-audit-page'
import { JobDrawerProvider } from '@/contexts/job-drawer-context'

const mockGetGlobalAudit = vi.fn()

vi.mock('@/lib/api-client', () => ({
  getGlobalAudit: (...args: unknown[]) => mockGetGlobalAudit(...args),
}))

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location-search">{location.search}</output>
}

function renderPage(initialEntry = '/audit') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <JobDrawerProvider>
        <GlobalAuditPage />
        <LocationProbe />
      </JobDrawerProvider>
    </MemoryRouter>,
  )
}

describe('GlobalAuditPage', () => {
  beforeEach(() => {
    mockGetGlobalAudit.mockReset()
    mockGetGlobalAudit.mockResolvedValue({ items: [], next_cursor: null })
  })

  it('hydrates filters from the URL and sends local date boundaries to the server', async () => {
    renderPage('/audit?q=restart&result=failed&from=2026-07-01&to=2026-07-02')

    expect(screen.getByTestId('audit-filter')).toHaveValue('restart')
    expect(screen.getByLabelText('Audit result')).toHaveValue('failed')
    expect(screen.getByLabelText('Audit from date')).toHaveValue('2026-07-01')
    expect(screen.getByLabelText('Audit through date')).toHaveValue('2026-07-02')

    await waitFor(() => expect(mockGetGlobalAudit).toHaveBeenCalledWith({
      q: 'restart',
      result: 'failed',
      from: new Date(2026, 6, 1).toISOString(),
      to: new Date(2026, 6, 3).toISOString(),
      limit: 50,
    }, expect.any(AbortSignal)))
    expect(await screen.findByText('No audit entries match these filters')).toBeInTheDocument()
  })

  it('stores search in the URL immediately and debounces the server request', async () => {
    renderPage()
    await waitFor(() => expect(mockGetGlobalAudit).toHaveBeenCalledTimes(1))

    fireEvent.change(screen.getByTestId('audit-filter'), { target: { value: 'pull images' } })

    expect(screen.getByTestId('location-search')).toHaveTextContent('?q=pull+images')
    expect(mockGetGlobalAudit).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(mockGetGlobalAudit).toHaveBeenLastCalledWith(
      { q: 'pull images', limit: 50 },
      expect.any(AbortSignal),
    ), { timeout: 1_000 })

    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
    expect(screen.getByTestId('location-search')).toHaveTextContent('')
    await waitFor(() => expect(mockGetGlobalAudit).toHaveBeenLastCalledWith(
      { limit: 50 },
      expect.any(AbortSignal),
    ))
  })

  it('does not send an invalid local date range', async () => {
    renderPage('/audit?from=2026-07-03&to=2026-07-02')

    expect(await screen.findByRole('alert')).toHaveTextContent('The start date must not be after the end date.')
    expect(mockGetGlobalAudit).not.toHaveBeenCalled()
  })
})
