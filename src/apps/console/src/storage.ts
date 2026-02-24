// 与 web app 共享同一 localStorage key，保证同域登录态共享
const ACCESS_TOKEN_KEY = 'arkloop:web:access_token'
const REFRESH_TOKEN_KEY = 'arkloop:console:refresh_token'
const THEME_KEY = 'arkloop:console:theme'

export type Theme = 'system' | 'light' | 'dark'

function canUseLocalStorage(): boolean {
  try {
    return typeof localStorage !== 'undefined'
  } catch {
    return false
  }
}

export function readAccessTokenFromStorage(): string | null {
  if (!canUseLocalStorage()) return null
  try {
    const raw = localStorage.getItem(ACCESS_TOKEN_KEY)
    return raw?.trim() ? raw : null
  } catch {
    return null
  }
}

export function writeAccessTokenToStorage(token: string): void {
  if (!canUseLocalStorage() || !token.trim()) return
  try {
    localStorage.setItem(ACCESS_TOKEN_KEY, token)
  } catch {
    // ignore
  }
}

export function clearAccessTokenFromStorage(): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.removeItem(ACCESS_TOKEN_KEY)
  } catch {
    // ignore
  }
}

export function readRefreshTokenFromStorage(): string | null {
  if (!canUseLocalStorage()) return null
  try {
    const raw = localStorage.getItem(REFRESH_TOKEN_KEY)
    return raw?.trim() ? raw : null
  } catch {
    return null
  }
}

export function writeRefreshTokenToStorage(token: string): void {
  if (!canUseLocalStorage() || !token.trim()) return
  try {
    localStorage.setItem(REFRESH_TOKEN_KEY, token)
  } catch {
    // ignore
  }
}

export function clearRefreshTokenFromStorage(): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.removeItem(REFRESH_TOKEN_KEY)
  } catch {
    // ignore
  }
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
