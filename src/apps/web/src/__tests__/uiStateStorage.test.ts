import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import {
  readDesktopSettingsStateFromStorage,
  readSidebarCollapsedFromStorage,
  writeDesktopSettingsStateToStorage,
  writeSidebarCollapsedToStorage,
} from '../storage'

function createMemoryStorage(): Storage {
  const store = new Map<string, string>()
  return {
    get length() {
      return store.size
    },
    clear() {
      store.clear()
    },
    getItem(key: string) {
      return store.has(key) ? store.get(key)! : null
    },
    key(index: number) {
      return Array.from(store.keys())[index] ?? null
    },
    removeItem(key: string) {
      store.delete(key)
    },
    setItem(key: string, value: string) {
      store.set(key, value)
    },
  }
}

describe('ui state storage', () => {
  const originalLocalStorage = globalThis.localStorage

  beforeEach(() => {
    const storage = createMemoryStorage()
    Object.defineProperty(globalThis, 'localStorage', { value: storage, configurable: true })
    Object.defineProperty(window, 'localStorage', { value: storage, configurable: true })
  })

  afterEach(() => {
    localStorage.clear()
    Object.defineProperty(globalThis, 'localStorage', { value: originalLocalStorage, configurable: true })
    Object.defineProperty(window, 'localStorage', { value: originalLocalStorage, configurable: true })
  })

  it('读写 sidebar 折叠状态', () => {
    expect(readSidebarCollapsedFromStorage()).toBeNull()

    writeSidebarCollapsedToStorage(true)
    expect(readSidebarCollapsedFromStorage()).toBe(true)

    writeSidebarCollapsedToStorage(false)
    expect(readSidebarCollapsedFromStorage()).toBe(false)
  })

  it('读写 Desktop 设置页状态', () => {
    writeDesktopSettingsStateToStorage({
      open: true,
      section: 'providers',
      advancedSection: null,
    })

    expect(readDesktopSettingsStateFromStorage()).toEqual({
      open: true,
      section: 'providers',
      advancedSection: null,
    })
  })

  it('忽略损坏的 Desktop 设置页状态', () => {
    localStorage.setItem('arkloop:web:desktop_settings_state', '{bad')

    expect(readDesktopSettingsStateFromStorage()).toBeNull()
  })
})
