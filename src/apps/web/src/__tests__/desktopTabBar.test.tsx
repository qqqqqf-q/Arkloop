import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { DesktopTabBar } from '../components/DesktopTabBar'
import { LocaleProvider } from '../contexts/LocaleContext'

describe('DesktopTabBar', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot> | null
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    window.localStorage.setItem('arkloop:web:locale', 'zh')
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    if (root) {
      act(() => root!.unmount())
    }
    vi.clearAllMocks()
    window.localStorage.removeItem('arkloop:web:locale')
    container.remove()
    root = null
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('在主体 Bar 中展示 chat/work，并把右侧按钮作为浏览器容器展开入口', async () => {
    const handleToggleBrowserPanel = vi.fn()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <DesktopTabBar
            appMode="chat"
            availableModes={['chat', 'work']}
            browserPanelOpen={false}
            onSetAppMode={() => {}}
            onToggleBrowserPanel={handleToggleBrowserPanel}
          />
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toContain('Chat')
    expect(container.textContent).toContain('Work')
    expect(container.textContent).not.toContain('Arkloop Docs')
    expect(container.querySelector('button[title="新建网页 Tab"]')).toBeNull()

    await act(async () => {
      container.querySelector('button[title="展开右侧浏览器"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    expect(handleToggleBrowserPanel).toHaveBeenCalledTimes(1)
  })

  it('浏览器容器展开后，主体 Bar 不再显示展开按钮', async () => {
    await act(async () => {
      root!.render(
        <LocaleProvider>
          <DesktopTabBar
            appMode="chat"
            availableModes={['chat', 'work']}
            browserPanelOpen
            onSetAppMode={() => {}}
            onToggleBrowserPanel={() => {}}
          />
        </LocaleProvider>,
      )
    })

    expect(container.querySelector('button[title="展开右侧浏览器"]')).toBeNull()
    expect(container.textContent).toContain('Chat')
    expect(container.textContent).toContain('Work')
  })
})
