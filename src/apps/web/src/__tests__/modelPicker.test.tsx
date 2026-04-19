import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ModelPicker } from '../components/ModelPicker'
import { LocaleProvider } from '../contexts/LocaleContext'
import { listLlmProviders } from '../api'

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return {
    ...actual,
    listLlmProviders: vi.fn(),
  }
})

vi.mock('@arkloop/shared/desktop', () => ({
  isDesktop: vi.fn(() => false),
}))

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

describe('ModelPicker', () => {
  const mockedListLlmProviders = vi.mocked(listLlmProviders)
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
  const originalLocalStorage = globalThis.localStorage

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    const storage = createMemoryStorage()
    Object.defineProperty(globalThis, 'localStorage', { value: storage, configurable: true })
    Object.defineProperty(window, 'localStorage', { value: storage, configurable: true })
    mockedListLlmProviders.mockResolvedValue([
      {
        id: 'provider-1',
        scope: 'account',
        provider: 'anthropic',
        name: 'cery',
        key_prefix: null,
        base_url: null,
        openai_api_mode: null,
        advanced_json: null,
        created_at: '2026-04-08T00:00:00Z',
        models: [
          {
            id: 'model-1',
            provider_id: 'provider-1',
            model: 'claude-sonnet-4-6',
            priority: 1,
            is_default: true,
            show_in_picker: true,
            tags: [],
            when: {},
            advanced_json: {
              available_catalog: {
                reasoning: true,
              },
            },
            multiplier: 1,
          },
        ],
      },
    ])
  })

  afterEach(() => {
    vi.restoreAllMocks()
    Object.defineProperty(globalThis, 'localStorage', { value: originalLocalStorage, configurable: true })
    Object.defineProperty(window, 'localStorage', { value: originalLocalStorage, configurable: true })
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('选择模型后保持展开，点外部时仍会关闭', async () => {
    const onChange = vi.fn()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ModelPicker
            accessToken="token"
            value={null}
            onChange={onChange}
            onAddApiKey={vi.fn()}
            thinkingEnabled={'off'}
            onThinkingChange={vi.fn()}
          />
        </LocaleProvider>,
      )
    })

    const trigger = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent?.includes('默认'),
    ) as HTMLButtonElement | null
    expect(trigger).not.toBeNull()
    if (!trigger) return

    await act(async () => {
      trigger.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    expect(mockedListLlmProviders).toHaveBeenCalledWith('token')

    const modelButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent?.trim() === 'claude-sonnet-4-6',
    ) as HTMLButtonElement | null
    expect(modelButton).not.toBeNull()
    if (!modelButton) return

    await act(async () => {
      modelButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    expect(onChange).toHaveBeenCalledWith('cery^claude-sonnet-4-6')
    expect(container.textContent).toContain('添加 API Key')
    expect(
      Array.from(container.querySelectorAll('button')).some(
        (button) => button.textContent?.trim() === 'claude-sonnet-4-6',
      ),
    ).toBe(true)

    await act(async () => {
      document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
      await flushMicrotasks()
    })

    expect(container.textContent).not.toContain('添加 API Key')
    expect(
      Array.from(container.querySelectorAll('button')).some(
        (button) => button.textContent?.trim() === 'claude-sonnet-4-6',
      ),
    ).toBe(false)

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('点击 Options 会打开二级菜单，Thinking 整行可点击切换', async () => {
    const onThinkingChange = vi.fn()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ModelPicker
            accessToken="token"
            value="cery^claude-sonnet-4-6"
            onChange={vi.fn()}
            onAddApiKey={vi.fn()}
            thinkingEnabled="off"
            onThinkingChange={onThinkingChange}
          />
        </LocaleProvider>,
      )
    })

    const trigger = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent?.includes('claude-sonnet-4-6'),
    ) as HTMLButtonElement | null
    expect(trigger).not.toBeNull()
    if (!trigger) return

    await act(async () => {
      trigger.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    const modelRow = Array.from(container.querySelectorAll('div')).find(
      (node) => node.textContent?.includes('claude-sonnet-4-6'),
    ) as HTMLDivElement | null
    expect(modelRow).not.toBeNull()
    if (!modelRow) return

    await act(async () => {
      modelRow.dispatchEvent(new MouseEvent('mouseenter', { bubbles: true }))
      await flushMicrotasks()
    })

    const optionsButton = Array.from(container.querySelectorAll('[role="button"]')).find(
      (button) => button.textContent?.trim() === 'Options',
    ) as HTMLElement | null
    expect(optionsButton).not.toBeNull()
    if (!optionsButton) return

    await act(async () => {
      optionsButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    expect(container.textContent).toContain('Thinking')
    expect(container.textContent).toContain('Effort')

    const optionsPanel = Array.from(container.querySelectorAll('div')).find(
      (node) => node.style.transform === 'translateY(-50%)',
    ) as HTMLDivElement | null
    expect(optionsPanel).not.toBeNull()

    const thinkingRow = Array.from(container.querySelectorAll('div')).find(
      (node) => node.getAttribute('role') === 'button' && node.textContent?.includes('Thinking'),
    ) as HTMLDivElement | null
    expect(thinkingRow).not.toBeNull()
    if (!thinkingRow) return

    await act(async () => {
      thinkingRow.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    expect(onThinkingChange).toHaveBeenCalledWith('medium')

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('重复点击当前模型不会把 reasoning mode 重置掉', async () => {
    const onChange = vi.fn()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ModelPicker
            accessToken="token"
            value="cery^claude-sonnet-4-6"
            onChange={onChange}
            onAddApiKey={vi.fn()}
            thinkingEnabled="high"
            onThinkingChange={vi.fn()}
          />
        </LocaleProvider>,
      )
    })

    const trigger = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent?.includes('claude-sonnet-4-6'),
    ) as HTMLButtonElement | null
    expect(trigger).not.toBeNull()
    if (!trigger) return

    await act(async () => {
      trigger.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    const currentModelButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent?.includes('High'),
    ) as HTMLButtonElement | null
    expect(currentModelButton).not.toBeNull()
    if (!currentModelButton) return

    await act(async () => {
      currentModelButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await flushMicrotasks()
    })

    expect(onChange).not.toHaveBeenCalled()

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })
})
