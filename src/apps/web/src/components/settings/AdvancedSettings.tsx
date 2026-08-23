import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Database,
  Download,
  FolderOpen,
  Import,
  Loader2,
  Mic,
  Network,
  RefreshCw,
  ScrollText,
  type LucideIcon,
} from 'lucide-react'
import { getDesktopApi } from '@arkloop/shared/desktop'
import { Modal, useToast } from '@arkloop/shared'
import type {
  AgentImportDiscovery,
  DesktopExportSection,
  DesktopLogEntry,
  DesktopLogLevel,
  DesktopLogQuery,
  ImportItemKey,
  ImportSourceKind,
} from '@arkloop/shared/desktop'
import { useAppearance } from '../../contexts/AppearanceContext'
import { useLocale } from '../../contexts/LocaleContext'
import type { ThemeBackgroundImage, ThemeDefinition, ThemePreset } from '../../themes/types'
import { readGtdEnabled, writeGtdEnabled } from '../../storage'
import { SettingsSection } from './_SettingsSection'
import { SettingsSectionHeader } from './_SettingsSectionHeader'
import { settingsInputCls } from './_SettingsInput'
import { SettingsLabel } from './_SettingsLabel'
import { SettingsSelect } from './_SettingsSelect'
import { ConnectionSettings } from './ConnectionSettings'
import { VoiceSettings } from './VoiceSettings'
import { SettingsSwitch } from './_SettingsSwitch'

export type AdvancedSettingsKey = 'voice' | 'network' | 'data' | 'logs'

type Props = {
  accessToken: string
  initialKey?: AdvancedSettingsKey | null
}

type AgentImportSelectionState = Partial<Record<ImportSourceKind, Record<ImportItemKey, boolean>>>

const DESKTOP_EXPORT_SECTIONS: DesktopExportSection[] = [
  'settings',
  'providers',
  'history',
  'personas',
  'projects',
  'mcp',
  'themes',
]

const AGENT_IMPORT_ITEM_KEYS = ['identity', 'skills', 'mcp', 'providers'] as const
const AGENT_IMPORT_SOURCE_ORDER: ImportSourceKind[] = ['openclaw', 'hermes']
const THEME_PRESETS: readonly ThemePreset[] = ['default', 'terra', 'github', 'nord', 'catppuccin', 'tokyo-night', 'retina-burn', 'background-image', 'custom']

function createDefaultAgentImportSelection(): Record<ImportItemKey, boolean> {
  return {
    identity: true,
    skills: true,
    mcp: true,
    providers: true,
  }
}

function createAgentImportSelections(sources: AgentImportDiscovery[]): AgentImportSelectionState {
  return sources.reduce<AgentImportSelectionState>((acc, source) => {
    acc[source.kind] = createDefaultAgentImportSelection()
    return acc
  }, {})
}

function isThemePreset(value: unknown): value is ThemePreset {
  return typeof value === 'string' && THEME_PRESETS.includes(value as ThemePreset)
}

type ThemeBackgroundImageImport =
  | { valid: true; value: ThemeBackgroundImage | null }
  | { valid: false }

function normalizeThemeBackgroundImage(value: unknown): ThemeBackgroundImageImport {
  if (value === null) return { valid: true, value: null }
  if (!value || typeof value !== 'object') return { valid: false }
  const image = value as Record<string, unknown>
  if (
    typeof image.dataUrl !== 'string' ||
    typeof image.name !== 'string' ||
    typeof image.mimeType !== 'string' ||
    typeof image.size !== 'number' ||
    typeof image.updatedAt !== 'number'
  ) {
    return { valid: false }
  }
  return {
    valid: true,
    value: {
      dataUrl: image.dataUrl,
      name: image.name,
      mimeType: image.mimeType,
      size: image.size,
      updatedAt: image.updatedAt,
    },
  }
}

function applySidebarGrouping(value: unknown): void {
  if (value !== 'normal' && value !== 'gtd') return
  writeGtdEnabled(value === 'gtd')
}

function collectImportedCustomThemes(value: unknown): ThemeDefinition[] {
  if (!value || typeof value !== 'object') return []
  return Object.values(value).filter((item): item is ThemeDefinition => (
    !!item && typeof item === 'object' && 'id' in item && typeof item.id === 'string'
  ))
}

