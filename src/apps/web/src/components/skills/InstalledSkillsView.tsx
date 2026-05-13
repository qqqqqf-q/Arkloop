import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { ChevronDown, ChevronRight, FolderOpen, Loader2, Plus, Trash2 } from 'lucide-react'
import { discoverExternalSkills, getExternalDirs, setExternalDirs, type ExternalSkillDir } from '../../api'
import type { ViewSkill } from './types'
import { SkillList } from './SkillList'
import { SettingsGroup, SETTINGS_CARD_SURFACE_CLASS } from '../settings/_SettingsLayout'
import { SettingsButton, SettingsIconButton } from '../settings/_SettingsButton'
import { SettingsInput } from '../settings/_SettingsInput'

type SkillTextSubset = {
  searchResults: (count: number) => string
  emptyTitle: string
  emptyBodyNoMarket: string
  emptyDesc: string
  sourceOfficial: string
  sourceGitHub: string
  sourceBuiltin: string
  enabledByDefault: string
  updatedAt: (value: string) => string
  trySkill: string
  trySkillPrompt: (skillKey: string) => string
  download: string
  replace: string
  remove: string
  cancelAction: string
  removeConfirmTitle: string
  removeConfirmBody: (displayName: string, skillKey: string, version: string) => string
  manualAvailable: string
  scanStatusLabel: (status: string) => string
  externalTitle: string
  externalEmpty: string
  externalNoSkills: string
  externalAddDir: string
  externalAddPlaceholder: string
  externalRemoveDir: string
  externalScanSummary: (dirCount: number, skillCount: number) => string
  externalLoadFailed: string
  externalSaveFailed: string
  externalRemoveFailed: string
}

type Props = {
  items: ViewSkill[]
  loading: boolean
  busySkillId: string | null
  menuSkillId: string | null
  setMenuSkillId: (id: string | null) => void
  onDetailSkill: (skill: ViewSkill) => void
  onEnable: (item: ViewSkill) => void
  onDisable: (item: ViewSkill) => void
  onRemove: (item: ViewSkill) => void
  onTrySkill?: (prompt: string) => void
  skillText: SkillTextSubset
  locale: string
  platformAvailabilityLabel: (status?: ViewSkill['platform_status']) => string
  platformAvailabilityStyle: (status?: ViewSkill['platform_status']) => React.CSSProperties | null
  scanStatusBadge: (item: ViewSkill) => { label: string; style: React.CSSProperties } | null
  active: (item: ViewSkill) => boolean
  cardMenuRef: React.RefObject<HTMLDivElement | null>
  accessToken: string
}

type ExternalSkillDirPayload = Omit<ExternalSkillDir, 'skills'> & {
  skills?: ExternalSkillDir['skills'] | null
}

function normalizeExternalDirs(dirs: ExternalSkillDirPayload[] | null | undefined): ExternalSkillDir[] {
  return (dirs ?? []).map((dir) => ({
    ...dir,
    skills: Array.isArray(dir.skills) ? dir.skills : [],
  }))
}

