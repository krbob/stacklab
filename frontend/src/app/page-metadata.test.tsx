import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'
import { PageMetadataProvider } from './page-metadata'
import { useStackPageIdentity } from './stack-page-identity'

describe('PageMetadataProvider', () => {
  beforeEach(() => {
    document.title = 'Previous screen | Stacklab'
    document.head.querySelector('meta[name="description"]')?.remove()
  })

  it.each([
    ['/', 'Stacks'],
    ['/stacks', 'Stacks'],
    ['/stacks/new', 'Create stack'],
    ['/host', 'Host'],
    ['/config', 'Config'],
    ['/maintenance', 'Maintenance'],
    ['/docker', 'Docker'],
    ['/audit', 'Audit log'],
    ['/settings', 'Settings'],
    ['/login', 'Log in'],
    ['/missing', 'Page not found'],
  ])('sets the document title for %s', (path, screenName) => {
    renderMetadata(path, <h1>{screenName}</h1>)

    expect(screen.getByRole('heading', { level: 1, name: screenName })).toBeInTheDocument()
    expect(document.title).toBe(`${screenName} | Stacklab`)
  })

  it.each([
    ['/settings', 'Settings', 'Manage Stacklab security, notifications, automation, updates, and system information.'],
    ['/settings/security', 'Settings — Security', 'Manage the Stacklab password and host observability privacy preferences.'],
    ['/settings/notifications', 'Settings — Notifications', 'Configure Stacklab notification channels, delivery events, and connection tests.'],
    ['/settings/automation', 'Settings — Automation', 'Configure recurring stack updates and Docker cleanup schedules in Stacklab.'],
    ['/settings/updates', 'Settings — Updates', 'Check for and apply Stacklab application updates.'],
    ['/settings/about', 'Settings — About', 'Review Stacklab, Docker Engine, Docker Compose, and environment details.'],
  ])('sets settings metadata for %s', (path, screenName, description) => {
    renderMetadata(path, <h1>{screenName}</h1>)

    expect(document.title).toBe(`${screenName} | Stacklab`)
    expect(document.head.querySelector('meta[name="description"]')).toHaveAttribute('content', description)
  })

  it('uses fallback metadata for an unknown route', () => {
    renderMetadata('/settings/unknown', <h1>Page not found</h1>)

    expect(document.title).toBe('Page not found | Stacklab')
    expect(document.head.querySelector('meta[name="description"]')).toHaveAttribute(
      'content',
      'Manage Docker Compose stacks, host health, updates, and maintenance from a host-native control panel.',
    )
  })

  it('uses the route stack ID while stack data is loading or unavailable', () => {
    renderMetadata('/stacks/missing/editor', <h1>Stack missing</h1>)

    expect(document.title).toBe('Editor — missing | Stacklab')
    expect(document.title).not.toContain('Previous screen')
  })

  it.each([
    ['/stacks/demo', 'Overview'],
    ['/stacks/demo/editor', 'Editor'],
    ['/stacks/demo/files', 'Files'],
    ['/stacks/demo/logs', 'Logs'],
    ['/stacks/demo/stats', 'Stats'],
    ['/stacks/demo/terminal', 'Terminal'],
    ['/stacks/demo/audit', 'History'],
  ])('maps the stack view title for %s', (path, viewName) => {
    renderMetadata(path, <h1>Stack demo</h1>)

    expect(document.title).toBe(`${viewName} — demo | Stacklab`)
  })

  it('adds the resolved stack name and ID to a stack view title', async () => {
    renderMetadata('/stacks/media/logs', <StackIdentityHeading id="media" name="Media services" />)

    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    await waitFor(() => {
      expect(document.title).toBe('Logs — Media services (media) | Stacklab')
    })
  })
})

function StackIdentityHeading({ id, name }: { id: string; name: string }) {
  useStackPageIdentity({ id, name })
  return <h1>{name}</h1>
}

function renderMetadata(path: string, content: React.ReactNode) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <PageMetadataProvider>{content}</PageMetadataProvider>
    </MemoryRouter>,
  )
}
