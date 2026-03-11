import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ChevronDown,
  ChevronRight,
  Download,
  ExternalLink,
  Github,
  Loader2,
  MessageSquare,
  MoreHorizontal,
  PackagePlus,
  RefreshCw,
  Search,
  ShieldCheck,
  Sparkles,
  Trash2,
  Upload,
  X,
} from 'lucide-react'
import {
  type InstalledSkill,
  type MarketSkill,
  type SkillImportCandidate,
  type SkillPackageResponse,
  type SkillReference,
  deleteSkill,
  importMarketSkill,
  importSkillFromGitHub,
  importSkillFromUpload,
  installSkill,
  isApiError,
  listDefaultSkills,
  listInstalledSkills,
  replaceDefaultSkills,
  searchMarketSkills,
} from '../api'
import { useLocale } from '../contexts/LocaleContext'

type Props = {
  accessToken: string
  onTrySkill?: (prompt: string) => void
}

type BrowseMode = 'registry' | 'local'

type ViewSkill = {
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
  source: 'official' | 'custom' | 'github'
  updated_at?: string
  installed: boolean
  enabled_by_default: boolean
  scan_status?: SkillPackageResponse['scan_status']
  scan_has_warnings?: boolean
  scan_summary?: string
  moderation_verdict?: string
}

type CandidateState = {
  candidates: SkillImportCandidate[]
}

