export function formatDesktopAppVersion(version: string | null | undefined): string {
  const normalized = version?.trim() ?? ''
  if (import.meta.env.MODE === 'development' && !import.meta.env.VITE_DEV_MOCK_APP_UPDATE?.trim()) return ''

  const match = normalized.match(/^(\d+)\.(\d+)\.(\d+)$/)
  if (!match) return normalized

  const major = Number(match[1])
  const minor = Number(match[2])
  const patch = Number(match[3])
  const day = Math.floor(patch / 100)
  const daily = patch % 100
  if (major < 26 || minor < 1 || minor > 12 || day < 1 || day > 31 || patch < 100) return normalized

  return `${match[1]}.${match[2]}.${day}.${daily}`
}
