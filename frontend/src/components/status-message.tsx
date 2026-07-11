import type { HTMLAttributes, ReactNode } from 'react'

interface StatusMessageProps extends Omit<HTMLAttributes<HTMLDivElement>, 'children' | 'role'> {
  children: ReactNode
}

/** Polite, atomic feedback for user-triggered actions such as save or push. */
export function StatusMessage({ children, ...props }: StatusMessageProps) {
  return (
    <div {...props} role="status" aria-live="polite" aria-atomic="true">
      {children}
    </div>
  )
}
