import {
  canUseStorage,
} from '@arkloop/shared/storage'
import type { Theme } from '@arkloop/shared/contexts/theme'

export {
  readAccessToken as readAccessTokenFromStorage,
  writeAccessToken as writeAccessTokenToStorage,
  clearAccessToken as clearAccessTokenFromStorage,
} from '@arkloop/shared/storage'

const THEME_KEY = 'arkloop:lite:theme'

export function readThemeFromStorage(): Theme {
  if (!canUseStorage()) return 'system'
  try {
    const raw = localStorage.getItem(THEME_KEY)
    if (raw === 'system' || raw === 'light' || raw === 'dark') return raw
    return 'system'
  } catch {
    return 'system'
  }
}

export function writeThemeToStorage(theme: Theme): void {
  if (!canUseStorage()) return
  try {
    localStorage.setItem(THEME_KEY, theme)
  } catch {
    // ignore
  }
}

const LOCALE_KEY = 'arkloop:lite:locale'

export function readLocaleFromStorage(): import('./locales').Locale {
  if (!canUseStorage()) return 'zh'
  try {
    const raw = localStorage.getItem(LOCALE_KEY)
    if (raw === 'zh' || raw === 'en') return raw
    return 'zh'
  } catch {
    return 'zh'
  }
}

export function writeLocaleToStorage(locale: import('./locales').Locale): void {
  if (!canUseStorage()) return
  try {
    localStorage.setItem(LOCALE_KEY, locale)
  } catch {
    // ignore
  }
}