export function InstalledSkillsView(props: Props) {
  const {
    items, loading, busySkillId, menuSkillId, setMenuSkillId,
    onDetailSkill, onEnable, onDisable, onRemove, onTrySkill,
    skillText, locale, platformAvailabilityLabel, platformAvailabilityStyle,
    scanStatusBadge, active, cardMenuRef, accessToken,
  } = props

  const [dirs, setDirs] = useState<ExternalSkillDir[]>([])
  const [externalLoading, setExternalLoading] = useState(true)
  const [externalError, setExternalError] = useState('')
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [newDir, setNewDir] = useState('')
  const [saving, setSaving] = useState(false)

  const totalSkillCount = useMemo(() => dirs.reduce((acc, d) => acc + d.skills.length, 0), [dirs])

  const refreshExternal = useCallback(async () => {
    setExternalLoading(true)
    setExternalError('')
    try {
      const res = await discoverExternalSkills(accessToken)
      setDirs(normalizeExternalDirs(res.dirs))
    } catch {
      setExternalError(skillText.externalLoadFailed)
    } finally {
      setExternalLoading(false)
    }
  }, [accessToken, skillText.externalLoadFailed])

  useEffect(() => { void refreshExternal() }, [refreshExternal])

  const handleAddDir = async () => {
    const trimmed = newDir.trim()
    if (!trimmed) return
    setSaving(true)
    setExternalError('')
    try {
      const current = await getExternalDirs(accessToken)
      if (!current.includes(trimmed)) {
        await setExternalDirs(accessToken, [...current, trimmed])
      }
      setNewDir('')
      await refreshExternal()
    } catch {
      setExternalError(skillText.externalSaveFailed)
    } finally {
      setSaving(false)
    }
  }

  const handleRemoveDir = async (path: string) => {
    setSaving(true)
    setExternalError('')
    try {
      const current = await getExternalDirs(accessToken)
      await setExternalDirs(accessToken, current.filter((d) => d !== path))
      await refreshExternal()
    } catch {
      setExternalError(skillText.externalRemoveFailed)
    } finally {
      setSaving(false)
    }
  }

  const toggleDir = (path: string) => {
    setExpanded((prev) => ({ ...prev, [path]: !prev[path] }))
  }

  return (
    <div className="flex flex-col gap-6">
      <SettingsGroup title={skillText.searchResults(items.length)}>
        <SkillList
          items={items}
          loading={loading}
          viewMode="installed"
          busySkillId={busySkillId}
          menuSkillId={menuSkillId}
          setMenuSkillId={setMenuSkillId}
          onDetailSkill={onDetailSkill}
          onEnable={onEnable}
          onDisable={onDisable}
          onRemove={onRemove}
          onTrySkill={onTrySkill}
          skillText={skillText}
          locale={locale}
          platformAvailabilityLabel={platformAvailabilityLabel}
          platformAvailabilityStyle={platformAvailabilityStyle}
          scanStatusBadge={scanStatusBadge}
          active={active}
          cardMenuRef={cardMenuRef}
        />
      </SettingsGroup>

      <SettingsGroup title={skillText.externalTitle}>
        <div className={SETTINGS_CARD_SURFACE_CLASS}>
          <div className="flex flex-col gap-4 p-4 sm:p-5">
            {!externalLoading && dirs.length > 0 && (
              <p className="text-[13px] leading-snug text-[var(--c-text-tertiary)]">
                {skillText.externalScanSummary(dirs.length, totalSkillCount)}
              </p>
            )}

            {externalError && (
              <p className="text-xs" style={{ color: 'var(--c-status-error-text)' }}>{externalError}</p>
            )}

            {externalLoading ? (
              <div className="flex h-24 items-center justify-center">
                <Loader2 size={16} className="animate-spin text-[var(--c-text-tertiary)]" />
              </div>
            ) : dirs.length === 0 ? (
              <p className="py-1 text-center text-[13px] text-[var(--c-text-muted)]">{skillText.externalEmpty}</p>
            ) : (
              <div className="flex flex-col gap-2">
                {dirs.map((dir) => {
                  const open = expanded[dir.path] === true
                  return (
                    <div
                      key={dir.path}
                      className="overflow-hidden rounded-[10px] border-[0.5px] border-[var(--c-border-subtle)] bg-[var(--c-bg-input)]"
                    >
                      <div
                        role="button"
                        tabIndex={0}
                        className="flex cursor-pointer items-center gap-2 px-3 py-2.5 select-none outline-none transition-colors hover:bg-[color-mix(in_srgb,var(--c-bg-deep)_30%,transparent)] focus-visible:ring-2 focus-visible:ring-[var(--c-accent)]"
                        onClick={() => toggleDir(dir.path)}
                        onKeyDown={(e) => {
                          if (e.key !== 'Enter' && e.key !== ' ') return
                          e.preventDefault()
                          toggleDir(dir.path)
                        }}
                      >
                        {open
                          ? <ChevronDown size={14} className="shrink-0 text-[var(--c-text-tertiary)]" />
                          : <ChevronRight size={14} className="shrink-0 text-[var(--c-text-tertiary)]" />
                        }
                        <FolderOpen size={14} className="shrink-0 text-[var(--c-text-tertiary)]" />
                        <span
                          className="min-w-0 flex-1 truncate font-mono text-[12px] text-[var(--c-text-heading)] sm:text-[13px]"
                          title={dir.path}
                        >
                          {dir.path}
                        </span>
                        <span
                          className="shrink-0 rounded-[6px] border-[0.5px] border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] px-2 py-0.5 text-[10px] font-medium tabular-nums text-[var(--c-text-secondary)]"
                        >
                          {dir.skills.length}
                        </span>
                        <SettingsIconButton
                          label={skillText.externalRemoveDir}
                          danger
                          variant="framed"
                          disabled={saving}
                          className="shrink-0"
                          onClick={(e) => {
                            e.stopPropagation()
                            void handleRemoveDir(dir.path)
                          }}
                        >
                          {saving ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                        </SettingsIconButton>
                      </div>
                      <div
                        className="grid transition-[grid-template-rows] duration-200 ease-out"
                        style={{ gridTemplateRows: open ? '1fr' : '0fr' }}
                      >
                        <div className="overflow-hidden">
                          <div className="border-t border-[var(--c-border-subtle)] px-3 py-2">
                            {dir.skills.length > 0 ? (
                              <ul className="flex flex-col gap-1">
                                {dir.skills.map((skill) => (
                                  <li
                                    key={skill.path}
                                    className="flex min-h-[36px] items-center gap-2 rounded-[6.5px] px-2.5 py-1.5"
                                    style={{ background: 'var(--c-bg-menu)' }}
                                  >
                                    <span className="min-w-0 flex-1 truncate text-[13px] text-[var(--c-text-primary)]">
                                      {skill.name}
                                    </span>
                                    <span className="shrink-0 max-w-[45%] truncate font-mono text-[11px] text-[var(--c-text-muted)]">
                                      {skill.instruction_path.split('/').pop()}
                                    </span>
                                  </li>
                                ))}
                              </ul>
                            ) : (
                              <p className="pl-1 text-[12px] text-[var(--c-text-muted)]">{skillText.externalNoSkills}</p>
                            )}
                          </div>
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}

            <div className="flex flex-col gap-2 border-t border-[var(--c-border-subtle)] pt-4 sm:flex-row sm:items-center">
              <SettingsInput
                variant="md"
                className="min-w-0 flex-1"
                value={newDir}
                onChange={(e) => setNewDir(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') void handleAddDir() }}
                placeholder={skillText.externalAddPlaceholder}
                disabled={saving || externalLoading}
              />
              <SettingsButton
                variant="primary"
                size="modal"
                className="w-full shrink-0 sm:w-auto"
                disabled={saving || externalLoading || !newDir.trim()}
                icon={saving ? <Loader2 size={14} className="animate-spin" /> : <Plus size={14} />}
                onClick={() => void handleAddDir()}
              >
                {skillText.externalAddDir}
              </SettingsButton>
            </div>
          </div>
        </div>
      </SettingsGroup>
    </div>
  )
}
