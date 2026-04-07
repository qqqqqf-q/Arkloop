import { act } from 'react'
import { type FormEvent } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ChatInput } from '../components/ChatInput'
import { LocaleProvider } from '../contexts/LocaleContext'
import { writeSelectedPersonaKeyToStorage } from '../storage'
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

function findButtonByText(container: HTMLElement, text: string): HTMLButtonElement | null {
  return Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.trim() === text) as HTMLButtonElement | null
}

describe('ChatInput persona selector', () => {
  const mockedListSelectablePersonas = vi.mocked(listSelectablePersonas)
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
  const originalLocalStorage = globalThis.localStorage

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    const storage = createMemoryStorage()
    Object.defineProperty(globalThis, 'localStorage', { value: storage, configurable: true })
    Object.defineProperty(window, 'localStorage', { value: storage, configurable: true })
    localStorage.clear()
    mockedListSelectablePersonas.mockResolvedValue([
      { persona_key: 'normal', selector_name: 'Normal', selector_order: 1 },
      { persona_key: 'extended-search', selector_name: 'Search', selector_order: 2 },
    ])
    writeSelectedPersonaKeyToStorage('normal')
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

  it('按动态列表循环切换并可从下拉选择人格', async () => {
    const onSubmit = vi.fn<(event: FormEvent<HTMLFormElement>, personaKey: string) => void>((event) => event.preventDefault())
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ChatInput
            onSubmit={onSubmit}
            accessToken="token"
          />
        </LocaleProvider>,
      )
    })
    await act(async () => {
      await flushMicrotasks()
    })

    expect(mockedListSelectablePersonas).toHaveBeenCalledWith('token')

    const selectorButton = findButtonByText(container, 'Normal')
    expect(selectorButton).not.toBeNull()
    if (!selectorButton) return

    await act(async () => {
      selectorButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    const searchMenuButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button !== selectorButton && button.textContent?.trim() === 'Search',
    ) as HTMLButtonElement | null
    expect(searchMenuButton).not.toBeNull()
    if (!searchMenuButton) return

    await act(async () => {
      searchMenuButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(findButtonByText(container, 'Search')).not.toBeNull()

    const searchSelectorButton = findButtonByText(container, 'Search')
    expect(searchSelectorButton).not.toBeNull()
    if (!searchSelectorButton) return

    await act(async () => {
      searchSelectorButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    const menuNormalButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button !== searchSelectorButton && button.textContent?.trim() === 'Normal',
    ) as HTMLButtonElement | null
    expect(menuNormalButton).not.toBeNull()
    if (!menuNormalButton) return

    await act(async () => {
      menuNormalButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    const form = container.querySelector('form')
    expect(form).not.toBeNull()
    if (!form) return

    await act(async () => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })

    expect(onSubmit).toHaveBeenCalledTimes(1)
    expect(onSubmit.mock.calls[0]?.[1]).toBe('normal')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('回车提交后仍由外部 value 驱动，清空时输入框应立即清空', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    function Harness() {
      return (
        <LocaleProvider>
          <ChatInput
            onSubmit={(event) => {
              event.preventDefault()
            }}
            accessToken="token"
          />
        </LocaleProvider>
      )
    }

    await act(async () => {
      root.render(<Harness />)
    })
    await act(async () => {
      await flushMicrotasks()
    })

    const textarea = container.querySelector('textarea')
    expect(textarea).not.toBeNull()
    if (!textarea) return

    await act(async () => {
      textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
      await flushMicrotasks()
    })

    expect((textarea as HTMLTextAreaElement).value).toBe('')

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
