export function jobStepLabel(action: string): string {
  if (action === 'backup_before') return 'Back up selected data'
  if (action === 'prepare_rollback') return 'Preserve previous images'
  if (action === 'rollback_images') return 'Roll back images and verify recovery'
  if (action === 'verify_before') return 'Check readiness before update'
  if (action === 'verify_after') return 'Check readiness after update'
  return action
}
