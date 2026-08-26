import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let container: HTMLDivElement
let root: ReturnType<typeof createRoot> | null
const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
const addToast = vi.fn()
const listPlatformSettings = vi.fn()
const updatePlatformSetting = vi.fn()

async function flushEffects() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

async function loadSubject() {
  vi.resetModules()
  vi.doMock('../storage', async () => {
    const actual = await vi.importActual<typeof import('../storage')>('../storage')
    return {
      ...actual,
      readLocaleFromStorage: vi.fn(() => 'zh'),
      writeLocaleToStorage: vi.fn(),
    }
  })
  vi.doMock('../api-admin', async () => {
    const actual = await vi.importActual<typeof import('../api-admin')>('../api-admin')
    return {
      ...actual,
      listPlatformSettings,
      updatePlatformSetting,
    }
  })
  vi.doMock('@arkloop/shared', async () => {
    const actual = await vi.importActual<typeof import('@arkloop/shared')>('@arkloop/shared')
    return {
      ...actual,
      useToast: () => ({ addToast }),
    }
  })

  const { ChatSettings } = await import('../components/settings/ChatSettings')
  const { LocaleProvider } = await import('../contexts/LocaleContext')
  return { ChatSettings, LocaleProvider }
}

beforeEach(() => {
  actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  addToast.mockReset()
  listPlatformSettings.mockReset()
  updatePlatformSetting.mockReset()
})

afterEach(() => {
  if (root) {
    act(() => root!.unmount())
  }
  container.remove()
  root = null
  vi.doUnmock('../storage')
  vi.doUnmock('../api-admin')
  vi.doUnmock('@arkloop/shared')
  vi.resetModules()
  vi.clearAllMocks()
  if (originalActEnvironment === undefined) {
    delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
  } else {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
  }
})

describe('ChatSettings', () => {
  it('初始加载不会自动保存，也不会因缺失键触发错误', async () => {
    listPlatformSettings.mockResolvedValue([
      { key: 'context.compact.enabled', value: 'true', updated_at: '2026-03-30T00:00:00Z' },
      { key: 'context.compact.persist_trigger_context_pct', value: '80', updated_at: '2026-03-30T00:00:00Z' },
      { key: 'context.compact.target_context_pct', value: '75', updated_at: '2026-03-30T00:00:00Z' },
    ])

    const { ChatSettings, LocaleProvider } = await loadSubject()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <ChatSettings accessToken="token" />
        </LocaleProvider>,
      )
    })
    await flushEffects()
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 650))
    })

    expect(listPlatformSettings).toHaveBeenCalledWith('token')
    expect(updatePlatformSetting).not.toHaveBeenCalled()
    expect(addToast).not.toHaveBeenCalledWith('已保存', 'success')
    expect(container.textContent).not.toContain('请求失败')
  })

  it('命中统一快照时不再重复首屏请求 platform settings', async () => {
    const { ChatSettings, LocaleProvider } = await loadSubject()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <ChatSettings
            accessToken="token"
            initialSnapshot={{
              config: null,
              platformSettings: {
                'context.compact.enabled': 'true',
                'context.compact.persist_trigger_context_pct': '70',
                'context.compact.target_context_pct': '75',
              },
              platformSettingsError: '',
            }}
          />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(listPlatformSettings).not.toHaveBeenCalled()
    expect(container.textContent).toContain('70%')
  })
})