function actionBtnCls(disabled?: boolean) {
  return [
    'inline-flex h-[32px] items-center gap-1.5 rounded-[6.5px] bg-[var(--c-bg-input)] px-3.5 text-sm font-[450] text-[color-mix(in_srgb,var(--c-text-secondary)_72%,var(--c-text-primary)_28%)] [background-clip:padding-box] transition-colors duration-[180ms]',
    disabled ? 'cursor-not-allowed opacity-40' : 'hover:border-transparent hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]',
  ].join(' ')
}

function primaryBtnCls(disabled?: boolean) {
  return [
    'inline-flex h-[32px] items-center gap-1.5 rounded-[6.5px] px-3.5 text-sm font-[450] text-[var(--c-btn-text)]',
    'transition-[box-shadow] duration-150 hover:[box-shadow:inset_0_0_0_999px_rgba(255,255,255,0.07),0_0_0_0.2px_var(--c-btn-bg)] active:[box-shadow:inset_0_0_0_999px_rgba(0,0,0,0.04)]',
    disabled ? 'cursor-not-allowed opacity-40' : '',
  ].join(' ')
}

function AgentImportSourceIcon({ source }: { source: ImportSourceKind }) {
  if (source === 'hermes') {
    return (
      <svg viewBox="0 0 64 64" aria-hidden="true" className="block h-4 w-4">
        <path d="M30 10h4v44h-4z" fill="currentColor" opacity="0.9" />
        <path d="M30 18c-7-5-15-5-20-1 6-1 13 0 19 5zM34 18c7-5 15-5 20-1-6-1-13 0-19 5z" fill="currentColor" opacity="0.72" />
        <path d="M30 24c-5-3-11-3-16 0 5-1 10 0 15 4zM34 24c5-3 11-3 16 0-5-1-10 0-15 4z" fill="currentColor" opacity="0.48" />
        <path d="M32 49c-10-4-13-12-5-17-8 3-10 11-3 16m8 1c10-4 13-12 5-17 8 3 10 11 3 16" fill="none" stroke="currentColor" strokeWidth="2.8" strokeLinecap="round" />
        <circle cx="32" cy="10" r="4" fill="currentColor" />
      </svg>
    )
  }

  return (
    <svg viewBox="0 0 120 120" aria-hidden="true" className="block h-4 w-4">
      <path d="M60 12c-25 0-43 18-43 43 0 19 12 35 28 42v10h10v-7c3 1 7 1 10 0v7h10V97c16-7 28-23 28-42 0-25-18-43-43-43Z" fill="currentColor" opacity="0.9" />
      <path d="M23 47C9 43 1 51 7 62c6 10 17 6 22-7 2-5-1-8-6-8Zm74 0c14-4 22 4 16 15-6 10-17 6-22-7-2-5 1-8 6-8Z" fill="currentColor" opacity="0.72" />
      <circle cx="45" cy="36" r="5" fill="var(--c-bg-page)" />
      <circle cx="75" cy="36" r="5" fill="var(--c-bg-page)" />
    </svg>
  )
}

function AgentImportButtonContent({ source, label }: { source: ImportSourceKind; label: string }) {
  return (
    <>
      <AgentImportSourceIcon source={source} />
      <span className="min-w-0 truncate">{label}</span>
    </>
  )
}





// -- Shared small components --

function logLevelColor(level: DesktopLogLevel): string {
  switch (level) {
    case 'error': return '#f87171'
    case 'warn': return '#fbbf24'
    case 'debug': return '#a78bfa'
    case 'info': return '#60a5fa'
    default: return '#9ca3af'
  }
}

function logLevelTag(level: DesktopLogLevel): string {
  return level.toUpperCase().padEnd(5, ' ')
}

// -- Sub-panes --

