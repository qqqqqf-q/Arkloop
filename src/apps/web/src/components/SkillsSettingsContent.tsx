import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ChevronDown,
  ChevronRight,
  Github,
  Loader2,
  PackagePlus,
  Search,
  Upload,
} from 'lucide-react'
import { Modal, TabBar } from '@arkloop/shared'
import {
  type InstalledSkill,
  type MarketSkill,
  type SkillImportCandidate,
  type SkillPackageResponse,
  type SkillReference,
  deleteSkill,
  importRegistrySkill,
  importSkillFromGitHub,
  importSkillFromUpload,
  installSkill,
  isApiError,
  listDefaultSkills,
  listInstalledSkills,
  listPlatformSkills,
  type PlatformSkillItem,
  replaceDefaultSkills,
  searchMarketSkills,
  setPlatformSkillOverride,
} from '../api'
import { useLocale } from '../contexts/LocaleContext'
import type { CandidateState, ViewMode, ViewSkill } from './skills/types'
import { asSkillRef, dedupeSkillRefs, mergeSkills } from './skills/types'
import { DropdownAction } from './skills/DropdownAction'
import { InstalledSkillsView } from './skills/InstalledSkillsView'
import { MarketplaceView } from './skills/MarketplaceView'
import { BuiltinSkillsView } from './skills/BuiltinSkillsView'
import { SkillDetailModal } from './skills/SkillDetailModal'

type Props = {
  accessToken: string
  onTrySkill?: (prompt: string) => void
}

