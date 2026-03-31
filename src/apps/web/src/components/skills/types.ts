import type { InstalledSkill, MarketSkill, SkillPackageResponse, SkillReference } from '../../api'

export type ViewMode = 'installed' | 'marketplace' | 'builtin'

export type ViewSkill = {
  id: string
  skill_key: string
  version?: string
  display_name: string
  description?: string
  detail_url?: string
  repository_url?: string
  registry_provider?: string
  registry_slug?: string
  owner_handle?: string
  source: 'official' | 'custom' | 'github' | 'platform'
  updated_at?: string
  installed: boolean
  enabled_by_default: boolean
  scan_status?: SkillPackageResponse['scan_status']
  scan_has_warnings?: boolean
  scan_summary?: string
  moderation_verdict?: string
  is_platform?: boolean
  platform_status?: 'auto' | 'manual' | 'removed'
}

export type CandidateState = {
  candidates: import('../../api').SkillImportCandidate[]
}

export type SkillsProps = {
  accessToken: string
  onTrySkill?: (prompt: string) => void
}

export function normalizeInstalledSource(item: InstalledSkill): ViewSkill['source'] {
  if (item.is_platform || item.source === 'builtin' || item.source === 'platform') {
    return 'platform'
  }
  if (item.source === 'official' || item.source === 'github') {
    return item.source
  }
  return 'custom'
}

export function dedupeSkillRefs(items: SkillReference[]): SkillReference[] {
  const seen = new Set<string>()
  return items.filter((item) => {
    const key = `${item.skill_key}@${item.version}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

export function formatDate(value?: string, locale = 'zh'): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale === 'zh' ? 'zh-CN' : 'en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  }).format(date)
}

export function asSkillRef(item: InstalledSkill | SkillPackageResponse): SkillReference {
  return { skill_key: item.skill_key, version: item.version }
}

export function buildSkillKey(skillKey?: string, version?: string, registrySlug?: string): string[] {
  const keys: string[] = []
  if (skillKey && version) keys.push(`${skillKey}@${version}`)
  if (registrySlug && version) keys.push(`${registrySlug}@${version}`)
  return keys
}

export function matchesSkillQuery(item: ViewSkill, normalized: string): boolean {
  if (!normalized) return true
  return `${item.display_name} ${item.description ?? ''} ${item.skill_key} ${item.owner_handle ?? ''}`.toLowerCase().includes(normalized)
}

export function mergeSkills(installed: InstalledSkill[], defaults: InstalledSkill[], market: MarketSkill[], query: string, viewMode: ViewMode): ViewSkill[] {
  const defaultKeys = new Set(defaults.flatMap((item) => buildSkillKey(item.skill_key, item.version, item.registry_slug)))
  const installedByKey = new Map(installed.map((item) => [item.skill_key, item]))
  const installedByRegistrySlug = new Map(
    installed
      .filter((item) => item.registry_slug)
      .map((item) => [item.registry_slug as string, item]),
  )
  const normalized = query.trim().toLowerCase()

  const installedViews = installed.map<ViewSkill>((item) => ({
    id: `installed:${item.skill_key}@${item.version}`,
    skill_key: item.skill_key,
    version: item.version,
    display_name: item.display_name,
    description: item.description ?? undefined,
    detail_url: item.registry_detail_url,
    repository_url: item.registry_source_url,
    registry_provider: item.registry_provider,
    registry_slug: item.registry_slug,
    owner_handle: item.registry_owner_handle,
    source: normalizeInstalledSource(item),
    updated_at: item.updated_at,
    installed: true,
    enabled_by_default: item.is_platform
      ? item.platform_status === 'auto'
      : buildSkillKey(item.skill_key, item.version, item.registry_slug).some((key) => defaultKeys.has(key)),
    scan_status: item.scan_status,
    scan_has_warnings: item.scan_has_warnings,
    scan_summary: item.scan_summary,
    moderation_verdict: item.moderation_verdict,
    is_platform: item.is_platform,
    platform_status: item.platform_status ?? (item.is_platform ? 'auto' : undefined),
  }))

  const marketViews = market.map<ViewSkill>((item) => {
    const installedItem = (item.registry_slug ? installedByRegistrySlug.get(item.registry_slug) : null) ?? installedByKey.get(item.skill_key)
    return {
      id: `market:${item.registry_slug ?? item.skill_key}`,
      skill_key: item.skill_key,
      version: installedItem?.version ?? item.version,
      display_name: installedItem?.display_name ?? item.display_name,
      description: installedItem?.description ?? item.description ?? undefined,
      detail_url: installedItem?.registry_detail_url ?? item.detail_url ?? undefined,
      repository_url: installedItem?.registry_source_url ?? item.repository_url ?? undefined,
      registry_provider: installedItem?.registry_provider ?? item.registry_provider,
      registry_slug: installedItem?.registry_slug ?? item.registry_slug,
      owner_handle: installedItem?.registry_owner_handle ?? item.owner_handle,
      source: installedItem ? normalizeInstalledSource(installedItem) : 'official',
      updated_at: installedItem?.updated_at ?? item.updated_at ?? undefined,
      installed: installedItem != null || item.installed,
      enabled_by_default: installedItem != null
        ? buildSkillKey(installedItem.skill_key, installedItem.version, installedItem.registry_slug).some((key) => defaultKeys.has(key))
        : item.enabled_by_default,
      scan_status: installedItem?.scan_status ?? item.scan_status,
      scan_has_warnings: installedItem?.scan_has_warnings ?? item.scan_has_warnings,
      scan_summary: installedItem?.scan_summary ?? item.scan_summary,
      moderation_verdict: installedItem?.moderation_verdict ?? item.moderation_verdict,
    }
  })

  const sourceItems = viewMode === 'installed' ? installedViews : marketViews
  return sourceItems.filter((item) => matchesSkillQuery(item, normalized))
}
