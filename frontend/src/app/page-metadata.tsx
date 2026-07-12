import { useLayoutEffect, useMemo, useState, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { StackIdentityContext, type RegisteredStackIdentity } from '@/app/stack-page-identity'

const APPLICATION_NAME = 'Stacklab'
const DEFAULT_DESCRIPTION = 'Manage Docker Compose stacks, host health, updates, and maintenance from a host-native control panel.'

const globalScreenTitles: Record<string, string> = {
  '/': 'Stacks',
  '/stacks': 'Stacks',
  '/stacks/new': 'Create stack',
  '/host': 'Host',
  '/config': 'Config',
  '/maintenance': 'Maintenance',
  '/docker': 'Docker',
  '/audit': 'Audit log',
  '/login': 'Log in',
}

const settingsScreenMetadata: Record<string, PageDescriptor> = {
  '/settings': {
    screen: 'Settings',
    description: 'Manage Stacklab security, notifications, automation, updates, and system information.',
  },
  '/settings/security': {
    screen: 'Settings — Security',
    description: 'Manage the Stacklab password and host observability privacy preferences.',
  },
  '/settings/notifications': {
    screen: 'Settings — Notifications',
    description: 'Configure Stacklab notification channels, delivery events, and connection tests.',
  },
  '/settings/automation': {
    screen: 'Settings — Automation',
    description: 'Configure recurring stack updates and Docker cleanup schedules in Stacklab.',
  },
  '/settings/updates': {
    screen: 'Settings — Updates',
    description: 'Check for and apply Stacklab application updates.',
  },
  '/settings/about': {
    screen: 'Settings — About',
    description: 'Review Stacklab, Docker Engine, Docker Compose, and environment details.',
  },
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
  description?: string
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
  const description = descriptor.description ?? DEFAULT_DESCRIPTION

  useLayoutEffect(() => {
    document.title = title
    let descriptionMeta = document.head.querySelector<HTMLMetaElement>('meta[name="description"]')
    if (!descriptionMeta) {
      descriptionMeta = document.createElement('meta')
      descriptionMeta.name = 'description'
      document.head.append(descriptionMeta)
    }
    descriptionMeta.content = description
  }, [description, title])

  return (
    <StackIdentityContext.Provider value={setStackIdentity}>
      {children}
    </StackIdentityContext.Provider>
  )
}

function describePath(pathname: string): PageDescriptor {
  const normalizedPath = pathname !== '/' ? pathname.replace(/\/+$/, '') : pathname
  const settingsMetadata = settingsScreenMetadata[normalizedPath]
  if (settingsMetadata) return settingsMetadata

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
