import { act } from 'react'
import { type FormEvent } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ChatInput } from '../components/ChatInput'
import { PersonaModelBar } from '../components/chat-input/PersonaModelBar'
import { LocaleProvider } from '../contexts/LocaleContext'
import { writeSelectedPersonaKeyToStorage } from '../storage'
import { listLlmProviders, listSelectablePersonas } from '../api'
import { measureTextareaHeight } from '@arkloop/shared'

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return {
    ...actual,
    listSelectablePersonas: vi.fn(),
    listLlmProviders: vi.fn(),
    transcribeAudio: vi.fn(),
  }
})

vi.mock('@arkloop/shared/desktop', () => ({
  isDesktop: () => true,
  getDesktopApi: () => null,
}))

vi.mock('../contexts/thread-list', () => ({
  useThreadList: () => ({
    threads: [],
    upsertThread: vi.fn(),
  }),
}))

vi.mock('@arkloop/shared', async () => {
  const actual = await vi.importActual<typeof import('@arkloop/shared')>('@arkloop/shared')
  return {
    ...actual,
    measureTextareaHeight: vi.fn(actual.measureTextareaHeight),
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

function setTextareaValue(textarea: HTMLTextAreaElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set
  if (setter) {
    setter.call(textarea, value)
    return
  }
  textarea.value = value
}

type TextareaReactProps = {
  onChange: (event: { currentTarget: HTMLTextAreaElement }) => void
  onCompositionStart: () => void
  onCompositionEnd: (event: { currentTarget: HTMLTextAreaElement }) => void
}

function readTextareaReactProps(textarea: HTMLTextAreaElement): TextareaReactProps {
  const propsKey = Object.keys(textarea).find((key) => key.startsWith('__reactProps$'))
  expect(propsKey).toBeTruthy()
  return (textarea as unknown as Record<string, TextareaReactProps>)[propsKey!]
}

describe('ChatInput persona selector', () => {
  const mockedListSelectablePersonas = vi.mocked(listSelectablePersonas)
  const mockedListLlmProviders = vi.mocked(listLlmProviders)
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
    mockedListLlmProviders.mockResolvedValue([])
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

  it('Work compact 单行输入框不渲染文件夹 Picker', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ChatInput
            onSubmit={(event) => event.preventDefault()}
            accessToken="token"
            variant="chat"
            appMode="work"
            hasMessages={false}
            messagesLoading={false}
          />
        </LocaleProvider>,
      )
    })
    await act(async () => {
      await flushMicrotasks()
    })

    expect(container.textContent).not.toContain('Work in a folder')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('Work compact 组合输入期间不因换行测量替换 textarea', async () => {
    const mockedMeasureTextareaHeight = vi.mocked(measureTextareaHeight)
    mockedMeasureTextareaHeight.mockImplementation(({ value }) => (
      value.length > 15 ? 40 : 20
    ))
    const originalGetComputedStyle = window.getComputedStyle
    vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => {
      const style = originalGetComputedStyle.call(window, element)
      Object.defineProperty(style, 'lineHeight', { value: '20px', configurable: true })
      Object.defineProperty(style, 'fontSize', { value: '16px', configurable: true })
      Object.defineProperty(style, 'font', { value: '16px sans-serif', configurable: true })
      return style
    })
    Object.defineProperty(HTMLTextAreaElement.prototype, 'clientWidth', { configurable: true, value: 120 })
    Object.defineProperty(HTMLTextAreaElement.prototype, 'offsetWidth', { configurable: true, value: 120 })

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ChatInput
            onSubmit={(event) => event.preventDefault()}
            accessToken="token"
            variant="chat"
            appMode="work"
            hasMessages={false}
            messagesLoading={false}
          />
        </LocaleProvider>,
      )
    })
    await act(async () => {
      await flushMicrotasks()
    })

    const compactTextarea = container.querySelector('textarea') as HTMLTextAreaElement | null
    expect(compactTextarea).not.toBeNull()
    if (!compactTextarea) return

    await act(async () => {
      compactTextarea.focus()
      const props = readTextareaReactProps(compactTextarea)
      props.onCompositionStart()
      const value = '这是一段足够长但仍然是单行的中文组合输入内容'
      setTextareaValue(compactTextarea, value)
      compactTextarea.setSelectionRange(value.length, value.length)
      expect(compactTextarea.value.length).toBeGreaterThan(15)
      props.onChange({ currentTarget: compactTextarea })
      await flushMicrotasks()
    })

    expect(container.querySelector('textarea')).toBe(compactTextarea)

    await act(async () => {
      readTextareaReactProps(compactTextarea).onCompositionEnd({ currentTarget: compactTextarea })
      await flushMicrotasks()
    })

    expect(mockedMeasureTextareaHeight).toHaveBeenCalled()
    expect(mockedMeasureTextareaHeight.mock.calls.some(([args]) => args.value.length > 15)).toBe(true)
    const expandedTextarea = container.querySelector('textarea') as HTMLTextAreaElement | null
    expect(expandedTextarea).not.toBe(compactTextarea)
    expect(document.activeElement).toBe(expandedTextarea)
    expect(expandedTextarea?.selectionStart).toBe(compactTextarea.value.length)

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('消息仍在加载时不渲染 Work 文件夹 Picker', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    const renderBar = async (threadMessagesLoading: boolean) => {
      await act(async () => {
        root.render(
          <LocaleProvider>
            <PersonaModelBar
              personas={[]}
              selectedPersonaKey="normal"
              selectedModel={null}
              isNonDefaultMode={false}
              selectedPersona={null}
              onModeSelect={() => undefined}
              onDeactivateMode={() => undefined}
              onModelChange={() => undefined}
              thinkingEnabled="off"
              onThinkingChange={() => undefined}
              onFileInputClick={() => undefined}
              appMode="work"
              variant="chat"
              threadHasMessages={false}
              threadMessagesLoading={threadMessagesLoading}
              hideModelPicker
            />
          </LocaleProvider>,
        )
      })
    }

    await renderBar(true)
    expect(container.textContent).not.toContain('Work in a folder')

    await renderBar(false)
    expect(container.textContent).toContain('Work in a folder')

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
