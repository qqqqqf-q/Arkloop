import { act, createRef } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ChatInput, type ChatInputHandle } from '../components/ChatInput'
import { LocaleProvider } from '../contexts/LocaleContext'
import { listSelectablePersonas } from '../api'

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return {
    ...actual,
    listSelectablePersonas: vi.fn(),
    transcribeAudio: vi.fn(),
  }
})

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

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

describe('ChatInput draft persistence', () => {
  const mockedListSelectablePersonas = vi.mocked(listSelectablePersonas)
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
  const originalLocalStorage = globalThis.localStorage

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    const storage = createMemoryStorage()
    Object.defineProperty(globalThis, 'localStorage', { value: storage, configurable: true })
    Object.defineProperty(window, 'localStorage', { value: storage, configurable: true })
    mockedListSelectablePersonas.mockResolvedValue([
      { persona_key: 'normal', selector_name: 'Normal', selector_order: 1 },
    ])
  })

  afterEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
    Object.defineProperty(globalThis, 'localStorage', { value: originalLocalStorage, configurable: true })
    Object.defineProperty(window, 'localStorage', { value: originalLocalStorage, configurable: true })
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('同一作用域重挂载后恢复草稿，并与 work 作用域分离', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const inputRef = createRef<ChatInputHandle>()

    const renderInput = async (appMode: 'chat' | 'work') => {
      await act(async () => {
        root.render(
          <LocaleProvider>
            <ChatInput
              ref={inputRef}
              onSubmit={(event) => event.preventDefault()}
              accessToken="token"
              variant="welcome"
              appMode={appMode}
              draftOwnerKey="user-1"
            />
          </LocaleProvider>,
        )
      })
      await act(async () => {
        await flushMicrotasks()
      })
    }

    await renderInput('chat')

    await act(async () => {
      inputRef.current?.setValue('welcome chat draft')
      await flushMicrotasks()
    })

    await renderInput('work')

    const workTextarea = container.querySelector('textarea') as HTMLTextAreaElement | null
    expect(workTextarea?.value).toBe('')

    await act(async () => {
      inputRef.current?.setValue('welcome work draft')
      await flushMicrotasks()
    })

    await renderInput('chat')

    const restoredChatTextarea = container.querySelector('textarea') as HTMLTextAreaElement | null
    expect(restoredChatTextarea?.value).toBe('welcome chat draft')

    await renderInput('work')

    const restoredWorkTextarea = container.querySelector('textarea') as HTMLTextAreaElement | null
    expect(restoredWorkTextarea?.value).toBe('welcome work draft')

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })
})
