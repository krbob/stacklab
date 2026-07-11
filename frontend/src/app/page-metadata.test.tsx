import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'
import { PageMetadataProvider } from './page-metadata'
import { useStackPageIdentity } from './stack-page-identity'

describe('PageMetadataProvider', () => {
  beforeEach(() => {
    document.title = 'Previous screen | Stacklab'
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
