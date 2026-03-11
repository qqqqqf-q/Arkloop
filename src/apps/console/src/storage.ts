import {
  canUseStorage,
} from '@arkloop/shared/storage'

export {
  readAccessToken as readAccessTokenFromStorage,
  writeAccessToken as writeAccessTokenToStorage,
  clearAccessToken as clearAccessTokenFromStorage,
} from '@arkloop/shared/storage'

const THEME_KEY = 'arkloop:console:theme'

export type Theme = 'system' | 'light' | 'dark'

function canUseLocalStorage(): boolean {
  return canUseStorage()
}

export function readThemeFromStorage(): Theme {
  if (!canUseLocalStorage()) return 'system'
  try {
    const raw = localStorage.getItem(THEME_KEY)
    if (raw === 'system' || raw === 'light' || raw === 'dark') return raw
    return 'system'
  } catch {
    return 'system'
  }
}

export function writeThemeToStorage(theme: Theme): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(THEME_KEY, theme)
  } catch {
    // ignore
  }
}

const LOCALE_KEY = 'arkloop:console:locale'
const CURRENT_PROJECT_KEY = 'arkloop:console:current-project'

export function readLocaleFromStorage(): import('./locales').Locale {
  if (!canUseLocalStorage()) return 'zh'
  try {
    const raw = localStorage.getItem(LOCALE_KEY)
    if (raw === 'zh' || raw === 'en') return raw
    return 'zh'
  } catch {
    return 'zh'
  }
}

export function writeLocaleToStorage(locale: import('./locales').Locale): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(LOCALE_KEY, locale)
  } catch {
    // ignore
  }
}

export function readCurrentProjectIdFromStorage(): string | null {
  if (!canUseLocalStorage()) return null
  try {
    const raw = localStorage.getItem(CURRENT_PROJECT_KEY)
    return raw && raw.trim() ? raw.trim() : null
  } catch {
    return null
  }
}

export function writeCurrentProjectIdToStorage(projectId: string | null): void {
  if (!canUseLocalStorage()) return
  try {
    if (!projectId || !projectId.trim()) {
      localStorage.removeItem(CURRENT_PROJECT_KEY)
      return
    }
    localStorage.setItem(CURRENT_PROJECT_KEY, projectId.trim())
  } catch {
    // ignore
  }
}
