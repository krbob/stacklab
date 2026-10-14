import { useEffect } from 'react'

// Browser focus and visibility events usually arrive together on tab return.
export function useVisibleRefresh(refresh: () => void, interval: number, active = true) {
  useEffect(() => {
    if (!active) return
    let queued: number | undefined
    const schedule = () => {
      if (document.visibilityState === 'hidden' || queued !== undefined) return
      queued = window.setTimeout(() => {
        queued = undefined
        if (document.visibilityState !== 'hidden') refresh()
      }, 100)
    }
    const timer = window.setInterval(() => {
      if (document.visibilityState !== 'hidden') refresh()
    }, interval)
    window.addEventListener('focus', schedule)
    document.addEventListener('visibilitychange', schedule)
    return () => {
      window.clearInterval(timer)
      window.clearTimeout(queued)
      window.removeEventListener('focus', schedule)
      document.removeEventListener('visibilitychange', schedule)
    }
  }, [active, interval, refresh])
}
