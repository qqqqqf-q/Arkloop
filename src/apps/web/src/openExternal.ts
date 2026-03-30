declare global {
  interface Window {
    arkloop?: {
      app?: {
        openExternal?: (url: string) => Promise<void>
      }
    }
  }
}

export function openExternal(url: string): void {
  if (window.arkloop?.app?.openExternal) {
    void window.arkloop.app.openExternal(url)
  } else {
    window.open(url, '_blank', 'noopener,noreferrer')
  }
}
