import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import type { ThemeBackgroundImage } from '../themes/types'
import {
  readBackgroundImageFromStorage,
  readBackgroundImageOpacityFromStorage,
  readThemePresetFromStorage,
  writeBackgroundImageToStorage,
  writeBackgroundImageOpacityToStorage,
  writeThemePresetToStorage,
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

describe('appearance storage', () => {
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

  it('读写背景图片', () => {
    const image: ThemeBackgroundImage = {
      dataUrl: 'data:image/webp;base64,aGVsbG8=',
      name: 'wall.webp',
      mimeType: 'image/webp',
      size: 5,
      updatedAt: 1710000000000,
    }

    expect(writeBackgroundImageToStorage(image)).toBe(true)
    expect(readBackgroundImageFromStorage()).toEqual(image)
  })

  it('移除背景图片', () => {
    const image: ThemeBackgroundImage = {
      dataUrl: 'data:image/jpeg;base64,aGVsbG8=',
      name: 'wall.jpg',
      mimeType: 'image/jpeg',
      size: 5,
      updatedAt: 1710000000000,
    }

    writeBackgroundImageToStorage(image)
    expect(writeBackgroundImageToStorage(null)).toBe(true)
    expect(readBackgroundImageFromStorage()).toBeNull()
  })

  it('忽略无效 data url', () => {
    localStorage.setItem('arkloop:web:background-image', JSON.stringify({
      dataUrl: 'javascript:alert(1)',
      name: 'bad',
      mimeType: 'image/svg+xml',
      size: 0,
      updatedAt: 1710000000000,
    }))

    expect(readBackgroundImageFromStorage()).toBeNull()
  })

  it('读写背景透明度并约束范围', () => {
    expect(readBackgroundImageOpacityFromStorage()).toBe(100)

    writeBackgroundImageOpacityToStorage(42)
    expect(readBackgroundImageOpacityFromStorage()).toBe(42)

    writeBackgroundImageOpacityToStorage(180)
    expect(readBackgroundImageOpacityFromStorage()).toBe(100)

    writeBackgroundImageOpacityToStorage(-20)
    expect(readBackgroundImageOpacityFromStorage()).toBe(0)
  })

  it('stores the custom background color scheme', () => {
    writeThemePresetToStorage('background-image')
    expect(readThemePresetFromStorage()).toBe('background-image')
  })
})
