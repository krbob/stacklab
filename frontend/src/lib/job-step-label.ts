export function jobStepLabel(action: string): string {
  if (action === 'verify_before') return 'Check readiness before update'
  if (action === 'verify_after') return 'Check readiness after update'
  return action
}
