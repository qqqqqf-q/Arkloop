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

function toggleButtonForLabel(text: string): HTMLButtonElement {
  const label = Array.from(container.querySelectorAll('div')).find((item) => item.textContent?.includes(text))
  if (!label) throw new Error(`label not found: ${text}`)
  const row = label.closest('div[class*="justify-between"]') ?? label.parentElement?.parentElement
  const button = row?.querySelector('button')
  if (!button) throw new Error(`button not found for: ${text}`)
  return button as HTMLButtonElement
}

describe('DeveloperSettings', () => {
  it('初次加载读取 Pipeline Trace 状态', async () => {
    const getAccountSettings = vi.fn().mockResolvedValue({ pipeline_trace_enabled: true })

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
    expect(toggleButtonForLabel('Pipeline Trace').textContent).toContain('ON')
  })

  it('切换失败时回滚并提示错误', async () => {
    const getAccountSettings = vi.fn().mockResolvedValue({ pipeline_trace_enabled: false })
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

    const pipelineButton = toggleButtonForLabel('Pipeline Trace')
    expect(pipelineButton.textContent).toContain('OFF')

    await act(async () => {
      pipelineButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushEffects()

    expect(updateAccountSettings).toHaveBeenCalledWith('token', { pipeline_trace_enabled: true })
    expect(toggleButtonForLabel('Pipeline Trace').textContent).toContain('OFF')
    expect(addToast).toHaveBeenCalledWith('save failed', 'error')
  })
})
