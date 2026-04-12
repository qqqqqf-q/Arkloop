import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import {
  clearInputDraft,
  readInputDraftAttachments,
  readInputDraftText,
  writeInputDraftAttachments,
  writeInputDraftText,
  type InputDraftScope,
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

describe('input draft storage', () => {
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

  it('welcome chat/work/search 草稿互相隔离', () => {
    const welcomeChat: InputDraftScope = { ownerKey: 'user-1', page: 'welcome', appMode: 'chat' }
    const welcomeWork: InputDraftScope = { ownerKey: 'user-1', page: 'welcome', appMode: 'work' }
    const welcomeSearch: InputDraftScope = { ownerKey: 'user-1', page: 'welcome', appMode: 'chat', searchMode: true }

    writeInputDraftText(welcomeChat, 'chat draft')
    writeInputDraftText(welcomeWork, 'work draft')
    writeInputDraftText(welcomeSearch, 'search draft')

    expect(readInputDraftText(welcomeChat)).toBe('chat draft')
    expect(readInputDraftText(welcomeWork)).toBe('work draft')
    expect(readInputDraftText(welcomeSearch)).toBe('search draft')
  })

  it('线程草稿按线程与附件一起隔离', () => {
    const threadA: InputDraftScope = { ownerKey: 'user-1', page: 'thread', threadId: 'thread-a', appMode: 'chat' }
    const threadB: InputDraftScope = { ownerKey: 'user-1', page: 'thread', threadId: 'thread-b', appMode: 'chat' }

    writeInputDraftText(threadA, 'alpha')
    writeInputDraftAttachments(threadA, [{
      id: 'att-1',
      name: 'cat.png',
      size: 12,
      mime_type: 'image/png',
      status: 'ready',
      uploaded: {
        key: 'staging/user-1/cat.png',
        filename: 'cat.png',
        mime_type: 'image/png',
        size: 12,
        kind: 'image',
      },
    }])

    expect(readInputDraftText(threadA)).toBe('alpha')
    expect(readInputDraftText(threadB)).toBe('')
    expect(readInputDraftAttachments(threadA)).toHaveLength(1)
    expect(readInputDraftAttachments(threadB)).toHaveLength(0)
  })

  it('清空文本和附件后移除草稿', () => {
    const scope: InputDraftScope = { ownerKey: 'user-1', page: 'welcome', appMode: 'chat' }

    writeInputDraftText(scope, 'draft')
    writeInputDraftAttachments(scope, [{
      id: 'att-1',
      name: 'note.txt',
      size: 10,
      mime_type: 'text/plain',
      status: 'ready',
      uploaded: {
        key: 'staging/user-1/note.txt',
        filename: 'note.txt',
        mime_type: 'text/plain',
        size: 10,
        kind: 'file',
        extracted_text: 'hello',
      },
      pasted: { text: 'hello', lineCount: 1 },
    }])

    clearInputDraft(scope)

    expect(readInputDraftText(scope)).toBe('')
    expect(readInputDraftAttachments(scope)).toEqual([])
  })
})
