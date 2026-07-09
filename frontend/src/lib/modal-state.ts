export function hasActiveModal(): boolean {
  return Boolean(document.querySelector('[role="dialog"][aria-modal="true"]'))
}
