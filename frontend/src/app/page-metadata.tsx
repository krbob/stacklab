import { useLayoutEffect, useMemo, useState, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { StackIdentityContext, type RegisteredStackIdentity } from '@/app/stack-page-identity'

const APPLICATION_NAME = 'Stacklab'

const globalScreenTitles: Record<string, string> = {
  '/': 'Stacks',
  '/stacks': 'Stacks',
  '/stacks/new': 'Create stack',
  '/host': 'Host',
  '/config': 'Config',
  '/maintenance': 'Maintenance',
  '/docker': 'Docker',
  '/audit': 'Audit log',
  '/settings': 'Settings',
  '/login': 'Log in',
}

const stackScreenTitles: Record<string, string> = {
  '': 'Overview',
  editor: 'Editor',
  files: 'Files',
  logs: 'Logs',
  stats: 'Stats',
  terminal: 'Terminal',
  audit: 'History',
}

interface PageDescriptor {
  screen: string
  stackId?: string
}

export function PageMetadataProvider({ children }: { children: ReactNode }) {
  const location = useLocation()
  const [stackIdentity, setStackIdentity] = useState<RegisteredStackIdentity | null>(null)
  const descriptor = useMemo(() => describePath(location.pathname), [location.pathname])

  const currentStackIdentity = descriptor.stackId &&
    stackIdentity?.pathname === location.pathname &&
    stackIdentity.id === descriptor.stackId
    ? stackIdentity
    : null

  const stackLabel = descriptor.stackId
    ? currentStackIdentity
      ? currentStackIdentity.name === currentStackIdentity.id
        ? currentStackIdentity.id
        : `${currentStackIdentity.name} (${currentStackIdentity.id})`
      : descriptor.stackId
    : null

  const title = stackLabel
    ? `${descriptor.screen} — ${stackLabel} | ${APPLICATION_NAME}`
    : `${descriptor.screen} | ${APPLICATION_NAME}`

  useLayoutEffect(() => {
    document.title = title
  }, [title])

  return (
    <StackIdentityContext.Provider value={setStackIdentity}>
      {children}
    </StackIdentityContext.Provider>
  )
}

function describePath(pathname: string): PageDescriptor {
  const normalizedPath = pathname !== '/' ? pathname.replace(/\/+$/, '') : pathname
  const globalTitle = globalScreenTitles[normalizedPath]
  if (globalTitle) return { screen: globalTitle }

  const segments = normalizedPath.split('/').filter(Boolean)
  if (segments[0] === 'stacks' && segments[1] && segments[1] !== 'new' && segments.length <= 3) {
    const suffix = segments[2] ?? ''
    const screen = stackScreenTitles[suffix]
    if (screen) {
      return { screen, stackId: decodePathSegment(segments[1]) }
    }
  }

  return { screen: 'Page not found' }
}

function decodePathSegment(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}