function NetworkPane({ onReloadOverview }: { onReloadOverview: () => Promise<void> }) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const api = getDesktopApi()
  const [config, setConfig] = useState({
    proxyEnabled: false,
    proxyUrl: '',
    requestTimeoutMs: 30000,
    retryCount: 1,
    userAgent: '',
  })
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!api) return
    void api.config.get().then((next) => {
      setConfig({
        proxyEnabled: next.network.proxyEnabled,
        proxyUrl: next.network.proxyUrl ?? '',
        requestTimeoutMs: next.network.requestTimeoutMs ?? 30000,
        retryCount: next.network.retryCount ?? 1,
        userAgent: next.network.userAgent ?? '',
      })
    }).catch(() => {})
  }, [api])

  const handleSave = useCallback(async () => {
    if (!api) return
    setSaving(true)
    setError('')
    setSaved(false)
    try {
      const current = await api.config.get()
      await api.config.set({
        ...current,
        network: {
          proxyEnabled: config.proxyEnabled,
          proxyUrl: config.proxyUrl.trim() || undefined,
          requestTimeoutMs: config.requestTimeoutMs,
          retryCount: config.retryCount,
          userAgent: config.userAgent.trim() || undefined,
        },
      })
      setSaved(true)
      void onReloadOverview()
    } catch (err) {
      setError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setSaving(false)
      window.setTimeout(() => setSaved(false), 2000)
    }
  }, [api, config, onReloadOverview, t.requestFailed])

  const INPUT = settingsInputCls('sm')

  return (
    <div className="flex flex-col gap-6">
      <SettingsSectionHeader title={ds.advancedNetwork} description={ds.advancedNetworkDesc} />

      <SettingsSection>
        <div className="flex flex-col gap-4">
          <div className="grid gap-4 md:grid-cols-2">
            <div>
              <SettingsLabel>{ds.advancedNetworkProxyEnable}</SettingsLabel>
              <SettingsSelect
                value={config.proxyEnabled ? 'true' : 'false'}
                options={[
                  { value: 'false', label: ds.advancedDisabled },
                  { value: 'true', label: ds.advancedEnabled },
                ]}
                onChange={(v) => setConfig((p) => ({ ...p, proxyEnabled: v === 'true' }))}
              />
            </div>
            <div>
              <SettingsLabel>{ds.advancedNetworkProxyUrl}</SettingsLabel>
              <input
                value={config.proxyUrl}
                onChange={(e) => setConfig((p) => ({ ...p, proxyUrl: e.target.value }))}
                placeholder="http://127.0.0.1:7890"
                className={INPUT}
              />
            </div>
            <div>
              <SettingsLabel>{ds.advancedNetworkTimeout}</SettingsLabel>
              <input
                type="number"
                min={1000}
                max={300000}
                value={config.requestTimeoutMs}
                onChange={(e) => setConfig((p) => ({ ...p, requestTimeoutMs: Number(e.target.value) || 30000 }))}
                className={INPUT}
              />
            </div>
            <div>
              <SettingsLabel>{ds.advancedNetworkRetry}</SettingsLabel>
              <input
                type="number"
                min={0}
                max={10}
                value={config.retryCount}
                onChange={(e) => setConfig((p) => ({ ...p, retryCount: Number(e.target.value) || 0 }))}
                className={INPUT}
              />
            </div>
          </div>
          <div>
            <SettingsLabel>{ds.advancedNetworkUserAgent}</SettingsLabel>
            <input
              value={config.userAgent}
              onChange={(e) => setConfig((p) => ({ ...p, userAgent: e.target.value }))}
              placeholder="Arkloop Desktop"
              className={INPUT}
            />
          </div>
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => void handleSave()}
              disabled={saving}
              className={primaryBtnCls(saving)}
              style={{ background: 'var(--c-btn-bg)' }}
            >
              {saving && <Loader2 size={14} className="animate-spin" />}
              <span>{saving ? ds.advancedSaving : ds.advancedSave}</span>
            </button>
            {saved && <span className="text-sm" style={{ color: 'var(--c-status-success)' }}>{ds.advancedSaved}</span>}
            {error && <span className="text-sm" style={{ color: 'var(--c-status-error)' }}>{error}</span>}
          </div>
        </div>
      </SettingsSection>

      <ConnectionSettings />
    </div>
  )
}


