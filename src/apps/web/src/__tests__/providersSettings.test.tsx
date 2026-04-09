import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let container: HTMLDivElement
let root: ReturnType<typeof createRoot> | null
const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

const listLlmProviders = vi.fn()
const listAvailableModels = vi.fn()

async function flushEffects() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

async function loadSubject() {
  vi.resetModules()
  vi.doMock('../api', async () => {
    const actual = await vi.importActual<typeof import('../api')>('../api')
    return {
      ...actual,
      listLlmProviders,
      listAvailableModels,
      createLlmProvider: vi.fn(),
      updateLlmProvider: vi.fn(),
      deleteLlmProvider: vi.fn(),
      createProviderModel: vi.fn(),
      deleteProviderModel: vi.fn(),
      patchProviderModel: vi.fn(),
      isApiError: () => false,
    }
  })

  const { ProvidersSettings } = await import('../components/settings/ProvidersSettings')
  const { LocaleProvider } = await import('../contexts/LocaleContext')
  return { ProvidersSettings, LocaleProvider }
}

beforeEach(() => {
  actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)

  listLlmProviders.mockReset()
  listAvailableModels.mockReset()
  listLlmProviders.mockResolvedValue([
    {
      id: 'provider-1',
      name: 'OpenRouter',
      provider: 'openai',
      openai_api_mode: 'responses',
      base_url: 'https://openrouter.ai/api/v1',
      advanced_json: {},
      models: [],
    },
  ])
  listAvailableModels.mockResolvedValue({
    models: [{ id: 'openai/gpt-4o-mini', name: 'GPT-4o mini', configured: false, type: 'chat' }],
  })
})

afterEach(() => {
  if (root) {
    act(() => root!.unmount())
  }
  container.remove()
  root = null
  vi.doUnmock('../api')
  vi.resetModules()
  vi.clearAllMocks()
  if (originalActEnvironment === undefined) {
    delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
  } else {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
  }
})

describe('ProvidersSettings', () => {
  it('打开页面时不自动请求 available models，点击导入后才请求', async () => {
    const { ProvidersSettings, LocaleProvider } = await loadSubject()

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <ProvidersSettings accessToken="token" />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(listLlmProviders).toHaveBeenCalledTimes(1)
    expect(listAvailableModels).not.toHaveBeenCalled()

    const importButton = container.querySelector('button.button-secondary')
    expect(importButton).toBeTruthy()

    await act(async () => {
      importButton!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushEffects()

    expect(listAvailableModels).toHaveBeenCalledTimes(1)
  })
})
