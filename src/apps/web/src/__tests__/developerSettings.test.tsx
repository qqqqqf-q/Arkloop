import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let container: HTMLDivElement
let root: ReturnType<typeof createRoot> | null
const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
const addToast = vi.fn()

async function flushEffects() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

beforeEach(() => {
  actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})

afterEach(() => {
  if (root) act(() => root!.unmount())
  root = null
  container.remove()
  vi.doUnmock('../api')
  vi.doUnmock('../storage')
  vi.doUnmock('@arkloop/shared')
  vi.doUnmock('@arkloop/shared/desktop')
  vi.resetModules()
  vi.clearAllMocks()
  if (originalActEnvironment === undefined) {
    delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
  } else {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
  }
})

function toggleSwitchForLabel(text: string): HTMLInputElement {
  const label = Array.from(container.querySelectorAll('div.text-sm')).find((item) => item.textContent?.trim() === text)
  if (!label) throw new Error(`label not found: ${text}`)
  const row = label.closest('div[class*="justify-between"]') ?? label.parentElement?.parentElement
  const input = row?.querySelector('input[type="checkbox"]')
  if (!input) throw new Error(`switch not found for: ${text}`)
  return input as HTMLInputElement
}

describe('DeveloperSettings', () => {
  it('初次加载读取 Pipeline Trace 状态', async () => {
    const getAccountSettings = vi.fn().mockResolvedValue({ pipeline_trace_enabled: true, prompt_cache_debug_enabled: false })

    vi.doMock('../api', async () => {
      const actual = await vi.importActual<typeof import('../api')>('../api')
      return {
        ...actual,
        getAccountSettings,
        updateAccountSettings: vi.fn(),
      }
    })
    vi.doMock('../storage', async () => {
      const actual = await vi.importActual<typeof import('../storage')>('../storage')
      return {
        ...actual,
        readLocaleFromStorage: vi.fn(() => 'zh'),
        writeLocaleToStorage: vi.fn(),
      }
    })
    vi.doMock('@arkloop/shared/desktop', () => ({
      getDesktopApi: () => ({
        app: { getVersion: vi.fn().mockResolvedValue('1.0.0') },
      }),
    }))
    vi.doMock('@arkloop/shared', async () => {
      const actual = await vi.importActual<typeof import('@arkloop/shared')>('@arkloop/shared')
      return {
        ...actual,
        useToast: () => ({ addToast }),
      }
    })

    const { DeveloperSettings } = await import('../components/settings/DeveloperSettings')
    const { LocaleProvider } = await import('../contexts/LocaleContext')

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <DeveloperSettings accessToken="token" />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(getAccountSettings).toHaveBeenCalledWith('token')
    expect(toggleSwitchForLabel('Pipeline Trace').checked).toBe(true)
  })

  it('切换失败时回滚并提示错误', async () => {
    const getAccountSettings = vi.fn().mockResolvedValue({ pipeline_trace_enabled: false, prompt_cache_debug_enabled: false })
    const updateAccountSettings = vi.fn().mockRejectedValue(new Error('save failed'))

    vi.doMock('../api', async () => {
      const actual = await vi.importActual<typeof import('../api')>('../api')
      return {
        ...actual,
        getAccountSettings,
        updateAccountSettings,
      }
    })
    vi.doMock('../storage', async () => {
      const actual = await vi.importActual<typeof import('../storage')>('../storage')
      return {
        ...actual,
        readLocaleFromStorage: vi.fn(() => 'zh'),
        writeLocaleToStorage: vi.fn(),
      }
    })
    vi.doMock('@arkloop/shared/desktop', () => ({
      getDesktopApi: () => ({
        app: { getVersion: vi.fn().mockResolvedValue('1.0.0') },
      }),
    }))
    vi.doMock('@arkloop/shared', async () => {
      const actual = await vi.importActual<typeof import('@arkloop/shared')>('@arkloop/shared')
      return {
        ...actual,
        useToast: () => ({ addToast }),
      }
    })

    const { DeveloperSettings } = await import('../components/settings/DeveloperSettings')
    const { LocaleProvider } = await import('../contexts/LocaleContext')

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <DeveloperSettings accessToken="token" />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    const pipelineSwitch = toggleSwitchForLabel('Pipeline Trace')
    expect(pipelineSwitch.checked).toBe(false)

    await act(async () => {
      pipelineSwitch.click()
    })
    await flushEffects()

    expect(updateAccountSettings).toHaveBeenCalledWith('token', { pipeline_trace_enabled: true })
    expect(toggleSwitchForLabel('Pipeline Trace').checked).toBe(false)
    expect(addToast).toHaveBeenCalledWith('save failed', 'error')
  })

  it('点击 Prompt Cache 调试开关触发 PATCH 请求', async () => {
    const getAccountSettings = vi.fn().mockResolvedValue({ pipeline_trace_enabled: false, prompt_cache_debug_enabled: false })
    const updateAccountSettings = vi.fn().mockResolvedValue({ pipeline_trace_enabled: false, prompt_cache_debug_enabled: true })

    vi.doMock('../api', async () => {
      const actual = await vi.importActual<typeof import('../api')>('../api')
      return {
        ...actual,
        getAccountSettings,
        updateAccountSettings,
      }
    })
    vi.doMock('../storage', async () => {
      const actual = await vi.importActual<typeof import('../storage')>('../storage')
      return {
        ...actual,
        readLocaleFromStorage: vi.fn(() => 'zh'),
        writeLocaleToStorage: vi.fn(),
      }
    })
    vi.doMock('@arkloop/shared/desktop', () => ({
      getDesktopApi: () => ({
        app: { getVersion: vi.fn().mockResolvedValue('1.0.0') },
      }),
    }))
    vi.doMock('@arkloop/shared', async () => {
      const actual = await vi.importActual<typeof import('@arkloop/shared')>('@arkloop/shared')
      return {
        ...actual,
        useToast: () => ({ addToast }),
      }
    })

    const { DeveloperSettings } = await import('../components/settings/DeveloperSettings')
    const { LocaleProvider } = await import('../contexts/LocaleContext')

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <DeveloperSettings accessToken="token" />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    const cacheSwitch = toggleSwitchForLabel('Prompt Cache 调试')
    expect(cacheSwitch.checked).toBe(false)

    await act(async () => {
      cacheSwitch.click()
    })
    await flushEffects()

    expect(updateAccountSettings).toHaveBeenCalledWith('token', { prompt_cache_debug_enabled: true })
    expect(toggleSwitchForLabel('Prompt Cache 调试').checked).toBe(true)
  })
})
