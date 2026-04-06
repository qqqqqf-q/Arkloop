import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AppUIProvider, useSidebarUI } from '../contexts/app-ui'
import { AuthContextBridge, type AuthContextValue } from '../contexts/auth'

vi.mock('@arkloop/shared/desktop', () => ({
  isDesktop: () => true,
}))

function SidebarProbe() {
  const { sidebarCollapsed, toggleSidebar } = useSidebarUI()

  return (
    <div>
      <button type="button" onClick={() => toggleSidebar('sidebar')}>
        toggle
      </button>
      <span data-testid="collapsed">{sidebarCollapsed ? 'collapsed' : 'expanded'}</span>
    </div>
  )
}

describe('AppUIProvider sidebar state', () => {
  const authValue: AuthContextValue = {
    me: null,
    meLoaded: true,
    accessToken: 'token',
    logout: vi.fn(),
    updateMe: vi.fn(),
  }

  const originalInnerWidth = window.innerWidth
  const originalActEnvironment = (globalThis as typeof globalThis & {
    IS_REACT_ACT_ENVIRONMENT?: boolean
  }).IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => setTimeout(() => cb(0), 0))
    vi.stubGlobal('cancelAnimationFrame', (id: number) => clearTimeout(id))
    Object.defineProperty(window, 'innerWidth', {
      configurable: true,
      writable: true,
      value: 1400,
    })
    ;(globalThis as typeof globalThis & {
      IS_REACT_ACT_ENVIRONMENT?: boolean
    }).IS_REACT_ACT_ENVIRONMENT = true
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
    Object.defineProperty(window, 'innerWidth', {
      configurable: true,
      writable: true,
      value: originalInnerWidth,
    })
    if (originalActEnvironment === undefined) {
      delete (globalThis as typeof globalThis & {
        IS_REACT_ACT_ENVIRONMENT?: boolean
      }).IS_REACT_ACT_ENVIRONMENT
    } else {
      ;(globalThis as typeof globalThis & {
        IS_REACT_ACT_ENVIRONMENT?: boolean
      }).IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('保留宽屏下的手动折叠状态，即使后续触发 resize', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/']}>
          <AuthContextBridge value={authValue}>
            <AppUIProvider>
              <SidebarProbe />
            </AppUIProvider>
          </AuthContextBridge>
        </MemoryRouter>,
      )
    })

    const toggleButton = container.querySelector('button')
    const collapsedState = container.querySelector('[data-testid="collapsed"]')
    expect(toggleButton).not.toBeNull()
    expect(collapsedState?.textContent).toBe('expanded')

    await act(async () => {
      toggleButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    expect(collapsedState?.textContent).toBe('collapsed')

    await act(async () => {
      Object.defineProperty(window, 'innerWidth', {
        configurable: true,
        writable: true,
        value: 1300,
      })
      window.dispatchEvent(new Event('resize'))
      vi.runAllTimers()
    })

    expect(collapsedState?.textContent).toBe('collapsed')

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
