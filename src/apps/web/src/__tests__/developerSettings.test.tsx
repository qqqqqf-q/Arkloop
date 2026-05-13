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

function toggleCheckboxForLabel(text: string): HTMLInputElement {
  const label = Array.from(container.querySelectorAll('div')).find((item) => item.textContent?.includes(text))
  if (!label) throw new Error(`label not found: ${text}`)
  const row = label.closest('div[class*="justify-between"]') ?? label.parentElement?.parentElement
  const checkbox = row?.querySelector('input[type="checkbox"]') as HTMLInputElement
  if (checkbox) return checkbox
  const button = row?.querySelector('button') as HTMLButtonElement
  if (button) throw new Error(`found button instead of checkbox for: ${text}, test may need updating`)
  throw new Error(`toggle not found for: ${text}`)
}

describe('DeveloperSettings', () => {
  it('初次加载读取 Pipeline Trace 状态', async () => {
    const getAccountSettings = vi.fn().mockResolvedValue({ pipeline_trace_enabled: true, prompt_cache_debug_enabled: false })

    vi.doMock('../api', async () => {
      const actual = await vi.importActual<typeof import('../api')>('../api')
      return {
        ...actual,
        getAccountSettings,
        updateAccountSettings: vi.fn().mockResolvedValue({ prompt_cache_debug_enabled: true, pipeline_trace_enabled: true }),
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
        PillToggle: ({
          checked,
          disabled,
          onChange,
        }: {
          checked: boolean
          disabled?: boolean
          onChange: (next: boolean) => void
        }) => (
          <button type="button" disabled={disabled} onClick={() => onChange(!checked)}>
            {checked ? 'ON' : 'OFF'}
          </button>
        ),
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
    expect(toggleCheckboxForLabel('Pipeline Trace').checked).toBe(true)
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
        PillToggle: ({
          checked,
          disabled,
          onChange,
        }: {
          checked: boolean
          disabled?: boolean
          onChange: (next: boolean) => void
        }) => (
          <button type="button" disabled={disabled} onClick={() => onChange(!checked)}>
            {checked ? 'ON' : 'OFF'}
          </button>
        ),
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

    const pipelineButton = toggleCheckboxForLabel('Pipeline Trace')
    expect(pipelineButton.checked).toBe(false)

    await act(async () => {
      pipelineButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushEffects()

    expect(updateAccountSettings).toHaveBeenCalledWith('token', { pipeline_trace_enabled: true })
    expect(toggleCheckboxForLabel('Pipeline Trace').checked).toBe(false)
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
        PillToggle: ({
          checked,
          disabled,
          onChange,
        }: {
          checked: boolean
          disabled?: boolean
          onChange: (next: boolean) => void
        }) => (
          <button type="button" disabled={disabled} onClick={() => onChange(!checked)}>
            {checked ? 'ON' : 'OFF'}
          </button>
        ),
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

    const cacheLabel = Array.from(container.querySelectorAll('div.text-sm')).find((item) => item.textContent === 'Prompt Cache 调试')
    if (!cacheLabel) throw new Error('Prompt Cache 调试 label not found')
    const cacheRow = cacheLabel.closest('div[class*="justify-between"]') as HTMLElement | null
    const cacheCheckbox = cacheRow?.querySelector('input[type="checkbox"]') as HTMLInputElement | null
    if (!cacheCheckbox) throw new Error('cache checkbox not found')
    expect(cacheCheckbox.checked).toBe(false)

    await act(async () => {
      cacheCheckbox.click()
      // click 后 React 同步处理乐观更新（setPromptCacheDebugEnabled(true) + setPromptCacheDebugSaving(true)）
      // 此时 mock 的 Promise 尚未 resolve，saving 仍为 true
      // 注：React 19 + jsdom 下 .checked 在 controlled checkbox click 后可能不同步，
      // 因此优先验证 disabled 状态以证明 handler 确实被触发
      expect(cacheCheckbox.disabled).toBe(true)
    })

    expect(updateAccountSettings).toHaveBeenCalledWith('token', { prompt_cache_debug_enabled: true })
  })
})
