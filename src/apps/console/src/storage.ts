// 与 web app 共享同一 localStorage key，保证同域登录态共享
const ACCESS_TOKEN_KEY = 'arkloop:web:access_token'
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
