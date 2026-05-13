import { describe, expect, it } from 'vitest'
import { formatSkillRegistryProviderLabel } from '../components/skills/types'

describe('formatSkillRegistryProviderLabel', () => {
  it('shortens arkloop plugin and fixes casing', () => {
    expect(formatSkillRegistryProviderLabel('arkloop plugin', 'official', '官方')).toBe('Arkloop')
    expect(formatSkillRegistryProviderLabel('ARKLOOP PLUGIN', 'custom', '')).toBe('Arkloop')
    expect(formatSkillRegistryProviderLabel('arkloop-plugin', 'custom', '')).toBe('Arkloop')
  })

  it('uses official fallback when provider empty', () => {
    expect(formatSkillRegistryProviderLabel(undefined, 'official', '官方')).toBe('官方')
    expect(formatSkillRegistryProviderLabel('  ', 'github', '官方')).toBe('')
  })

  it('keeps clawhub label', () => {
    expect(formatSkillRegistryProviderLabel('clawhub', 'official', '官方')).toBe('ClawHub')
  })
})
