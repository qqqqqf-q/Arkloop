import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Github,
  Loader2,
  PackagePlus,
  Search,
  Upload,
  X,
} from 'lucide-react'
import { ConfirmDialog, Modal } from '@arkloop/shared'
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
import { SettingsSegmentedControl } from './settings/_SettingsSegmentedControl'
import { SettingsInput } from './settings/_SettingsInput'
import { SettingsButton } from './settings/_SettingsButton'
import { SettingsSwitch } from './settings/_SettingsSwitch'
import { SettingsCheckboxList } from './settings/_SettingsCheckboxList'

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
  const [riskConfirm, setRiskConfirm] = useState<{
    message: string
    resolve: (value: boolean) => void
  } | null>(null)
  const [file, setFile] = useState<File | null>(null)
  const [installAfterImport, setInstallAfterImport] = useState(true)
  const [uploading, setUploading] = useState(false)
  const [githubUrl, setGitHubUrl] = useState('')
  const [githubRef, setGitHubRef] = useState('')
  const [importing, setImporting] = useState(false)
  const [selectedCandidatePaths, setSelectedCandidatePaths] = useState<string[]>([])
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

  const confirmRisk = useCallback((skill: { display_name: string; scan_status?: SkillPackageResponse['scan_status']; scan_summary?: string; scan_has_warnings?: boolean }) => {
    const status = skill.scan_status ?? (skill.scan_has_warnings ? 'suspicious' : 'unknown')
    if (status !== 'suspicious' && status !== 'malicious') return Promise.resolve(true)
    return new Promise<boolean>((resolve) => {
      setRiskConfirm({
        message: skillText.riskConfirm(skill.display_name, skillText.scanStatusLabel(status), skill.scan_summary),
        resolve,
      })
    })
  }, [skillText])

  const ensureSkillsInstalledAndDefault = useCallback(async (skills: SkillPackageResponse[]) => {
    if (skills.length === 0) return
    const installedKeys = new Set(installedSkills.map((item) => `${item.skill_key}@${item.version}`))
    for (const skill of skills) {
      const key = `${skill.skill_key}@${skill.version}`
      if (!installedKeys.has(key)) {
        await installSkill(accessToken, asSkillRef(skill))
        installedKeys.add(key)
      }
    }
    await syncDefaultSkills((current) => [...current, ...skills.map(asSkillRef)])
    await refreshInstalled()
  }, [accessToken, installedSkills, refreshInstalled, syncDefaultSkills])

  const ensureInstalledAndDefault = useCallback(async (skill: SkillPackageResponse) => {
    await ensureSkillsInstalledAndDefault([skill])
  }, [ensureSkillsInstalledAndDefault])

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
        if (!(await confirmRisk(item))) return
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
      if (!(await confirmRisk(imported))) return
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
  }, [accessToken, confirmRisk, ensureInstalledAndDefault, refreshBuiltin, refreshInstalled, skillText.importFailed, skillText.repositoryMissing, syncDefaultSkills])

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

  const requestGitHubImport = useCallback(async (candidatePath?: string) => {
    return await importSkillFromGitHub(accessToken, {
      repository_url: githubUrl.trim(),
      ref: githubRef.trim() || undefined,
      candidate_path: candidatePath,
    })
  }, [accessToken, githubRef, githubUrl])

  const handleGitHubImport = useCallback(async () => {
    setImporting(true)
    setError('')
    try {
      const response = await requestGitHubImport()
      setCandidateState(null)
      setSelectedCandidatePaths([])
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
        setSelectedCandidatePaths([])
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
  }, [ensureInstalledAndDefault, requestGitHubImport, skillText.githubInvalidUrl, skillText.githubSkillNotFound, skillText.importFailed])

  const closeCandidatePicker = useCallback(() => {
    setCandidateState(null)
    setSelectedCandidatePaths([])
  }, [])

  const handleSelectedCandidateImport = useCallback(async () => {
    if (!candidateState || selectedCandidatePaths.length === 0) return
    setImporting(true)
    setError('')
    try {
      const selected = candidateState.candidates.filter((candidate) => selectedCandidatePaths.includes(candidate.path))
      const imported: SkillPackageResponse[] = []
      for (const candidate of selected) {
        const response = await requestGitHubImport(candidate.path)
        imported.push(response.skill)
      }
      closeCandidatePicker()
      setGitHubOpen(false)
      await ensureSkillsInstalledAndDefault(imported)
      setGitHubUrl('')
      setGitHubRef('')
    } catch (err) {
      const apiErr = isApiError(err) ? err : null
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
  }, [candidateState, closeCandidatePicker, ensureSkillsInstalledAndDefault, requestGitHubImport, selectedCandidatePaths, skillText.githubInvalidUrl, skillText.githubSkillNotFound, skillText.importFailed])

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

  const candidatePaths = candidateState?.candidates.map((candidate) => candidate.path) ?? []
  const allCandidatesSelected = candidatePaths.length > 0 && candidatePaths.every((path) => selectedCandidatePaths.includes(path))

  return (
    <div className="flex flex-col gap-4">
      {/* 分段切换 + 搜索 + 添加 */}
      <div className="flex flex-wrap items-center gap-2">
        <SettingsSegmentedControl<SkillTab>
          options={tabItems.map((item) => ({ value: item.key, label: item.label }))}
          value={viewMode}
          onChange={(tab) => { setViewMode(tab); setQuery('') }}
        />
        <div className="flex-1" />
        <div className="relative min-w-[220px]">
          <Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--c-text-tertiary)]" />
          <SettingsInput
            ref={searchInputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={viewMode === 'marketplace' ? skillText.searchPlaceholderMarketplace : skillText.searchPlaceholder}
            className="h-9 pl-9 pr-3"
          />
          {viewMode === 'marketplace' && marketLoading && (
            <Loader2 size={14} className="absolute right-3 top-1/2 -translate-y-1/2 animate-spin text-[var(--c-text-tertiary)]" />
          )}
        </div>

        <div className="relative" ref={addMenuRef}>
          <SettingsButton
            variant="primary"
            onClick={() => setShowAddMenu((v) => !v)}
            icon={<PackagePlus size={14} />}
          >
            {skillText.add}
          </SettingsButton>
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

      <ConfirmDialog
        open={riskConfirm != null}
        title={skillText.scanStatusLabel('suspicious')}
        message={riskConfirm?.message ?? ''}
        confirmLabel={locale === 'zh' ? '继续' : 'Continue'}
        cancelLabel={skillText.cancelAction}
        onClose={() => {
          riskConfirm?.resolve(false)
          setRiskConfirm(null)
        }}
        onConfirm={() => {
          riskConfirm?.resolve(true)
          setRiskConfirm(null)
        }}
      />

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
          <label className="flex items-center justify-between gap-3 text-sm text-[var(--c-text-secondary)]">
            <span>{skillText.uploadImmediateInstall}</span>
            <SettingsSwitch
              checked={installAfterImport}
              onChange={setInstallAfterImport}
              size="sm"
            />
          </label>
          <div className="flex items-center justify-end gap-2">
            <SettingsButton
              size="modal"
              onClick={() => setUploadOpen(false)}
            >
              {skillText.cancelAction}
            </SettingsButton>
            <SettingsButton
              variant="primary"
              size="modal"
              onClick={() => void handleUploadImport()}
              disabled={!file || uploading}
              icon={uploading ? <Loader2 size={14} className="animate-spin" /> : undefined}
            >
              {uploading ? skillText.uploading : skillText.uploadAction}
            </SettingsButton>
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
            <SettingsInput
              variant="md"
              value={githubUrl}
              onChange={(e) => setGitHubUrl(e.target.value)}
              placeholder={skillText.githubUrlPlaceholder}
            />
          </div>
          <div className="flex flex-col gap-2">
            <span className="text-sm font-medium text-[var(--c-text-heading)]">{skillText.githubRefLabel}</span>
            <SettingsInput
              variant="md"
              value={githubRef}
              onChange={(e) => setGitHubRef(e.target.value)}
              placeholder="main"
            />
          </div>
          <div className="flex items-center justify-end gap-2">
            <SettingsButton
              size="modal"
              onClick={() => setGitHubOpen(false)}
            >
              {skillText.cancelAction}
            </SettingsButton>
            <SettingsButton
              variant="primary"
              size="modal"
              onClick={() => void handleGitHubImport()}
              disabled={!githubUrl.trim() || importing}
              icon={importing ? <Loader2 size={14} className="animate-spin" /> : undefined}
            >
              {importing ? skillText.importing : skillText.githubAction}
            </SettingsButton>
          </div>
        </div>
      </Modal>

      {/* 候选目录选择对话框 */}
      <Modal
        open={!!candidateState}
        onClose={() => { if (!importing) closeCandidatePicker() }}
        width="560px"
      >
        {candidateState && (
          <div className="flex flex-col gap-4">
            <div className="flex items-start justify-between gap-4">
              <div>
                <h3 className="text-[19px] font-semibold leading-none text-[var(--c-text-heading)]">{skillText.candidatesTitle}</h3>
                <p className="mt-3 text-xs text-[var(--c-text-tertiary)]">{skillText.chooseCandidate}</p>
              </div>
              <button
                type="button"
                aria-label="Close"
                onClick={closeCandidatePicker}
                disabled={importing}
                className="-mr-2 -mt-1 flex h-8 w-8 shrink-0 items-center justify-center rounded-[7px] text-[color-mix(in_srgb,var(--c-border)_72%,var(--c-text-primary)_28%)] transition-colors duration-[160ms] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-primary)] disabled:pointer-events-none disabled:opacity-40"
              >
                <X size={16} />
              </button>
            </div>
            <div className="flex items-center justify-end">
              <SettingsButton
                size="modal"
                onClick={() => setSelectedCandidatePaths(allCandidatesSelected ? [] : candidatePaths)}
                disabled={importing}
              >
                {allCandidatesSelected ? skillText.clearAllCandidates : skillText.selectAllCandidates}
              </SettingsButton>
            </div>
            <SettingsCheckboxList
              options={candidateState.candidates.map((candidate) => ({
                value: candidate.path,
                title: candidate.display_name ?? candidate.skill_key ?? candidate.path,
                description: candidate.path,
                meta: candidate.skill_key && candidate.version ? `${candidate.skill_key}@${candidate.version}` : undefined,
              }))}
              selectedValues={selectedCandidatePaths}
              onChange={setSelectedCandidatePaths}
            />
            <div className="flex items-center justify-end gap-2">
              <SettingsButton size="modal" onClick={closeCandidatePicker} disabled={importing}>
                {skillText.cancelAction}
              </SettingsButton>
              <SettingsButton
                variant="primary"
                size="modal"
                onClick={() => void handleSelectedCandidateImport()}
                disabled={selectedCandidatePaths.length === 0 || importing}
                icon={importing ? <Loader2 size={14} className="animate-spin" /> : undefined}
              >
                {importing ? skillText.importing : skillText.importSelectedCandidates(selectedCandidatePaths.length)}
              </SettingsButton>
            </div>
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