function dedupeSkillRefs(items: SkillReference[]): SkillReference[] {
  const seen = new Set<string>()
  return items.filter((item) => {
    const key = `${item.skill_key}@${item.version}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function formatDate(value?: string, locale = 'zh'): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale === 'zh' ? 'zh-CN' : 'en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  }).format(date)
}

function asSkillRef(item: InstalledSkill | SkillPackageResponse): SkillReference {
  return { skill_key: item.skill_key, version: item.version }
}

function buildSkillKey(skillKey?: string, version?: string, registrySlug?: string): string[] {
  const keys: string[] = []
  if (skillKey && version) keys.push(`${skillKey}@${version}`)
  if (registrySlug && version) keys.push(`${registrySlug}@${version}`)
  return keys
}

function matchesSkillQuery(item: ViewSkill, normalized: string): boolean {
  if (!normalized) return true
  return `${item.display_name} ${item.description ?? ''} ${item.skill_key} ${item.owner_handle ?? ''}`.toLowerCase().includes(normalized)
}

function mergeSkills(installed: InstalledSkill[], defaults: InstalledSkill[], market: MarketSkill[], query: string, browseMode: BrowseMode): ViewSkill[] {
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
    source: item.source ?? 'custom',
    updated_at: item.updated_at,
    installed: true,
    enabled_by_default: buildSkillKey(item.skill_key, item.version, item.registry_slug).some((key) => defaultKeys.has(key)),
    scan_status: item.scan_status,
    scan_has_warnings: item.scan_has_warnings,
    scan_summary: item.scan_summary,
    moderation_verdict: item.moderation_verdict,
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
      source: installedItem?.source ?? 'official',
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

  const sourceItems = browseMode === 'local' ? installedViews : marketViews
  return sourceItems.filter((item) => matchesSkillQuery(item, normalized))
}

export function SkillsSettingsContent({ accessToken, onTrySkill }: Props) {
  const { t, locale } = useLocale()
  const skillText = t.skills
  const [query, setQuery] = useState('')
  const [browseMode, setBrowseMode] = useState<BrowseMode>('local')
  const [showAddMenu, setShowAddMenu] = useState(false)
  const [menuSkillId, setMenuSkillId] = useState<string | null>(null)
  const [installedSkills, setInstalledSkills] = useState<InstalledSkill[]>([])
  const [defaultSkills, setDefaultSkills] = useState<InstalledSkill[]>([])
  const [marketSkills, setMarketSkills] = useState<MarketSkill[]>([])
  const [loading, setLoading] = useState(true)
  const [marketLoading, setMarketLoading] = useState(false)
  const [officialDisabled, setOfficialDisabled] = useState(false)
  const [error, setError] = useState('')
  const [busySkillId, setBusySkillId] = useState<string | null>(null)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [githubOpen, setGitHubOpen] = useState(false)
  const [candidateState, setCandidateState] = useState<CandidateState | null>(null)
  const [file, setFile] = useState<File | null>(null)
  const [installAfterImport, setInstallAfterImport] = useState(true)
  const [uploading, setUploading] = useState(false)
  const [githubUrl, setGitHubUrl] = useState('')
  const [githubRef, setGitHubRef] = useState('')
  const [importing, setImporting] = useState(false)
  const [detailSkill, setDetailSkill] = useState<ViewSkill | null>(null)
  const searchInputRef = useRef<HTMLInputElement>(null)
  const addMenuRef = useRef<HTMLDivElement>(null)
  const cardMenuRef = useRef<HTMLDivElement>(null)

  const refreshInstalled = useCallback(async () => {
    const [installed, defaults] = await Promise.all([
      listInstalledSkills(accessToken),
      listDefaultSkills(accessToken),
    ])
    setInstalledSkills(installed)
    setDefaultSkills(defaults)
    return { installed, defaults }
  }, [accessToken])

  useEffect(() => {
    void (async () => {
      setLoading(true)
      setError('')
      try {
        await refreshInstalled()
      } catch {
        setError(skillText.loadFailed)
      } finally {
        setLoading(false)
      }
    })()
  }, [refreshInstalled, skillText.loadFailed])

  useEffect(() => {
    if (browseMode !== 'registry') {
      setMarketLoading(false)
      setOfficialDisabled(false)
      return
    }
    const timer = window.setTimeout(async () => {
      setMarketLoading(true)
      try {
        const items = await searchMarketSkills(accessToken, query, true)
        setMarketSkills(items)
        setOfficialDisabled(false)
        if (query.trim()) {
          setError('')
        }
      } catch (err) {
        const apiErr = isApiError(err) ? err : null
        if (apiErr?.status === 503 || apiErr?.code === 'skills.market.not_configured') {
          setOfficialDisabled(true)
          setMarketSkills([])
          setError(skillText.officialUnconfigured)
        } else {
          setMarketSkills([])
          setError(apiErr?.message || skillText.officialSearchFailed)
        }
      } finally {
        setMarketLoading(false)
      }
    }, query.trim() ? 160 : 0)
    return () => window.clearTimeout(timer)
  }, [accessToken, browseMode, query, skillText.officialSearchFailed, skillText.officialUnconfigured])

  useEffect(() => {
    setError('')
  }, [browseMode])

  useEffect(() => {
    const handler = (event: MouseEvent) => {
      const target = event.target as HTMLElement
      if (addMenuRef.current && !addMenuRef.current.contains(target)) {
        setShowAddMenu(false)
      }
      if (cardMenuRef.current && !cardMenuRef.current.contains(target)) {
        setMenuSkillId(null)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const items = useMemo(
    () => mergeSkills(installedSkills, defaultSkills, marketSkills, query, browseMode),
    [browseMode, defaultSkills, installedSkills, marketSkills, query],
  )

  const syncDefaultSkills = useCallback(async (updater: (current: SkillReference[]) => SkillReference[]) => {
    const current = defaultSkills.map(asSkillRef)
    const next = dedupeSkillRefs(updater(current))
    const updated = await replaceDefaultSkills(accessToken, next)
    setDefaultSkills(updated)
    return updated
  }, [accessToken, defaultSkills])

  const shouldConfirmRisk = useCallback((skill: { display_name: string; scan_status?: SkillPackageResponse['scan_status']; scan_summary?: string; scan_has_warnings?: boolean }) => {
    const status = skill.scan_status ?? (skill.scan_has_warnings ? 'suspicious' : 'unknown')
    if (status !== 'suspicious' && status !== 'malicious') return true
    return window.confirm(skillText.riskConfirm(skill.display_name, skillText.scanStatusLabel(status), skill.scan_summary))
  }, [skillText])

  const ensureInstalledAndDefault = useCallback(async (skill: SkillPackageResponse) => {
    const exists = installedSkills.some((item) => item.skill_key === skill.skill_key && item.version === skill.version)
    if (!exists) {
      await installSkill(accessToken, asSkillRef(skill))
    }
    await syncDefaultSkills((current) => [...current, asSkillRef(skill)])
    await refreshInstalled()
  }, [accessToken, installedSkills, refreshInstalled, syncDefaultSkills])

  const handleEnable = useCallback(async (item: ViewSkill) => {
    setBusySkillId(item.id)
    setError('')
    try {
      if (item.installed && item.version) {
        const version = item.version
        if (!shouldConfirmRisk(item)) return
        await syncDefaultSkills((current) => [...current, { skill_key: item.skill_key, version }])
        await refreshInstalled()
        return
      }
      const imported = await importMarketSkill(accessToken, {
        slug: item.registry_slug ?? item.skill_key,
        version: item.version,
        skill_key: item.skill_key,
        detail_url: item.detail_url,
        repository_url: item.repository_url,
      })
      if (!shouldConfirmRisk(imported)) return
      await ensureInstalledAndDefault(imported)
    } catch (err) {
      const apiErr = isApiError(err) ? err : null
      if (apiErr?.code === 'skills.market.repository_missing') {
        setError(skillText.repositoryMissing)
      } else {
        setError(apiErr?.message || skillText.importFailed)
      }
    } finally {
      setBusySkillId(null)
    }
  }, [accessToken, ensureInstalledAndDefault, refreshInstalled, shouldConfirmRisk, skillText.importFailed, skillText.repositoryMissing, syncDefaultSkills])

  const handleRemove = useCallback(async (item: ViewSkill) => {
    if (!item.version) return
    setBusySkillId(item.id)
    setError('')
    try {
      await syncDefaultSkills((current) => current.filter((skill) => !(skill.skill_key === item.skill_key && skill.version === item.version)))
      await deleteSkill(accessToken, { skill_key: item.skill_key, version: item.version })
      await refreshInstalled()
    } catch (err) {
      const apiErr = isApiError(err) ? err : null
      if (apiErr?.code === 'skills.in_use') {
        setError(skillText.deleteConflict)
      } else {
        setError(skillText.importFailed)
      }
    } finally {
      setBusySkillId(null)
    }
  }, [accessToken, refreshInstalled, skillText.deleteConflict, skillText.importFailed, syncDefaultSkills])

  const handleDisable = useCallback(async (item: ViewSkill) => {
    if (!item.version) return
    setBusySkillId(item.id)
    setError('')
    try {
      await syncDefaultSkills((current) => current.filter((skill) => !(skill.skill_key === item.skill_key && skill.version === item.version)))
      await refreshInstalled()
    } catch {
      setError(skillText.disableFailed)
    } finally {
      setBusySkillId(null)
    }
  }, [refreshInstalled, skillText.disableFailed, syncDefaultSkills])

  const handleGitHubImport = useCallback(async (candidatePath?: string) => {
    setImporting(true)
    setError('')
    try {
      const response = await importSkillFromGitHub(accessToken, {
        repository_url: githubUrl.trim(),
        ref: githubRef.trim() || undefined,
        candidate_path: candidatePath,
      })
      setCandidateState(null)
      setGitHubOpen(false)
      await ensureInstalledAndDefault(response.skill)
      setGitHubUrl('')
      setGitHubRef('')
    } catch (err) {
      const apiErr = isApiError(err) ? err : null
      const details = apiErr?.details
      if (apiErr?.code === 'skills.import_ambiguous' && details && typeof details === 'object' && Array.isArray((details as { candidates?: unknown[] }).candidates)) {
        setCandidateState({
          candidates: (details as { candidates: SkillImportCandidate[] }).candidates,
        })
        return
      }
      if (apiErr?.code === 'skills.import_invalid_repository') {
        setError(skillText.githubInvalidUrl)
      } else if (apiErr?.code === 'skills.import_not_found') {
        setError(skillText.githubSkillNotFound)
      } else {
        setError(apiErr?.message || skillText.importFailed)
      }
    } finally {
      setImporting(false)
    }
  }, [accessToken, ensureInstalledAndDefault, githubRef, githubUrl, skillText.githubInvalidUrl, skillText.githubSkillNotFound, skillText.importFailed])

  const handleUploadImport = useCallback(async () => {
    if (!file) return
    setUploading(true)
    setError('')
    try {
      const response = await importSkillFromUpload(accessToken, {
        file,
        install_after_import: false,
      })
      if (installAfterImport) {
        await ensureInstalledAndDefault(response)
      } else {
        await refreshInstalled()
      }
      setFile(null)
      setUploadOpen(false)
    } catch {
      setError(skillText.importFailed)
    } finally {
      setUploading(false)
    }
  }, [accessToken, ensureInstalledAndDefault, file, installAfterImport, refreshInstalled, skillText.importFailed])

  const active = (item: ViewSkill) => item.installed && item.enabled_by_default

  const scanStatusBadge = useCallback((item: ViewSkill) => {
    const status = item.scan_status ?? (item.scan_has_warnings ? 'suspicious' : 'unknown')
    if (status === 'unknown' && !item.scan_summary && !item.scan_has_warnings) return null
    if (status === 'clean') {
      return {
        label: skillText.scanStatusLabel(status),
        style: { background: 'var(--c-status-ok-bg,#f0fdf4)', color: 'var(--c-status-ok-text,#15803d)' },
      }
    }
    if (status === 'suspicious') {
      return {
        label: skillText.scanStatusLabel(status),
        style: { background: 'rgba(245, 158, 11, 0.12)', color: '#b45309' },
      }
    }
    if (status === 'malicious') {
      return {
        label: skillText.scanStatusLabel(status),
        style: { background: 'rgba(239, 68, 68, 0.12)', color: '#b91c1c' },
      }
    }
    return {
      label: skillText.scanStatusLabel(status),
      style: { background: 'var(--c-bg-deep)', color: 'var(--c-text-tertiary)' },
    }
  }, [skillText])

  return (
    <div className="flex flex-col gap-4">
      {/* 切换 + 搜索 + 添加 */}
      <div className="flex flex-wrap items-center gap-2">
        <div
          className="relative shrink-0"
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
            padding: '2px',
            borderRadius: '10px',
            background: 'var(--c-bg-deep)',
            minWidth: '168px',
          }}
        >
          <div
            aria-hidden
            className="pointer-events-none absolute left-[2px] top-[2px] h-8 rounded-lg"
            style={{
              width: 'calc((100% - 4px) / 2)',
              background: 'var(--c-bg-page)',
              border: '0.5px solid var(--c-border-subtle)',
              transition: 'transform 180ms cubic-bezier(0.16, 1, 0.3, 1)',
              transform: browseMode === 'registry' ? 'translateX(0)' : 'translateX(100%)',
            }}
          />
          <button
            type="button"
            onClick={() => setBrowseMode('registry')}
            className="relative z-[1] flex h-8 items-center justify-center rounded-lg px-3 text-sm transition-colors"
            style={{ color: browseMode === 'registry' ? 'var(--c-text-heading)' : 'var(--c-text-tertiary)' }}
          >
            {skillText.registryTab}
          </button>
          <button
            type="button"
            onClick={() => setBrowseMode('local')}
            className="relative z-[1] flex h-8 items-center justify-center rounded-lg px-3 text-sm transition-colors"
            style={{ color: browseMode === 'local' ? 'var(--c-text-heading)' : 'var(--c-text-tertiary)' }}
          >
            {skillText.localTab}
          </button>
        </div>
        <div className="relative min-w-[220px] flex-1">
          <Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--c-text-tertiary)]" />
          <input
            ref={searchInputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={skillText.searchPlaceholder}
            className="h-9 w-full rounded-lg pl-9 pr-3 text-sm text-[var(--c-text-heading)] outline-none placeholder:text-[var(--c-text-tertiary)]"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
          />
          {browseMode === 'registry' && marketLoading && (
            <Loader2 size={14} className="absolute right-3 top-1/2 -translate-y-1/2 animate-spin text-[var(--c-text-tertiary)]" />
          )}
        </div>
        <div className="relative" ref={addMenuRef}>
          <button
            type="button"
            onClick={() => setShowAddMenu((v) => !v)}
            className="flex h-9 items-center gap-1.5 rounded-lg px-3 text-sm font-medium"
            style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
          >
            <PackagePlus size={14} />
            {skillText.add}
            <ChevronDown size={13} />
          </button>
          {showAddMenu && (
            <div
              className="dropdown-menu absolute right-0 top-[calc(100%+4px)] z-50"
              style={{
                border: '0.5px solid var(--c-border-subtle)',
                borderRadius: '10px',
                padding: '4px',
                background: 'var(--c-bg-menu)',
                minWidth: '200px',
                boxShadow: 'var(--c-dropdown-shadow)',
              }}
            >
              <DropdownAction icon={<Sparkles size={14} />} label={skillText.createWithArkloop} disabled onClick={() => {}} />
              <DropdownAction icon={<Upload size={14} />} label={skillText.addFromUpload} onClick={() => { setShowAddMenu(false); setUploadOpen(true) }} />
              <DropdownAction icon={<ShieldCheck size={14} />} label={skillText.addFromSkillsmp} onClick={() => { setShowAddMenu(false); setBrowseMode('registry'); searchInputRef.current?.focus() }} />
              <DropdownAction icon={<Github size={14} />} label={skillText.addFromGitHub} onClick={() => { setShowAddMenu(false); setGitHubOpen(true) }} />
            </div>
          )}
        </div>
      </div>

      {browseMode === 'registry' && officialDisabled && (
        <p className="text-xs text-[var(--c-text-tertiary)]">{skillText.officialUnconfigured}</p>
      )}
      {error && (
        <p className="text-xs" style={{ color: 'var(--c-status-error-text, #ef4444)' }}>{error}</p>
      )}

      {/* 技能列表 */}
      <div className="flex flex-col gap-2">
        <span className="text-xs font-medium text-[var(--c-text-tertiary)]">
          {skillText.searchResults(items.length)}
        </span>

        {loading ? (
          <div className="flex h-40 items-center justify-center">
            <Loader2 size={16} className="animate-spin text-[var(--c-text-tertiary)]" />
          </div>
        ) : items.length === 0 ? (
          <div
            className="flex flex-col items-center justify-center gap-1 rounded-xl py-12 text-center"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <span className="text-sm font-medium text-[var(--c-text-heading)]">{skillText.emptyTitle}</span>
            <span className="text-xs text-[var(--c-text-tertiary)]">{browseMode === 'local' ? skillText.emptyBodyNoMarket : skillText.emptyDesc}</span>
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {items.map((item) => {
              const busy = busySkillId === item.id
              const enabled = active(item)
              const scanBadge = scanStatusBadge(item)
              const providerLabel = item.registry_provider?.trim().toLowerCase() === 'clawhub'
                ? 'ClawHub'
                : item.registry_provider?.trim() || (item.source === 'official' ? skillText.sourceOfficial : '')
              const metaParts = [providerLabel, item.owner_handle ? `@${item.owner_handle}` : '', item.version ? `v${item.version}` : '']
                .filter(Boolean)
                .join(' · ')
              return (
                <div
                  key={item.id}
                  className="flex items-start gap-3 rounded-xl p-3 cursor-pointer transition-colors duration-100"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
                  onClick={() => setDetailSkill(item)}
                  onMouseEnter={(e) => { e.currentTarget.style.background = 'var(--c-bg-deep)' }}
                  onMouseLeave={(e) => { e.currentTarget.style.background = 'var(--c-bg-menu)' }}
                >
                  <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="truncate text-sm font-medium text-[var(--c-text-heading)]">
                        {item.display_name}
                      </span>
                      {item.source === 'official' && (
                        <span
                          className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight"
                          style={{ background: 'var(--c-pro-bg)', color: '#6ba3f6' }}
                        >
                          {providerLabel}
                        </span>
                      )}
                      {item.source === 'github' && (
                        <span
                          className="flex shrink-0 items-center gap-0.5 rounded px-1.5 py-px text-[10px] font-medium leading-tight text-[var(--c-text-tertiary)]"
                          style={{ background: 'var(--c-bg-deep)' }}
                        >
                          <Github size={9} />
                          {skillText.sourceGitHub}
                        </span>
                      )}
                      {scanBadge && (
                        <span
                          className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight"
                          style={scanBadge.style}
                        >
                          {scanBadge.label}
                        </span>
                      )}
                      {enabled && (
                        <span
                          className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight"
                          style={{ background: 'var(--c-status-ok-bg,#f0fdf4)', color: 'var(--c-status-ok-text,#15803d)' }}
                        >
                          {skillText.enabledByDefault}
                        </span>
                      )}
                    </div>
                    <span className="line-clamp-2 text-xs text-[var(--c-text-tertiary)]">
                      {item.description ?? item.skill_key}
                    </span>
                    {metaParts && (
                      <span className="text-[10px] text-[var(--c-text-muted)]">{metaParts}</span>
                    )}
                    {(item.scan_summary || item.updated_at) && (
                      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[10px] text-[var(--c-text-muted)]">
                        {item.scan_summary && <span className="line-clamp-2">{item.scan_summary}</span>}
                        {item.updated_at && <span>{skillText.updatedAt(formatDate(item.updated_at, locale))}</span>}
                      </div>
                    )}
                  </div>

                  <label className="relative mt-0.5 inline-flex shrink-0 cursor-pointer items-center" onClick={(e) => e.stopPropagation()}>
                    <input
                      type="checkbox"
                      checked={enabled}
                      disabled={busy}
                      onChange={() => {
                        if (enabled) void handleDisable(item)
                        else void handleEnable(item)
                      }}
                      className="peer sr-only"
                    />
                    <span
                      className="h-5 w-9 rounded-full transition-colors"
                      style={{ background: enabled ? 'var(--c-btn-bg)' : 'var(--c-border-mid)' }}
                    />
                    <span
                      className="absolute left-0.5 top-0.5 h-4 w-4 rounded-full transition-transform peer-checked:translate-x-4"
                      style={{ background: enabled ? 'var(--c-btn-text)' : 'var(--c-bg-page)' }}
                    />
                  </label>

                  <div className="relative" ref={menuSkillId === item.id ? cardMenuRef : undefined} onClick={(e) => e.stopPropagation()}>
                    <button
                      type="button"
                      onClick={() => setMenuSkillId((v) => (v === item.id ? null : item.id))}
                      className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
                    >
                      {busy ? <Loader2 size={14} className="animate-spin" /> : <MoreHorizontal size={14} />}
                    </button>
                    {menuSkillId === item.id && (
                      <div
                        className="dropdown-menu absolute right-0 top-[calc(100%+4px)] z-50"
                        style={{
                          border: '0.5px solid var(--c-border-subtle)',
                          borderRadius: '10px',
                          padding: '4px',
                          background: 'var(--c-bg-menu)',
                          width: '180px',
                          boxShadow: 'var(--c-dropdown-shadow)',
                        }}
                      >
                        <DropdownAction
                          icon={<MessageSquare size={14} />}
                          label={skillText.trySkill}
                          disabled={!item.installed || !item.enabled_by_default}
                          onClick={() => {
                            setMenuSkillId(null)
                            onTrySkill?.(skillText.trySkillPrompt(item.skill_key))
                          }}
                        />
                        <DropdownAction
                          icon={<Download size={14} />}
                          label={skillText.download}
                          disabled={!item.detail_url}
                          onClick={() => {
                            setMenuSkillId(null)
                            if (item.detail_url) window.open(item.detail_url, '_blank', 'noopener,noreferrer')
                          }}
                        />
                        <DropdownAction
                          icon={<RefreshCw size={14} />}
                          label={skillText.replace}
                          disabled={item.source === 'custom' || (!item.detail_url && !item.repository_url)}
                          onClick={() => { setMenuSkillId(null); void handleEnable(item) }}
                        />
                        <DropdownAction
                          icon={<Trash2 size={14} />}
                          label={skillText.remove}
                          disabled={!item.installed || !item.version}
                          destructive
                          onClick={() => { setMenuSkillId(null); void handleRemove(item) }}
                        />
                      </div>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* 上传对话框 */}
      {uploadOpen && (
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center"
          style={{ background: 'rgba(0,0,0,0.12)', backdropFilter: 'blur(2px)', WebkitBackdropFilter: 'blur(2px)' }}
          onMouseDown={(e) => { if (e.target === e.currentTarget && !uploading) setUploadOpen(false) }}
        >
          <div
            className="modal-enter w-full max-w-md rounded-2xl p-6"
            style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)' }}
          >
            <div className="mb-4 flex items-center justify-between">
              <h3 className="text-base font-semibold text-[var(--c-text-heading)]">{skillText.uploadTitle}</h3>
              <button
                type="button"
                onClick={() => { if (!uploading) setUploadOpen(false) }}
                className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
              >
                <X size={16} />
              </button>
            </div>
            <div className="flex flex-col gap-4">
              <div className="flex flex-col gap-2">
                <span className="text-sm font-medium text-[var(--c-text-heading)]">{skillText.uploadFileLabel}</span>
                <label
                  className="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2.5 text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
                  style={{ border: '0.5px dashed var(--c-border-mid)', background: 'var(--c-bg-page)' }}
                >
                  <Upload size={14} className="shrink-0 text-[var(--c-text-tertiary)]" />
                  <span className={file ? 'text-[var(--c-text-heading)]' : 'text-[var(--c-text-tertiary)]'}>
                    {file?.name ?? skillText.uploadFileHint}
                  </span>
                  <input
                    type="file"
                    accept=".zip,.skill,application/zip,application/octet-stream"
                    className="hidden"
                    onChange={(e) => setFile(e.target.files?.[0] ?? null)}
                  />
                </label>
              </div>
              <label className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
                <input
                  type="checkbox"
                  checked={installAfterImport}
                  onChange={(e) => setInstallAfterImport(e.target.checked)}
                  className="h-3.5 w-3.5 rounded"
                  style={{ accentColor: 'var(--c-text-heading)' }}
                />
                {skillText.uploadImmediateInstall}
              </label>
              <div className="flex items-center justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setUploadOpen(false)}
                  className="rounded-lg px-4 py-2 text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                >
                  {skillText.cancelAction}
                </button>
                <button
                  type="button"
                  onClick={() => void handleUploadImport()}
                  disabled={!file || uploading}
                  className="flex items-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium transition-colors disabled:opacity-50"
                  style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
                >
                  {uploading && <Loader2 size={14} className="animate-spin" />}
                  {uploading ? skillText.uploading : skillText.uploadAction}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* GitHub 导入对话框 */}
      {githubOpen && (
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center"
          style={{ background: 'rgba(0,0,0,0.12)', backdropFilter: 'blur(2px)', WebkitBackdropFilter: 'blur(2px)' }}
          onMouseDown={(e) => { if (e.target === e.currentTarget && !importing) setGitHubOpen(false) }}
        >
          <div
            className="modal-enter w-full max-w-md rounded-2xl p-6"
            style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)' }}
          >
            <div className="mb-4 flex items-center justify-between">
              <h3 className="text-base font-semibold text-[var(--c-text-heading)]">{skillText.githubTitle}</h3>
              <button
                type="button"
                onClick={() => { if (!importing) setGitHubOpen(false) }}
                className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
              >
                <X size={16} />
              </button>
            </div>
            <div className="flex flex-col gap-4">
              <div className="flex flex-col gap-2">
                <span className="text-sm font-medium text-[var(--c-text-heading)]">{skillText.githubUrlLabel}</span>
                <input
                  value={githubUrl}
                  onChange={(e) => setGitHubUrl(e.target.value)}
                  placeholder={skillText.githubUrlPlaceholder}
                  className="h-9 w-full rounded-lg px-3 text-sm text-[var(--c-text-heading)] outline-none placeholder:text-[var(--c-text-tertiary)]"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                />
              </div>
              <div className="flex flex-col gap-2">
                <span className="text-sm font-medium text-[var(--c-text-heading)]">{skillText.githubRefLabel}</span>
                <input
                  value={githubRef}
                  onChange={(e) => setGitHubRef(e.target.value)}
                  placeholder="main"
                  className="h-9 w-full rounded-lg px-3 text-sm text-[var(--c-text-heading)] outline-none placeholder:text-[var(--c-text-tertiary)]"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                />
              </div>
              <div className="flex items-center justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setGitHubOpen(false)}
                  className="rounded-lg px-4 py-2 text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                >
                  {skillText.cancelAction}
                </button>
                <button
                  type="button"
                  onClick={() => void handleGitHubImport()}
                  disabled={!githubUrl.trim() || importing}
                  className="flex items-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium transition-colors disabled:opacity-50"
                  style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
                >
                  {importing && <Loader2 size={14} className="animate-spin" />}
                  {importing ? skillText.importing : skillText.githubAction}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* 候选目录选择对话框 */}
      {candidateState && (
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center"
          style={{ background: 'rgba(0,0,0,0.12)', backdropFilter: 'blur(2px)', WebkitBackdropFilter: 'blur(2px)' }}
          onMouseDown={(e) => { if (e.target === e.currentTarget) setCandidateState(null) }}
        >
          <div
            className="modal-enter w-full max-w-md rounded-2xl p-6"
            style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)' }}
          >
            <div className="mb-4 flex items-center justify-between">
              <h3 className="text-base font-semibold text-[var(--c-text-heading)]">{skillText.candidatesTitle}</h3>
              <button
                type="button"
                onClick={() => setCandidateState(null)}
                className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
              >
                <X size={16} />
              </button>
            </div>
            <div className="flex flex-col gap-3">
              <p className="text-xs text-[var(--c-text-tertiary)]">{skillText.chooseCandidate}</p>
              {candidateState.candidates.map((candidate) => (
                <button
                  key={candidate.path}
                  type="button"
                  onClick={() => void handleGitHubImport(candidate.path)}
                  className="flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-left transition-colors duration-100"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                  onMouseEnter={(e) => { e.currentTarget.style.background = 'var(--c-bg-deep)' }}
                  onMouseLeave={(e) => { e.currentTarget.style.background = 'var(--c-bg-page)' }}
                >
                  <div>
                    <span className="block text-sm font-medium text-[var(--c-text-heading)]">
                      {candidate.display_name ?? candidate.skill_key ?? candidate.path}
                    </span>
                    <span className="block text-xs text-[var(--c-text-tertiary)]">{candidate.path}</span>
                  </div>
                  <ChevronRight size={13} className="shrink-0 text-[var(--c-text-tertiary)]" />
                </button>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* 技能详情 Modal */}
      {detailSkill && (() => {
        const item = detailSkill
        const enabled = active(item)
        const scanBadge = scanStatusBadge(item)
        const providerLabel = item.registry_provider?.trim().toLowerCase() === 'clawhub'
          ? 'ClawHub'
          : item.registry_provider?.trim() || (item.source === 'official' ? skillText.sourceOfficial : item.source === 'github' ? skillText.sourceGitHub : skillText.sourceCustom)
        return (
          <div
            className="fixed inset-0 z-[60] flex items-center justify-center"
            style={{ background: 'rgba(0,0,0,0.12)', backdropFilter: 'blur(2px)', WebkitBackdropFilter: 'blur(2px)' }}
            onMouseDown={(e) => { if (e.target === e.currentTarget) setDetailSkill(null) }}
          >
            <div
              className="modal-enter flex w-full max-w-lg flex-col overflow-hidden rounded-2xl"
              style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)', maxHeight: '80vh' }}
            >
              {/* header */}
              <div className="flex items-center justify-between gap-3 border-b px-5 py-4" style={{ borderColor: 'var(--c-border-subtle)' }}>
                <div className="flex min-w-0 flex-col gap-0.5">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-base font-semibold text-[var(--c-text-heading)]">{item.display_name}</span>
                    {item.source === 'official' && (
                      <span className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight" style={{ background: 'var(--c-pro-bg)', color: '#6ba3f6' }}>
                        {providerLabel}
                      </span>
                    )}
                    {item.source === 'github' && (
                      <span className="flex shrink-0 items-center gap-0.5 rounded px-1.5 py-px text-[10px] font-medium leading-tight text-[var(--c-text-tertiary)]" style={{ background: 'var(--c-bg-deep)' }}>
                        <Github size={9} />
                        {skillText.sourceGitHub}
                      </span>
                    )}
                    {scanBadge && (
                      <span className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight" style={scanBadge.style}>
                        {scanBadge.label}
                      </span>
                    )}
                  </div>
                  <span className="text-xs text-[var(--c-text-tertiary)]">{item.skill_key}{item.version ? ` v${item.version}` : ''}</span>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <button
                    type="button"
                    onClick={() => {
                      setDetailSkill(null)
                      onTrySkill?.(skillText.trySkillPrompt(item.skill_key))
                    }}
                    disabled={!item.installed || !enabled}
                    className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors disabled:opacity-40"
                    style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)', color: 'var(--c-text-heading)' }}
                  >
                    <MessageSquare size={13} />
                    {skillText.trySkill}
                  </button>
                  <button
                    type="button"
                    onClick={() => setDetailSkill(null)}
                    className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
                  >
                    <X size={16} />
                  </button>
                </div>
              </div>

              {/* body */}
              <div className="flex-1 overflow-auto p-5">
                <div className="flex flex-col gap-4">
                  <div className="flex flex-col gap-1.5">
                    <span className="text-xs font-medium text-[var(--c-text-tertiary)]">{skillText.detailDescription}</span>
                    <p className="text-sm leading-relaxed text-[var(--c-text-secondary)]">
                      {item.description || skillText.noDescription}
                    </p>
                  </div>

                  <div className="grid grid-cols-2 gap-3">
                    <div className="flex flex-col gap-1 rounded-lg p-3" style={{ background: 'var(--c-bg-deep)' }}>
                      <span className="text-[10px] font-medium text-[var(--c-text-muted)]">{skillText.detailVersion}</span>
                      <span className="text-sm text-[var(--c-text-heading)]">{item.version || '-'}</span>
                    </div>
                    <div className="flex flex-col gap-1 rounded-lg p-3" style={{ background: 'var(--c-bg-deep)' }}>
                      <span className="text-[10px] font-medium text-[var(--c-text-muted)]">{skillText.detailSource}</span>
                      <span className="text-sm text-[var(--c-text-heading)]">{providerLabel || item.source}</span>
                    </div>
                  </div>

                  {item.updated_at && (
                    <div className="flex flex-col gap-1 rounded-lg p-3" style={{ background: 'var(--c-bg-deep)' }}>
                      <span className="text-[10px] font-medium text-[var(--c-text-muted)]">{skillText.detailUpdatedAt}</span>
                      <span className="text-sm text-[var(--c-text-heading)]">{formatDate(item.updated_at, locale)}</span>
                    </div>
                  )}

                  {item.scan_summary && (
                    <div className="rounded-lg p-3" style={{ background: 'var(--c-bg-deep)' }}>
                      <p className="text-xs leading-relaxed text-[var(--c-text-tertiary)]">{item.scan_summary}</p>
                    </div>
                  )}
                </div>
              </div>

              {/* footer */}
              <div className="flex items-center justify-between border-t px-5 py-3" style={{ borderColor: 'var(--c-border-subtle)' }}>
                <div className="flex items-center gap-2">
                  {item.detail_url && (
                    <button
                      type="button"
                      onClick={() => window.open(item.detail_url!, '_blank', 'noopener,noreferrer')}
                      className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
                      style={{ border: '0.5px solid var(--c-border-subtle)' }}
                    >
                      <Download size={12} />
                      {skillText.download}
                    </button>
                  )}
                  {item.installed && item.version && (
                    <button
                      type="button"
                      onClick={() => { setDetailSkill(null); void handleRemove(item) }}
                      className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors hover:bg-[var(--c-error-bg)]"
                      style={{ color: 'var(--c-status-error-text, #ef4444)' }}
                    >
                      <Trash2 size={12} />
                      {skillText.remove}
                    </button>
                  )}
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <span className="text-xs text-[var(--c-text-tertiary)]">{enabled ? skillText.enabledByDefault : skillText.disable}</span>
                  <label className="relative inline-flex shrink-0 cursor-pointer items-center">
                    <input
                      type="checkbox"
                      checked={enabled}
                      onChange={() => {
                        if (enabled) void handleDisable(item)
                        else void handleEnable(item)
                      }}
                      className="peer sr-only"
                    />
                    <span className="h-5 w-9 rounded-full transition-colors" style={{ background: enabled ? 'var(--c-btn-bg)' : 'var(--c-border-mid)' }} />
                    <span className="absolute left-0.5 top-0.5 h-4 w-4 rounded-full transition-transform peer-checked:translate-x-4" style={{ background: enabled ? 'var(--c-btn-text)' : 'var(--c-bg-page)' }} />
                  </label>
                </div>
              </div>
            </div>
          </div>
        )
      })()}
    </div>
  )
}

function DropdownAction({ icon, label, onClick, disabled, destructive }: { icon: ReactNode; label: string; onClick: () => void; disabled?: boolean; destructive?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className="flex w-full items-center gap-2 px-3 py-2 text-sm transition-colors duration-100 disabled:cursor-not-allowed disabled:opacity-40"
      style={{
        borderRadius: '8px',
        color: destructive ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-secondary)',
        background: 'var(--c-bg-menu)',
      }}
      onMouseEnter={(e) => { if (!e.currentTarget.disabled) e.currentTarget.style.background = destructive ? 'var(--c-error-bg)' : 'var(--c-bg-deep)' }}
      onMouseLeave={(e) => { e.currentTarget.style.background = 'var(--c-bg-menu)' }}
    >
      {icon}
      {label}
    </button>
  )
}
