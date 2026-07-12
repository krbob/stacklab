const modalLayers: symbol[] = []
let bodyOverflowBeforeModal: string | null = null

export function hasActiveModal(): boolean {
  return Boolean(document.querySelector('[role="dialog"][aria-modal="true"]'))
}

export function registerModalLayer(layer: symbol): () => void {
  if (modalLayers.length === 0) {
    bodyOverflowBeforeModal = document.body.style.overflow
    document.body.style.overflow = 'hidden'
  }
  modalLayers.push(layer)

  let registered = true
  return () => {
    if (!registered) return
    registered = false
    const index = modalLayers.lastIndexOf(layer)
    if (index !== -1) modalLayers.splice(index, 1)
    if (modalLayers.length === 0 && bodyOverflowBeforeModal !== null) {
      document.body.style.overflow = bodyOverflowBeforeModal
      bodyOverflowBeforeModal = null
    }
  }
}

export function isTopmostModalLayer(layer: symbol): boolean {
  return modalLayers[modalLayers.length - 1] === layer
}
