import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let container: HTMLDivElement
let root: ReturnType<typeof createRoot> | null
const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

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
      deleteSkill: vi.fn(),
      importRegistrySkill: vi.fn(),
      importSkillFromGitHub: vi.fn(),
      importSkillFromUpload: vi.fn(),
      installSkill: vi.fn(),
      isApiError: vi.fn(() => false),
      listDefaultSkills: vi.fn(),
      listInstalledSkills: vi.fn(),
      listPlatformSkills: vi.fn(),
      replaceDefaultSkills: vi.fn(),
      searchMarketSkills: vi.fn(),
      setPlatformSkillOverride: vi.fn(),
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

  const api = await import('../api')
  const { SkillsSettingsContent } = await import('../components/SkillsSettingsContent')
  const { LocaleProvider } = await import('../contexts/LocaleContext')
  return { api, SkillsSettingsContent, LocaleProvider }
}

beforeEach(() => {
  actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})

afterEach(() => {
  if (root) {
    act(() => root!.unmount())
  }
  container.remove()
  root = null
  vi.doUnmock('../api')
  vi.doUnmock('../storage')
  vi.resetModules()
  vi.clearAllMocks()
  if (originalActEnvironment === undefined) {
    delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
  } else {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
  }
})

describe('SkillsSettingsContent', () => {
  it('本地列表中的 builtin skill 关闭默认启用后显示手动可用', async () => {
    const { api, SkillsSettingsContent, LocaleProvider } = await loadSubject()
    vi.mocked(api.listInstalledSkills)
      .mockResolvedValueOnce([{
        skill_key: 'geogebra-drawing',
        version: '1',
        display_name: 'GeoGebra Drawing',
        description: 'Math diagrams',
        instruction_path: 'SKILL.md',
        manifest_key: 'manifest-1',
        bundle_key: 'bundle-1',
        is_active: true,
        source: 'builtin',
        is_platform: true,
        platform_status: 'auto',
      }])
      .mockResolvedValueOnce([{
        skill_key: 'geogebra-drawing',
        version: '1',
        display_name: 'GeoGebra Drawing',
        description: 'Math diagrams',
        instruction_path: 'SKILL.md',
        manifest_key: 'manifest-1',
        bundle_key: 'bundle-1',
        is_active: true,
        source: 'builtin',
        is_platform: true,
        platform_status: 'manual',
      }])
    vi.mocked(api.listDefaultSkills).mockResolvedValue([])
    vi.mocked(api.searchMarketSkills).mockResolvedValue([])
    vi.mocked(api.replaceDefaultSkills).mockResolvedValue([])
    vi.mocked(api.setPlatformSkillOverride).mockResolvedValue()
    vi.mocked(api.listPlatformSkills).mockResolvedValue([])

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <SkillsSettingsContent accessToken="token" />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(container.textContent).toContain('内置')
    expect(container.textContent).toContain('默认可用')

    const toggle = container.querySelector('input[type="checkbox"]') as HTMLInputElement | null
    expect(toggle).toBeTruthy()
    await act(async () => {
      toggle!.click()
    })
    await flushEffects()

    expect(api.setPlatformSkillOverride).toHaveBeenCalledWith('token', 'geogebra-drawing', '1', 'manual')
    expect(container.textContent).toContain('内置')
    expect(container.textContent).toContain('手动可用')
    expect(container.textContent).not.toContain('默认可用')
  })
})