export function SkillsSettingsContent({ accessToken, onTrySkill }: Props) {
  const { t, locale } = useLocale()
  const skillText = t.skills
  const [query, setQuery] = useState('')
  const [viewMode, setViewMode] = useState<ViewMode>('installed')
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
  const [builtinSkills, setBuiltinSkills] = useState<PlatformSkillItem[]>([])
  const [builtinLoading, setBuiltinLoading] = useState(false)
  const searchInputRef = useRef<HTMLInputElement>(null)
  const addMenuRef = useRef<HTMLDivElement>(null)
  const cardMenuRef = useRef<HTMLDivElement>(null)

  const refreshBuiltin = useCallback(async () => {
    try {
      const items = await listPlatformSkills(accessToken)
      setBuiltinSkills(items)
    } catch { /* silent */ }
  }, [accessToken])

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
    if (viewMode !== 'marketplace') {
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
  }, [accessToken, viewMode, query, skillText.officialSearchFailed, skillText.officialUnconfigured])

  useEffect(() => {
    setError('')
  }, [viewMode])

  useEffect(() => {
    if (viewMode !== 'builtin') return
    setBuiltinLoading(true)
    refreshBuiltin().finally(() => setBuiltinLoading(false))
  }, [viewMode, refreshBuiltin])

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

  const items = useMemo(() => {
    if (viewMode === 'builtin') return []
    return mergeSkills(installedSkills, defaultSkills, marketSkills, query, viewMode)
  }, [viewMode, defaultSkills, installedSkills, marketSkills, query])

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
      if (item.is_platform && item.version) {
        await setPlatformSkillOverride(accessToken, item.skill_key, item.version, 'auto')
        await Promise.all([refreshInstalled(), refreshBuiltin()])
        return
      }
      if (item.installed && item.version) {
        const version = item.version
        if (!shouldConfirmRisk(item)) return
        await syncDefaultSkills((current) => [...current, { skill_key: item.skill_key, version }])
        await refreshInstalled()
        return
      }
      const imported = await importRegistrySkill(accessToken, {
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
  }, [accessToken, ensureInstalledAndDefault, refreshBuiltin, refreshInstalled, shouldConfirmRisk, skillText.importFailed, skillText.repositoryMissing, syncDefaultSkills])

  const handleRemove = useCallback(async (item: ViewSkill) => {
    if (!item.version) return
    setBusySkillId(item.id)
    setError('')
    try {
      if (item.is_platform) {
        await setPlatformSkillOverride(accessToken, item.skill_key, item.version, 'removed')
        await Promise.all([refreshInstalled(), refreshBuiltin()])
        return
      }
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
  }, [accessToken, refreshBuiltin, refreshInstalled, skillText.deleteConflict, skillText.importFailed, syncDefaultSkills])

  const handleDisable = useCallback(async (item: ViewSkill) => {
    if (!item.version) return
    setBusySkillId(item.id)
    setError('')
    try {
      if (item.is_platform) {
        await setPlatformSkillOverride(accessToken, item.skill_key, item.version, 'manual')
        await Promise.all([refreshInstalled(), refreshBuiltin()])
        return
      }
      await syncDefaultSkills((current) => current.filter((skill) => !(skill.skill_key === item.skill_key && skill.version === item.version)))
      await refreshInstalled()
    } catch {
      setError(skillText.disableFailed)
    } finally {
      setBusySkillId(null)
    }
  }, [accessToken, refreshBuiltin, refreshInstalled, skillText.disableFailed, syncDefaultSkills])

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

  const platformAvailabilityLabel = (status?: ViewSkill['platform_status']) => {
    if (status === 'auto') return skillText.enabledByDefault
    if (status === 'manual') return skillText.manualAvailable
    return ''
  }

  const platformAvailabilityStyle = (status?: ViewSkill['platform_status']): React.CSSProperties | null => {
    if (status === 'auto') {
      return { background: 'var(--c-status-ok-bg)', color: 'var(--c-status-ok-text)' }
    }
    if (status === 'manual') {
      return { background: 'var(--c-bg-deep)', color: 'var(--c-text-secondary)' }
    }
    return null
  }

  const scanStatusBadge = useCallback((item: ViewSkill) => {
    const status = item.scan_status ?? (item.scan_has_warnings ? 'suspicious' : 'unknown')
    if (status === 'unknown' && !item.scan_summary && !item.scan_has_warnings) return null
    if (status === 'clean') {
      return {
        label: skillText.scanStatusLabel(status),
        style: { background: 'var(--c-status-ok-bg)', color: 'var(--c-status-ok-text)' },
      }
    }
    if (status === 'suspicious') {
      return {
        label: skillText.scanStatusLabel(status),
        style: { background: 'var(--c-status-danger-bg)', color: 'var(--c-status-warning-text)' },
      }
    }
    if (status === 'malicious') {
      return {
        label: skillText.scanStatusLabel(status),
        style: { background: 'var(--c-status-danger-bg)', color: 'var(--c-status-danger-text)' },
      }
    }
    return {
      label: skillText.scanStatusLabel(status),
      style: { background: 'var(--c-bg-deep)', color: 'var(--c-text-tertiary)' },
    }
  }, [skillText])

  type SkillTab = ViewMode
  const tabItems: { key: SkillTab; label: string }[] = [
    { key: 'installed', label: skillText.installedTab },
    { key: 'marketplace', label: skillText.marketplaceTab },
    { key: 'builtin', label: skillText.builtinTab },
  ]

  const sharedViewProps = {
    busySkillId,
    menuSkillId,
    setMenuSkillId,
    onDetailSkill: setDetailSkill,
    onEnable: (item: ViewSkill) => void handleEnable(item),
    onDisable: (item: ViewSkill) => void handleDisable(item),
    onRemove: (item: ViewSkill) => void handleRemove(item),
    onTrySkill,
    skillText,
    locale,
    platformAvailabilityLabel,
    platformAvailabilityStyle,
    scanStatusBadge,
    active,
    cardMenuRef,
  }

  return (
    <div className="flex flex-col gap-4">
      {/* TabBar + 搜索 + 添加 */}
      <div className="flex flex-wrap items-center gap-2">
        <TabBar<SkillTab>
          tabs={tabItems}
          active={viewMode}
          onChange={(tab) => { setViewMode(tab); setQuery('') }}
        />
        <div className="flex-1" />
        <div className="relative min-w-[220px]">
          <Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--c-text-tertiary)]" />
          <input
            ref={searchInputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={viewMode === 'marketplace' ? skillText.searchPlaceholderMarketplace : skillText.searchPlaceholder}
            className="h-9 w-full rounded-lg pl-9 pr-3 text-sm text-[var(--c-text-heading)] outline-none placeholder:text-[var(--c-text-tertiary)]"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
          />
          {viewMode === 'marketplace' && marketLoading && (
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
              <DropdownAction icon={<Upload size={14} />} label={skillText.addFromUpload} onClick={() => { setShowAddMenu(false); setUploadOpen(true) }} />
              <DropdownAction icon={<Github size={14} />} label={skillText.addFromGitHub} onClick={() => { setShowAddMenu(false); setGitHubOpen(true) }} />
            </div>
          )}
        </div>
      </div>

      {viewMode === 'marketplace' && officialDisabled && (
        <p className="text-xs text-[var(--c-text-tertiary)]">{skillText.officialUnconfigured}</p>
      )}
      {error && (
        <p className="text-xs" style={{ color: 'var(--c-status-error-text)' }}>{error}</p>
      )}

      {viewMode === 'installed' && (
        <InstalledSkillsView
          {...sharedViewProps}
          items={items}
          loading={loading}
          accessToken={accessToken}
        />
      )}

      {viewMode === 'marketplace' && (
        <MarketplaceView
          {...sharedViewProps}
          items={items}
          loading={loading}
          marketLoading={marketLoading}
        />
      )}

      {viewMode === 'builtin' && (
        <BuiltinSkillsView
          builtinSkills={builtinSkills}
          builtinLoading={builtinLoading}
          busySkillId={busySkillId}
          setBusySkillId={setBusySkillId}
          setError={setError}
          query={query}
          accessToken={accessToken}
          skillText={skillText}
          refreshInstalled={refreshInstalled}
          setBuiltinSkills={setBuiltinSkills}
          platformAvailabilityLabel={platformAvailabilityLabel}
          platformAvailabilityStyle={platformAvailabilityStyle}
        />
      )}

      {/* 上传对话框 */}
      <Modal
        open={uploadOpen}
        onClose={() => { if (!uploading) setUploadOpen(false) }}
        title={skillText.uploadTitle}
        width="440px"
      >
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
      </Modal>

      {/* GitHub 导入对话框 */}
      <Modal
        open={githubOpen}
        onClose={() => { if (!importing) setGitHubOpen(false) }}
        title={skillText.githubTitle}
        width="440px"
      >
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
      </Modal>

      {/* 候选目录选择对话框 */}
      <Modal
        open={!!candidateState}
        onClose={() => setCandidateState(null)}
        title={skillText.candidatesTitle}
        width="440px"
      >
        {candidateState && (
          <div className="flex flex-col gap-3">
            <p className="text-xs text-[var(--c-text-tertiary)]">{skillText.chooseCandidate}</p>
            {candidateState.candidates.map((candidate) => (
              <button
                key={candidate.path}
                type="button"
                onClick={() => void handleGitHubImport(candidate.path)}
                className="flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-left transition-colors duration-100 bg-[var(--c-bg-page)] hover:bg-[var(--c-bg-deep)]"
                style={{ border: '0.5px solid var(--c-border-subtle)' }}
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
        )}
      </Modal>

      {detailSkill && (
        <SkillDetailModal
          item={detailSkill}
          onClose={() => setDetailSkill(null)}
          onEnable={(item) => void handleEnable(item)}
          onDisable={(item) => void handleDisable(item)}
          onRemove={(item) => void handleRemove(item)}
          onTrySkill={onTrySkill}
          skillText={skillText}
          locale={locale}
          active={active}
          platformAvailabilityLabel={platformAvailabilityLabel}
          platformAvailabilityStyle={platformAvailabilityStyle}
          scanStatusBadge={scanStatusBadge}
        />
      )}
    </div>
  )
}