function DataPane({ onReloadOverview }: { onReloadOverview: () => Promise<void> }) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const ob = t.onboarding
  const api = getDesktopApi()
  const { addToast } = useToast()
  const {
    themePreset,
    setThemePreset,
    customThemeId,
    customThemes,
    saveCustomTheme,
    setActiveCustomTheme,
    backgroundImage,
    setBackgroundImage,
    backgroundImageOpacity,
    setBackgroundImageOpacity,
  } = useAppearance()
  const [actionLoading, setActionLoading] = useState<'choose' | 'export' | 'import' | null>(null)
  const [actionError, setActionError] = useState('')
  const [exportDialogOpen, setExportDialogOpen] = useState(false)
  const [selectedSections, setSelectedSections] = useState<DesktopExportSection[]>(DESKTOP_EXPORT_SECTIONS)
  const [agentImportSources, setAgentImportSources] = useState<AgentImportDiscovery[]>([])
  const [selectedAgentImport, setSelectedAgentImport] = useState<ImportSourceKind | null>(null)
  const [agentImportSelections, setAgentImportSelections] = useState<AgentImportSelectionState>({})
  const [agentImporting, setAgentImporting] = useState(false)
  const [agentImportError, setAgentImportError] = useState('')

  const exportOptions = [
    { key: 'settings' as const, label: ds.advancedExportSettings },
    { key: 'providers' as const, label: ds.advancedExportProviders },
    { key: 'history' as const, label: ds.advancedExportHistory },
    { key: 'personas' as const, label: ds.advancedExportPersonas },
    { key: 'projects' as const, label: ds.advancedExportProjects },
    { key: 'mcp' as const, label: ds.advancedExportMcp },
    { key: 'themes' as const, label: ds.advancedExportThemes },
  ]

  const toggleSection = useCallback((section: DesktopExportSection) => {
    setSelectedSections((prev) => (
      prev.includes(section)
        ? prev.filter((item) => item !== section)
        : [...prev, section]
    ))
  }, [])

  useEffect(() => {
    const detectImports = api?.onboarding?.detectImports
    if (!detectImports) return
    let cancelled = false

    void detectImports().then((sources) => {
      if (cancelled) return
      setAgentImportSources(sources)
      setAgentImportSelections(createAgentImportSelections(sources))
    }).catch(() => {
      if (cancelled) return
      setAgentImportSources([])
      setAgentImportSelections({})
    })

    return () => {
      cancelled = true
    }
  }, [api])

  const handleChoose = useCallback(async () => {
    if (!api?.advanced) return
    setActionLoading('choose')
    setActionError('')
    try {
      const selected = await api.advanced.chooseDataFolder()
      if (selected) addToast(ds.advancedSelectedFolder, 'success')
    } catch (err) {
      setActionError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setActionLoading(null)
    }
  }, [api, addToast, ds.advancedSelectedFolder, t.requestFailed])

  const handleExport = useCallback(async () => {
    if (!api?.advanced) return
    if (selectedSections.length === 0) return
    setActionLoading('export')
    setActionError('')
    try {
      const result = await api.advanced.exportDataBundle({
        sections: selectedSections,
        themes: {
          themePreset,
          customThemeId,
          customThemes: selectedSections.includes('themes') ? customThemes : {},
          backgroundImage: selectedSections.includes('themes') ? backgroundImage : null,
          backgroundImageOpacity: selectedSections.includes('themes') ? backgroundImageOpacity : null,
          sidebarGrouping: selectedSections.includes('themes') ? (readGtdEnabled() ? 'gtd' : 'normal') : null,
        },
      })
      if (result.canceled) {
        addToast(ds.advancedExportCanceled, 'neutral')
        setExportDialogOpen(false)
        return
      }
      addToast(ds.advancedExportDone, 'success')
      setExportDialogOpen(false)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setActionLoading(null)
    }
  }, [api, addToast, backgroundImage, backgroundImageOpacity, customThemeId, customThemes, ds.advancedExportCanceled, ds.advancedExportDone, selectedSections, t.requestFailed, themePreset])

  const handleImport = useCallback(async () => {
    if (!api?.advanced) return
    setActionLoading('import')
    setActionError('')
    try {
      const result = await api.advanced.importDataBundle()
      if (result.canceled) {
        addToast(ds.advancedImportCanceled, 'neutral')
        return
      }
      const importedThemes = result.themes
      const importedCustomThemes = collectImportedCustomThemes(importedThemes?.customThemes)
      let hasImportedBackground = false
      let importedBackground: ThemeBackgroundImage | null = null
      if (importedThemes && 'backgroundImage' in importedThemes) {
        const backgroundResult = normalizeThemeBackgroundImage(importedThemes.backgroundImage)
        if (!backgroundResult.valid) {
          throw new Error(t.requestFailed)
        }
        importedBackground = backgroundResult.value
        hasImportedBackground = true
      }
      const importedBackgroundOpacity = typeof importedThemes?.backgroundImageOpacity === 'number' && Number.isFinite(importedThemes.backgroundImageOpacity)
        ? importedThemes.backgroundImageOpacity
        : null
      const importedSidebarGrouping = importedThemes?.sidebarGrouping
      const importedThemePreset = importedThemes?.themePreset
      const importedCustomThemeId = importedThemes?.customThemeId

      if (hasImportedBackground && !setBackgroundImage(importedBackground)) {
        throw new Error(t.requestFailed)
      }
      for (const theme of importedCustomThemes) {
        saveCustomTheme(theme)
      }
      if (importedBackgroundOpacity !== null) {
        setBackgroundImageOpacity(importedBackgroundOpacity)
      }
      applySidebarGrouping(importedSidebarGrouping)
      if (isThemePreset(importedThemePreset)) {
        if (importedThemePreset === 'custom' && importedCustomThemeId) {
          setActiveCustomTheme(importedCustomThemeId)
        } else if (importedThemePreset === 'background-image') {
          setThemePreset('background-image')
        } else {
          setThemePreset(importedThemePreset)
        }
      } else if (importedCustomThemeId) {
        setActiveCustomTheme(importedCustomThemeId)
      }
      addToast(ds.advancedImportDone, 'success')
      await onReloadOverview()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setActionLoading(null)
    }
  }, [api, addToast, ds.advancedImportCanceled, ds.advancedImportDone, onReloadOverview, saveCustomTheme, setActiveCustomTheme, setBackgroundImage, setBackgroundImageOpacity, setThemePreset, t.requestFailed])

  const selectedAgentImportSource = selectedAgentImport
    ? agentImportSources.find((source) => source.kind === selectedAgentImport) ?? null
    : null

  const visibleAgentImportSources = AGENT_IMPORT_SOURCE_ORDER
    .map((kind) => agentImportSources.find((source) => source.kind === kind))
    .filter((source): source is AgentImportDiscovery => Boolean(source))

  const handleAgentImportItemToggle = useCallback((source: ImportSourceKind, item: ImportItemKey) => {
    setAgentImportSelections((current) => {
      const currentSource = current[source] ?? createDefaultAgentImportSelection()
      return {
        ...current,
        [source]: {
          ...currentSource,
          [item]: !currentSource[item],
        },
      }
    })
  }, [])

  const handleAgentImportBack = useCallback(() => {
    setAgentImportError('')
    setSelectedAgentImport(null)
  }, [])

  const handleAgentImportSelected = useCallback(async () => {
    if (!selectedAgentImportSource || !api?.onboarding.applyImport || agentImporting) return
    const selection = agentImportSelections[selectedAgentImportSource.kind] ?? createDefaultAgentImportSelection()
    setAgentImportError('')
    setAgentImporting(true)
    try {
      const result = await api.onboarding.applyImport({
        source: selectedAgentImportSource.kind,
        selection,
      })
      if (!result.ok) {
        throw new Error(result.errors[0] ?? t.requestFailed)
      }
      addToast(ds.advancedImportDone, 'success')
      setSelectedAgentImport(null)
      await onReloadOverview()
    } catch (err) {
      setAgentImportError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setAgentImporting(false)
    }
  }, [addToast, agentImportSelections, agentImporting, api, ds.advancedImportDone, onReloadOverview, selectedAgentImportSource, t.requestFailed])

  const busy = actionLoading !== null

  if (selectedAgentImportSource) {
    const selection = agentImportSelections[selectedAgentImportSource.kind] ?? createDefaultAgentImportSelection()
    const importRows = AGENT_IMPORT_ITEM_KEYS.map((itemKey) => {
      const title = {
        identity: ob.importAgentIdentity,
        skills: ob.importSkills,
        mcp: ob.importMcpServers,
        providers: ob.importLlmProviders,
      }[itemKey]
      const desc = {
        identity: selectedAgentImportSource.kind === 'hermes'
          ? ob.importAgentIdentityHermesDesc
          : ob.importAgentIdentityOpenClawDesc,
        skills: ob.importSkillsDesc(selectedAgentImportSource.skillsCount),
        mcp: ob.importMcpServersDesc(selectedAgentImportSource.mcpServers.join(', ')),
        providers: ob.importLlmProvidersDesc(selectedAgentImportSource.llmProviders.join(', ')),
      }[itemKey]

      return (
        <div
          key={itemKey}
          role="button"
          tabIndex={0}
          onClick={() => {
            if (!agentImporting) handleAgentImportItemToggle(selectedAgentImportSource.kind, itemKey)
          }}
          onKeyDown={(event) => {
            if (agentImporting) return
            if (event.key === 'Enter' || event.key === ' ') {
              event.preventDefault()
              handleAgentImportItemToggle(selectedAgentImportSource.kind, itemKey)
            }
          }}
          className="flex cursor-pointer items-center gap-3 rounded-[10px] bg-[var(--c-bg-menu)] p-3.5 transition-colors hover:bg-[var(--c-bg-deep)]"
          style={{
            border: '0.5px solid var(--c-border-subtle)',
          }}
        >
          <div className="min-w-0 flex-1">
            <div
              className="text-[13px] font-medium"
              style={{ color: selection[itemKey] ? 'var(--c-text-primary)' : 'var(--c-text-secondary)' }}
            >
              {title}
            </div>
            <div className="mt-0.5 truncate text-[11px] text-[var(--c-placeholder)]">{desc}</div>
          </div>
          <span
            onClick={(event) => event.stopPropagation()}
            onKeyDown={(event) => event.stopPropagation()}
          >
            <SettingsSwitch
              checked={selection[itemKey]}
              onChange={() => handleAgentImportItemToggle(selectedAgentImportSource.kind, itemKey)}
              disabled={agentImporting}
            />
          </span>
        </div>
      )
    })

    return (
      <div className="flex w-full max-w-xl min-w-0 flex-col gap-4 overflow-hidden">
        <div className="min-w-0">
          <div className="min-w-0 text-lg font-medium text-[var(--c-text-heading)] [overflow-wrap:anywhere]">
            {ob.importFrom(selectedAgentImportSource.name)}
          </div>
          <div className="mt-1 min-w-0 text-[13px] text-[var(--c-placeholder)] [overflow-wrap:anywhere]">
            {ob.importDetectedAt(selectedAgentImportSource.sourcePath)}
          </div>
        </div>

        <div className="flex flex-col gap-2">{importRows}</div>

        {agentImportError && (
          <p className="text-sm" style={{ color: 'var(--c-status-error)' }}>{agentImportError}</p>
        )}

        <div className="flex flex-wrap items-center gap-2.5">
          <button
            type="button"
            onClick={() => void handleAgentImportSelected()}
            disabled={agentImporting}
            className={primaryBtnCls(agentImporting)}
            style={{
              background: 'var(--c-btn-bg)',
              color: 'var(--c-btn-text)',
            }}
          >
            {agentImporting ? (
              <>
                <Loader2 size={14} className="animate-spin" />
                <span>{ob.importSelected}</span>
              </>
            ) : (
              <AgentImportButtonContent
                source={selectedAgentImportSource.kind}
                label={ob.importSelected}
              />
            )}
          </button>
          <button
            type="button"
            onClick={handleAgentImportBack}
            disabled={agentImporting}
            className={actionBtnCls(agentImporting)}
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            {ob.back}
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      <SettingsSectionHeader title={ds.advancedData} description={ds.advancedDataDesc} />

      <SettingsSection>
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap gap-3">
            <button
              type="button"
              onClick={() => void handleChoose()}
              disabled={busy}
              className={actionBtnCls(busy)}
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              <FolderOpen size={14} />
              <span>{ds.advancedChooseFolder}</span>
            </button>
            <button
              type="button"
              onClick={() => setExportDialogOpen(true)}
              disabled={busy}
              className={actionBtnCls(busy)}
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              <Download size={14} />
              <span>{ds.advancedExport}</span>
            </button>
            <button
              type="button"
              onClick={() => void handleImport()}
              disabled={busy}
              className={actionBtnCls(busy)}
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              <Import size={14} />
              <span>{ds.advancedImport}</span>
            </button>
          </div>
          {visibleAgentImportSources.length > 0 && (
            <div className="flex flex-wrap gap-3">
              {visibleAgentImportSources.map((source) => (
                <button
                  key={source.kind}
                  type="button"
                  onClick={() => {
                    setAgentImportError('')
                    setSelectedAgentImport(source.kind)
                  }}
                  disabled={busy}
                  className={primaryBtnCls(busy)}
                  style={{
                    background: 'var(--c-btn-bg)',
                    color: 'var(--c-btn-text)',
                    maxWidth: '100%',
                    overflow: 'hidden',
                  }}
                >
                  <AgentImportButtonContent source={source.kind} label={ob.importFrom(source.name)} />
                </button>
              ))}
            </div>
          )}
        </div>
        {actionError && <p className="mt-2 text-sm" style={{ color: 'var(--c-status-error)' }}>{actionError}</p>}
      </SettingsSection>

      <Modal
        open={exportDialogOpen}
        onClose={() => {
          if (actionLoading !== 'export') setExportDialogOpen(false)
        }}
        title={ds.advancedExportTitle}
        width="520px"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-[var(--c-text-secondary)]">{ds.advancedExportDesc}</p>
          <div className="flex items-center justify-between text-sm">
            <span className="text-[var(--c-text-secondary)]">{selectedSections.length} / {exportOptions.length}</span>
            <div className="flex gap-2">
              <button
                type="button"
                className={actionBtnCls()}
                onClick={() => setSelectedSections(DESKTOP_EXPORT_SECTIONS)}
              >
                {ds.advancedExportSelectAll}
              </button>
              <button
                type="button"
                className={actionBtnCls()}
                onClick={() => setSelectedSections([])}
              >
                {ds.advancedExportClearAll}
              </button>
            </div>
          </div>
          <div className="grid gap-2">
            {exportOptions.map((option) => {
              const checked = selectedSections.includes(option.key)
              return (
                <label
                  key={option.key}
                  className="flex items-center gap-3 rounded-xl px-3 py-2 text-sm"
                  style={{ border: '0.5px solid var(--c-border-subtle)' }}
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => toggleSection(option.key)}
                  />
                  <span className="text-[var(--c-text-primary)]">{option.label}</span>
                </label>
              )
            })}
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => setExportDialogOpen(false)}
              disabled={actionLoading === 'export'}
              className={actionBtnCls(actionLoading === 'export')}
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              {ds.advancedCancel}
            </button>
            <button
              type="button"
              onClick={() => void handleExport()}
              disabled={actionLoading === 'export' || selectedSections.length === 0}
              className={primaryBtnCls(actionLoading === 'export' || selectedSections.length === 0)}
              style={{ background: 'var(--c-btn-bg)' }}
            >
              {actionLoading === 'export' && <Loader2 size={14} className="animate-spin" />}
              <span>{ds.advancedExport}</span>
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}

