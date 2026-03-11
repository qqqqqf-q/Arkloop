import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { readMessageCodeExecutions, writeMessageCodeExecutions } from '../storage'

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

describe('readMessageCodeExecutions', () => {
  const originalLocalStorage = globalThis.localStorage

  beforeEach(() => {
    const storage = createMemoryStorage()
    Object.defineProperty(globalThis, 'localStorage', { value: storage, configurable: true })
    Object.defineProperty(window, 'localStorage', { value: storage, configurable: true })
  })

  afterEach(() => {
    Object.defineProperty(globalThis, 'localStorage', { value: originalLocalStorage, configurable: true })
    Object.defineProperty(window, 'localStorage', { value: originalLocalStorage, configurable: true })
  })

  it('旧缓存缺少 status 时应直接判无效并清除', () => {
    localStorage.setItem('arkloop:web:msg_code_exec:msg_1', JSON.stringify([
      {
        id: 'call_1',
        language: 'shell',
        code: 'ls -la',
      },
    ]))

    expect(readMessageCodeExecutions('msg_1')).toBeNull()
    expect(localStorage.getItem('arkloop:web:msg_code_exec:msg_1')).toBeNull()
  })

  it('新结构缓存应保持可读', () => {
    writeMessageCodeExecutions('msg_2', [{
      id: 'call_2',
      language: 'python',
      code: 'print(1)',
      output: '1\n',
      exitCode: 0,
      status: 'success',
    }])

    expect(readMessageCodeExecutions('msg_2')).toEqual([{
      id: 'call_2',
      language: 'python',
      code: 'print(1)',
      output: '1\n',
      exitCode: 0,
      status: 'success',
    }])
  })
})
