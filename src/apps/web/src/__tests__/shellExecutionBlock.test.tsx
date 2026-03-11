import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ShellExecutionBlock } from '../components/ShellExecutionBlock'
import { LocaleProvider } from '../contexts/LocaleContext'

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

describe('ShellExecutionBlock', () => {
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
  const originalLocalStorage = globalThis.localStorage
  const originalScrollTo = window.scrollTo

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    const storage = createMemoryStorage()
    Object.defineProperty(globalThis, 'localStorage', { value: storage, configurable: true })
    Object.defineProperty(window, 'localStorage', { value: storage, configurable: true })
    Object.defineProperty(window, 'scrollTo', { value: vi.fn(), configurable: true })
  })

  afterEach(() => {
    Object.defineProperty(globalThis, 'localStorage', { value: originalLocalStorage, configurable: true })
    Object.defineProperty(window, 'localStorage', { value: originalLocalStorage, configurable: true })
    Object.defineProperty(window, 'scrollTo', { value: originalScrollTo, configurable: true })
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('运行中展开后应显示正文加载动画', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ShellExecutionBlock code="python3 /tmp/script.py" status="running" />
        </LocaleProvider>,
      )
    })

    expect(container.querySelectorAll('.animate-spin')).toHaveLength(0)
    expect(container.textContent).not.toContain('无输出')

    const button = container.querySelector('[role="button"]')
    expect(button).not.toBeNull()
    if (!button) return

    await act(async () => {
      button.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(container.querySelectorAll('.animate-spin')).toHaveLength(2)
    expect(container.textContent).not.toContain('无输出')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('失败态应显示失败而不是成功', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ShellExecutionBlock code="ls -la /workspace/" status="failed" errorMessage="profile_ref and workspace_ref are required" />
        </LocaleProvider>,
      )
    })

    const trigger = container.querySelector('[role="button"]')
    expect(trigger).not.toBeNull()
    if (!trigger) return

    await act(async () => {
      trigger.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(container.textContent).toContain('失败')
    expect(container.textContent).not.toContain('成功')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('无明确成功失败证据时应显示完成', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ShellExecutionBlock code="ls -la /workspace/" status="completed" />
        </LocaleProvider>,
      )
    })

    const trigger = container.querySelector('[role="button"]')
    expect(trigger).not.toBeNull()
    if (!trigger) return

    await act(async () => {
      trigger.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(container.textContent).toContain('完成')
    expect(container.textContent).toContain('无输出')

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