function LogsPane() {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const api = getDesktopApi()
  const [logs, setLogs] = useState<DesktopLogEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [source, setSource] = useState<DesktopLogQuery['source']>('all')
  const [level, setLevel] = useState<DesktopLogQuery['level']>('all')
  const [search, setSearch] = useState('')
  const termRef = useRef<HTMLDivElement>(null)

  const loadLogs = useCallback(async () => {
    if (!api?.advanced) return
    setLoading(true)
    setError('')
    try {
      const result = await api.advanced.listLogs({ source, level, search, limit: 200 })
      setLogs(result.entries)
    } catch (err) {
      setError(err instanceof Error ? err.message : t.requestFailed)
    } finally {
      setLoading(false)
    }
  }, [api, source, level, search, t.requestFailed])

  useEffect(() => { void loadLogs() }, [loadLogs])

  useEffect(() => {
    if (termRef.current) termRef.current.scrollTop = termRef.current.scrollHeight
  }, [logs])

  return (
    <div className="flex flex-col gap-6">
      <SettingsSectionHeader title={ds.advancedLogs} description={ds.advancedLogsDesc} />

      <SettingsSection overflow="visible">
        <div className="flex flex-wrap gap-3">
          <SettingsSelect
            value={source ?? 'all'}
            options={[
              { value: 'all', label: ds.advancedLogsAllSources },
              { value: 'main', label: ds.advancedLogsMain },
              { value: 'sidecar', label: ds.advancedLogsSidecar },
            ]}
            onChange={(v) => setSource(v as DesktopLogQuery['source'])}
          />
          <SettingsSelect
            value={level ?? 'all'}
            options={[
              { value: 'all', label: ds.advancedLogsAllLevels },
              { value: 'info', label: 'info' },
              { value: 'warn', label: 'warn' },
              { value: 'error', label: 'error' },
              { value: 'debug', label: 'debug' },
              { value: 'other', label: 'other' },
            ]}
            onChange={(v) => setLevel(v as DesktopLogQuery['level'])}
          />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={ds.advancedLogsSearchPlaceholder}
            className={settingsInputCls('sm') + ' h-9 min-w-[200px]'}
          />
          <button
            type="button"
            onClick={() => void loadLogs()}
            className={actionBtnCls()}
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <RefreshCw size={14} />
            <span>{ds.advancedRefresh}</span>
          </button>
        </div>
        {error && <p className="mt-3 text-sm" style={{ color: 'var(--c-status-error)' }}>{error}</p>}
      </SettingsSection>

      <div
        ref={termRef}
        className="min-h-[320px] max-h-[600px] overflow-auto rounded-xl p-4"
        style={{
          background: '#0d1117',
          border: '1px solid #21262d',
          fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
        }}
      >
        {loading ? (
          <div className="flex min-h-[180px] items-center justify-center">
            <Loader2 size={18} className="animate-spin" style={{ color: '#8b949e' }} />
          </div>
        ) : logs.length === 0 ? (
          <span style={{ color: '#8b949e', fontSize: 12 }}>{ds.advancedLogsEmpty}</span>
        ) : (
          <div className="flex flex-col">
            {logs.map((entry, i) => (
              <div
                key={`${entry.source}-${entry.timestamp}-${i}`}
                className="whitespace-pre-wrap break-all py-[1px] leading-[20px]"
                style={{ fontSize: 12 }}
              >
                <span style={{ color: '#8b949e' }}>{entry.timestamp}</span>
                <span style={{ color: '#30363d' }}> | </span>
                <span style={{ color: logLevelColor(entry.level), fontWeight: 500 }}>{logLevelTag(entry.level)}</span>
                <span style={{ color: '#30363d' }}> | </span>
                <span style={{ color: '#58a6ff' }}>{entry.source}</span>
                <span style={{ color: '#30363d' }}> | </span>
                <span style={{ color: entry.level === 'error' ? '#f87171' : '#c9d1d9' }}>{entry.message}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

// -- Main component --

export function AdvancedSettings({ accessToken, initialKey = null }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const requestedKey = initialKey ?? 'voice'

  const [activeKey, setActiveKey] = useState<AdvancedSettingsKey>(requestedKey)

  const loadOverview = useCallback(async () => {
    // NetworkPane 和 DataPane 调用此方法刷新配置
  }, [])

  useEffect(() => { void loadOverview() }, [loadOverview])



  const navItems: Array<{ key: AdvancedSettingsKey; icon: LucideIcon; label: string }> = [
    { key: 'voice', icon: Mic, label: ds.voiceTitle },
    { key: 'network', icon: Network, label: ds.advancedNetwork },
    { key: 'data', icon: Database, label: ds.advancedData },
    { key: 'logs', icon: ScrollText, label: ds.advancedLogs },
  ]

  return (
    <div className="-m-6 flex min-h-0 min-w-0 overflow-hidden" style={{ height: 'calc(100% + 48px)' }}>
      <div className="flex w-[160px] shrink-0 flex-col overflow-hidden max-[1230px]:w-[140px] xl:w-[180px]" style={{ borderRight: '0.5px solid var(--c-border-subtle)' }}>
        <div className="flex-1 overflow-y-auto px-2 py-1">
          <div className="flex flex-col gap-[3px]">
            {navItems.map(({ key, icon: Icon, label }) => (
              <button
                key={key}
                type="button"
                onClick={() => setActiveKey(key)}
                className={[
                  'flex h-[38px] items-center gap-2.5 truncate rounded-lg px-2.5 text-left text-[14px] font-medium transition-all duration-[120ms] active:scale-[0.96]',
                  activeKey === key
                    ? 'rounded-[10px] bg-[var(--c-bg-deep)] text-[var(--c-text-heading)]'
                    : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]',
                ].join(' ')}
              >
                <Icon size={16} />
                <span>{label}</span>
              </button>
            ))}
          </div>
        </div>
      </div>

      <div className="min-w-0 flex-1 overflow-y-auto p-4 max-[1230px]:p-3 sm:p-5">
        <div className="mx-auto min-w-0 max-w-4xl">
          {activeKey === 'voice' && <VoiceSettings accessToken={accessToken} />}
          {activeKey === 'network' && <NetworkPane onReloadOverview={loadOverview} />}
          {activeKey === 'data' && (
            <DataPane onReloadOverview={loadOverview} />
          )}
          {activeKey === 'logs' && <LogsPane />}
        </div>
      </div>
    </div>
  )
}
