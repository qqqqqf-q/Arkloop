import React, { useCallback, useEffect, useState } from 'react'
import { ChevronDown, ChevronRight, FolderOpen, Loader2, Plus, Trash2 } from 'lucide-react'
import { discoverExternalSkills, getExternalDirs, setExternalDirs, type ExternalSkillDir } from '../../api'
import type { ViewSkill } from './types'
import { SkillList } from './SkillList'

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
  manualAvailable: string
  scanStatusLabel: (status: string) => string
  externalTitle: string
  externalEmpty: string
  externalNoSkills: string
  externalAddDir: string
  externalAddPlaceholder: string
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
  const [externalOpen, setExternalOpen] = useState(false)

  const refreshExternal = useCallback(async () => {
    setExternalLoading(true)
    setExternalError('')
    try {
      const res = await discoverExternalSkills(accessToken)
      setDirs(res.dirs ?? [])
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
    <div className="flex flex-col gap-2">
      <span className="text-xs font-medium text-[var(--c-text-tertiary)]">
        {skillText.searchResults(items.length)}
      </span>

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

      {/* external skills collapsible section */}
      <div
        className="mt-2 rounded-xl overflow-hidden"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <button
          type="button"
          className="flex w-full items-center gap-2 p-3 text-left select-none transition-colors bg-[var(--c-bg-menu)] hover:bg-[var(--c-bg-deep)]"
          onClick={() => setExternalOpen((v) => !v)}
        >
          {externalOpen
            ? <ChevronDown size={14} className="shrink-0 text-[var(--c-text-tertiary)]" />
            : <ChevronRight size={14} className="shrink-0 text-[var(--c-text-tertiary)]" />
          }
          <FolderOpen size={14} className="shrink-0 text-[var(--c-text-tertiary)]" />
          <span className="flex-1 text-sm font-medium text-[var(--c-text-heading)]">
            {skillText.externalTitle}
          </span>
          {!externalLoading && (
            <span
              className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight text-[var(--c-text-secondary)]"
              style={{ background: 'var(--c-bg-deep)' }}
            >
              {dirs.reduce((acc, d) => acc + d.skills.length, 0)}
            </span>
          )}
        </button>

        <div
          className="grid transition-[grid-template-rows] duration-200 ease-out"
          style={{ gridTemplateRows: externalOpen ? '1fr' : '0fr' }}
        >
          <div className="overflow-hidden" style={{ borderTop: externalOpen ? '0.5px solid var(--c-border-subtle)' : 'none' }}>
          <div className="flex flex-col gap-2 p-3">
            {externalError && (
              <p className="text-xs pt-3" style={{ color: 'var(--c-status-error-text)' }}>{externalError}</p>
            )}
            {externalLoading ? (
              <div className="flex h-20 items-center justify-center">
                <Loader2 size={14} className="animate-spin text-[var(--c-text-tertiary)]" />
              </div>
            ) : dirs.length === 0 ? (
              <div className="py-4 text-center">
                <span className="text-xs text-[var(--c-text-muted)]">{skillText.externalEmpty}</span>
              </div>
            ) : (
              <div className="flex flex-col gap-1 pt-2">
                {dirs.map((dir) => {
                  const open = expanded[dir.path] !== false
                  return (
                    <div
                      key={dir.path}
                      className="rounded-lg overflow-hidden"
                      style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
                    >
                      <div
                        className="flex items-center gap-2 px-3 py-2 cursor-pointer select-none transition-colors hover:bg-[var(--c-bg-deep)]"
                        onClick={() => toggleDir(dir.path)}
                      >
                        {open
                          ? <ChevronDown size={12} className="shrink-0 text-[var(--c-text-tertiary)]" />
                          : <ChevronRight size={12} className="shrink-0 text-[var(--c-text-tertiary)]" />
                        }
                        <span className="min-w-0 flex-1 truncate text-xs font-medium text-[var(--c-text-heading)]">
                          {dir.path}
                        </span>
                        <span
                          className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight text-[var(--c-text-secondary)]"
                          style={{ background: 'var(--c-bg-deep)' }}
                        >
                          {dir.skills.length}
                        </span>
                        <button
                          type="button"
                          disabled={saving}
                          onClick={(e) => { e.stopPropagation(); void handleRemoveDir(dir.path) }}
                          className="flex h-6 w-6 shrink-0 items-center justify-center rounded transition-colors hover:bg-[var(--c-error-bg)]"
                          style={{ color: 'var(--c-status-error-text)' }}
                        >
                          {saving ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
                        </button>
                      </div>
                      <div
                        className="grid transition-[grid-template-rows] duration-200 ease-out"
                        style={{ gridTemplateRows: open ? '1fr' : '0fr' }}
                      >
                        <div className="overflow-hidden">
                        {dir.skills.length > 0 ? (
                          <div className="flex flex-col gap-0.5 px-3 pb-2">
                            {dir.skills.map((skill) => (
                              <div
                                key={skill.path}
                                className="flex items-center gap-2 rounded px-2 py-1 pl-5"
                                style={{ background: 'var(--c-bg-menu)' }}
                              >
                                <span className="min-w-0 flex-1 truncate text-xs text-[var(--c-text-heading)]">
                                  {skill.name}
                                </span>
                                <span className="shrink-0 truncate text-[10px] text-[var(--c-text-muted)]">
                                  {skill.instruction_path.split('/').pop()}
                                </span>
                              </div>
                            ))}
                          </div>
                        ) : (
                          <div className="px-3 pb-2 pl-7">
                            <span className="text-[10px] text-[var(--c-text-muted)]">{skillText.externalNoSkills}</span>
                          </div>
                        )}
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}

            <div className="flex items-center gap-2 pt-1">
              <input
                value={newDir}
                onChange={(e) => setNewDir(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') void handleAddDir() }}
                placeholder={skillText.externalAddPlaceholder}
                className="h-8 min-w-0 flex-1 rounded-lg pl-3 pr-3 text-xs text-[var(--c-text-heading)] outline-none placeholder:text-[var(--c-text-tertiary)]"
                style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
              />
              <button
                type="button"
                disabled={saving || !newDir.trim()}
                onClick={() => void handleAddDir()}
                className="flex h-8 shrink-0 items-center gap-1.5 rounded-lg px-3 text-xs font-medium transition-colors disabled:opacity-40"
                style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
              >
                {saving ? <Loader2 size={12} className="animate-spin" /> : <Plus size={12} />}
                {skillText.externalAddDir}
              </button>
            </div>
          </div>
          </div>
        </div>
      </div>
    </div>
  )
}
