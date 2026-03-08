const TOOL_CATALOG_REFRESH_EVENT = 'arkloop:tool-catalog-changed'
const TOOL_CATALOG_REFRESH_STORAGE_KEY = 'arkloop:tool-catalog-version'

export function notifyToolCatalogChanged(): void {
  const version = String(Date.now())
  window.localStorage.setItem(TOOL_CATALOG_REFRESH_STORAGE_KEY, version)
  window.dispatchEvent(new CustomEvent(TOOL_CATALOG_REFRESH_EVENT, { detail: version }))
}

export function subscribeToolCatalogRefresh(onChange: () => void): () => void {
  const handleEvent = () => onChange()
  const handleStorage = (event: StorageEvent) => {
    if (event.key === TOOL_CATALOG_REFRESH_STORAGE_KEY) {
      onChange()
    }
  }
  const handleVisibility = () => {
    if (document.visibilityState === 'visible') {
      onChange()
    }
  }

  window.addEventListener(TOOL_CATALOG_REFRESH_EVENT, handleEvent)
  window.addEventListener('storage', handleStorage)
  window.addEventListener('focus', handleEvent)
  document.addEventListener('visibilitychange', handleVisibility)

  return () => {
    window.removeEventListener(TOOL_CATALOG_REFRESH_EVENT, handleEvent)
    window.removeEventListener('storage', handleStorage)
    window.removeEventListener('focus', handleEvent)
    document.removeEventListener('visibilitychange', handleVisibility)
  }
}
