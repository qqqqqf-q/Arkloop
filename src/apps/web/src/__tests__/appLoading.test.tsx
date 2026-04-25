import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '@arkloop/shared'

import App from '../App'
import { LocaleProvider } from '../contexts/LocaleContext'

const {
  restoreAccessSession,
  setUnauthenticatedHandler,
  setAccessTokenHandler,
  setSessionExpiredHandler,
  setClientApp,
} = vi.hoisted(() => ({
  restoreAccessSession: vi.fn(),
  setUnauthenticatedHandler: vi.fn(),
  setAccessTokenHandler: vi.fn(),
  setSessionExpiredHandler: vi.fn(),
  setClientApp: vi.fn(),
}))

vi.mock('../api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api')>()
  return {
    ...actual,
    restoreAccessSession,
    setUnauthenticatedHandler,
    setAccessTokenHandler,
    setSessionExpiredHandler,
  }
})

vi.mock('@arkloop/shared/api', () => ({
  setClientApp,
}))

vi.mock('../storage', async () => {
  const actual = await vi.importActual<typeof import('../storage')>('../storage')
  return {
    ...actual,
    readLocaleFromStorage: vi.fn(() => 'zh'),
    writeLocaleToStorage: vi.fn(),
  }
})

describe('App loading state', () => {
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
  const originalRAF = globalThis.requestAnimationFrame
  const originalCAF = globalThis.cancelAnimationFrame

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => {
      cb(performance.now())
      return 0
    }
    globalThis.cancelAnimationFrame = () => {}
    restoreAccessSession.mockReset()
    setUnauthenticatedHandler.mockReset()
    setAccessTokenHandler.mockReset()
    setSessionExpiredHandler.mockReset()
    setClientApp.mockReset()
    restoreAccessSession.mockReturnValue(new Promise(() => {}))
  })

  afterEach(() => {
    vi.clearAllMocks()
    globalThis.requestAnimationFrame = originalRAF
    globalThis.cancelAnimationFrame = originalCAF
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('等待刷新会话时应显示全屏加载页而不是空白', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ToastProvider>
            <MemoryRouter initialEntries={['/t/thread-1']}>
              <App />
            </MemoryRouter>
          </ToastProvider>
        </LocaleProvider>,
      )
    })

    expect(restoreAccessSession).toHaveBeenCalledTimes(1)
    expect(container.textContent).toContain('Arkloop')
    expect(container.textContent).toContain('加载中...')

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
