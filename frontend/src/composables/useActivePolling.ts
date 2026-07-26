import { onActivated, onDeactivated, onMounted, onUnmounted } from 'vue'

export function useActivePolling(start: () => void | Promise<void>, stop: () => void) {
  let active = false
  let startPromise: Promise<void> | null = null

  const resume = () => {
    if (!active || document.hidden || startPromise) return
    startPromise = Promise.resolve(start()).finally(() => {
      startPromise = null
      if (!active || document.hidden) stop()
    })
  }

  const handleVisibilityChange = () => {
    if (document.hidden) {
      stop()
      return
    }
    resume()
  }

  onMounted(() => {
    document.addEventListener('visibilitychange', handleVisibilityChange)
  })

  onActivated(() => {
    active = true
    resume()
  })

  onDeactivated(() => {
    active = false
    stop()
  })

  onUnmounted(() => {
    active = false
    stop()
    document.removeEventListener('visibilitychange', handleVisibilityChange)
  })
}
