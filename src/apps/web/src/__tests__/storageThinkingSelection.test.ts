import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import {
  readSelectedReasoningMode,
  transferGlobalThinkingToThread,
  writeSelectedReasoningMode,
  readThreadReasoningMode,
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

describe('thinking storage', () => {
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

  it('默认关闭', () => {
    expect(readSelectedReasoningMode()).toBe('off')
  })

  it('写入全局 think 后可迁移到新线程', () => {
    writeSelectedReasoningMode('medium')
    transferGlobalThinkingToThread('thread_1')

    expect(readSelectedReasoningMode()).toBe('medium')
    expect(readThreadReasoningMode('thread_1')).toBe('medium')
  })

  it('旧值 true 向后兼容返回 medium', () => {
    localStorage.setItem('arkloop:web:selected_thinking', 'true')
    expect(readSelectedReasoningMode()).toBe('medium')
  })

  it('旧值 false 向后兼容返回 off', () => {
    localStorage.setItem('arkloop:web:selected_thinking', 'false')
    expect(readSelectedReasoningMode()).toBe('off')
  })
})
