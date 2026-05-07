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

function setInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
  setter?.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

async function loadSubject() {
  vi.resetModules()
  vi.doMock('../api', async () => {
    const actual = await vi.importActual<typeof import('../api')>('../api')
    return {
      ...actual,
      deleteSkill: vi.fn(),
      discoverExternalSkills: vi.fn(async () => ({ dirs: [] })),
      getExternalDirs: vi.fn(async () => []),
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
      setExternalDirs: vi.fn(),
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
  it('GitHub 多目录候选支持空默认、多选并批量导入', async () => {
    const { api, SkillsSettingsContent, LocaleProvider } = await loadSubject()
    const ambiguousError = {
      status: 409,
      code: 'skills.import_ambiguous',
      message: 'multiple skill packages found',
      details: {
        candidates: [
          { path: 'skills/a', skill_key: 'a', version: '1', display_name: 'Skill A' },
          { path: 'skills/b', skill_key: 'b', version: '1', display_name: 'Skill B' },
          { path: 'skills/c', skill_key: 'c', version: '1', display_name: 'Skill C' },
        ],
      },
    }
    vi.mocked(api.isApiError).mockImplementation((error) => error === ambiguousError)
    vi.mocked(api.listInstalledSkills).mockResolvedValue([])
    vi.mocked(api.listDefaultSkills).mockResolvedValue([])
    vi.mocked(api.searchMarketSkills).mockResolvedValue([])
    vi.mocked(api.listPlatformSkills).mockResolvedValue([])
    vi.mocked(api.replaceDefaultSkills).mockResolvedValue([])
    vi.mocked(api.installSkill).mockResolvedValue()
    vi.mocked(api.importSkillFromGitHub)
      .mockRejectedValueOnce(ambiguousError)
      .mockResolvedValueOnce({
        skill: {
          skill_key: 'a',
          version: '1',
          display_name: 'Skill A',
          instruction_path: 'SKILL.md',
          manifest_key: 'manifest-a',
          bundle_key: 'bundle-a',
          is_active: true,
        },
      })
      .mockResolvedValueOnce({
        skill: {
          skill_key: 'c',
          version: '1',
          display_name: 'Skill C',
          instruction_path: 'SKILL.md',
          manifest_key: 'manifest-c',
          bundle_key: 'bundle-c',
          is_active: true,
        },
      })

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <SkillsSettingsContent accessToken="token" />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    const addButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.trim() === '添加')
    expect(addButton).toBeTruthy()
    await act(async () => {
      addButton!.click()
    })

    const githubButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.trim() === '从 GitHub 导入')
    expect(githubButton).toBeTruthy()
    await act(async () => {
      githubButton!.click()
    })

    const githubInput = document.body.querySelector('input[placeholder="https://github.com/org/repo/tree/main/skills/demo"]') as HTMLInputElement | null
    expect(githubInput).toBeTruthy()
    await act(async () => {
      setInputValue(githubInput!, 'https://github.com/acme/skills')
    })

    const importButton = Array.from(document.body.querySelectorAll('button')).find((button) => button.textContent?.trim() === '导入 GitHub')
    expect(importButton).toBeTruthy()
    await act(async () => {
      importButton!.click()
    })
    await flushEffects()

    const candidateRows = Array.from(document.body.querySelectorAll('[role="checkbox"]')) as HTMLElement[]
    expect(candidateRows).toHaveLength(3)
    expect(candidateRows.every((row) => row.getAttribute('aria-checked') === 'false')).toBe(true)
    const emptyImportButton = Array.from(document.body.querySelectorAll('button')).find((button) => button.textContent?.trim() === '导入所选 0 项') as HTMLButtonElement | undefined
    expect(emptyImportButton?.disabled).toBe(true)

    const selectAllButton = Array.from(document.body.querySelectorAll('button')).find((button) => button.textContent?.trim() === '全选')
    expect(selectAllButton).toBeTruthy()
    await act(async () => {
      selectAllButton!.click()
    })
    expect(candidateRows.every((row) => row.getAttribute('aria-checked') === 'true')).toBe(true)

    await act(async () => {
      candidateRows[1].click()
    })

    const selectedImportButton = Array.from(document.body.querySelectorAll('button')).find((button) => button.textContent?.trim() === '导入所选 2 项') as HTMLButtonElement | undefined
    expect(selectedImportButton?.disabled).toBe(false)
    await act(async () => {
      selectedImportButton!.click()
    })
    await flushEffects()

    expect(api.importSkillFromGitHub).toHaveBeenNthCalledWith(2, 'token', {
      repository_url: 'https://github.com/acme/skills',
      ref: undefined,
      candidate_path: 'skills/a',
    })
    expect(api.importSkillFromGitHub).toHaveBeenNthCalledWith(3, 'token', {
      repository_url: 'https://github.com/acme/skills',
      ref: undefined,
      candidate_path: 'skills/c',
    })
    expect(api.installSkill).toHaveBeenCalledTimes(2)
    expect(api.replaceDefaultSkills).toHaveBeenCalledWith('token', [
      { skill_key: 'a', version: '1' },
      { skill_key: 'c', version: '1' },
    ])
  })

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

  it('外部目录 skills 为空或缺失时仍渲染设置页', async () => {
    const { api, SkillsSettingsContent, LocaleProvider } = await loadSubject()
    vi.mocked(api.listInstalledSkills).mockResolvedValue([])
    vi.mocked(api.listDefaultSkills).mockResolvedValue([])
    vi.mocked(api.searchMarketSkills).mockResolvedValue([])
    vi.mocked(api.listPlatformSkills).mockResolvedValue([])
    vi.mocked(api.discoverExternalSkills).mockResolvedValue({
      dirs: [
        { path: '/tmp/empty-skills', skills: null },
        { path: '/tmp/missing-skills' },
      ],
    } as unknown as Awaited<ReturnType<typeof api.discoverExternalSkills>>)

    await act(async () => {
      root!.render(
        <LocaleProvider>
          <SkillsSettingsContent accessToken="token" />
        </LocaleProvider>,
      )
    })
    await flushEffects()

    expect(api.discoverExternalSkills).toHaveBeenCalledWith('token')
    expect(container.textContent).toContain('外部技能目录')
    expect(container.textContent).toContain('/tmp/empty-skills')
    expect(container.textContent).toContain('/tmp/missing-skills')
    expect(container.textContent).toContain('未发现技能')
  })
})
