const RETRY_KEY_PREFIX = 'weknora_route_chunk_retry:'
const RETRY_COOLDOWN_MS = 30_000

interface RecoveryEnvironment {
  isOnline: () => boolean
  getStorage: () => Pick<Storage, 'getItem' | 'setItem'>
  now: () => number
  resolveHref: (fullPath: string) => string
  replace: (href: string) => void
}

export function isRouteChunkLoadError(error: unknown): boolean {
  if (!error || typeof error !== 'object' || !('message' in error)) return false
  if (typeof error.message !== 'string') return false

  // Native import failures in Chromium, Firefox and Safari, plus Vite's
  // route stylesheet preload failure. Do not retry arbitrary runtime errors.
  return /^(?:Failed to fetch dynamically imported module(?:\s*:|$)|error loading dynamically imported module(?:\s*:|$)|Importing a module script failed\.?$|Unable to preload CSS for )/i.test(error.message)
}

/** Install only on router.onError: unrelated lazy components may contain unsaved work. */
export function createRouteChunkRecovery(environment: RecoveryEnvironment) {
  return (error: unknown, fullPath: string): boolean => {
    if (!isRouteChunkLoadError(error) || !environment.isOnline()) return false

    try {
      const storage = environment.getStorage()
      const key = `${RETRY_KEY_PREFIX}${fullPath}`
      const now = environment.now()
      const previous = storage.getItem(key)
      if (previous !== null && now - Number(previous) < RETRY_COOLDOWN_MS) return false

      // Persist before navigating so the fresh document cannot reload in a loop.
      // If storage is blocked or silently ignores writes, leave the page intact.
      storage.setItem(key, String(now))
      if (storage.getItem(key) !== String(now)) return false

      environment.replace(environment.resolveHref(fullPath))
      return true
    } catch {
      return false
    }
  }
}
