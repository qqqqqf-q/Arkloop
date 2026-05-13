import type { CSSProperties } from 'react'
import { Loader2, RefreshCw, Trash2 } from 'lucide-react'
import { listPlatformSkills, setPlatformSkillOverride, type PlatformSkillItem } from '../../api'
import type { ViewSkill } from './types'
import { matchesSkillQuery } from './types'
import { secondaryButtonBorderStyle, secondaryButtonXsCls } from '../buttonStyles'
import { SettingsSwitch } from '../settings/_SettingsSwitch'
import {
  SETTINGS_CARD_INSET_RULE_CLASS,
  SETTINGS_CARD_SURFACE_OVERFLOW_VISIBLE_CLASS,
  SETTINGS_TWO_COLUMN_GRID_CLASS,
  SettingsGroup,
} from '../settings/_SettingsLayout'

const builtinCardControlsCol = 'flex h-7 w-11 shrink-0 items-center justify-end'

type SkillTextSubset = {
  builtinTitle: string
  builtinEmpty: string
  sourceBuiltin: string
  restore: string
  disableFailed: string
  importFailed: string
}

type Props = {
  builtinSkills: PlatformSkillItem[]
  builtinLoading: boolean
  busySkillId: string | null
  setBusySkillId: (id: string | null) => void
  setError: (err: string) => void
  query: string
  accessToken: string
  skillText: SkillTextSubset
  refreshInstalled: () => Promise<unknown>
  setBuiltinSkills: (items: PlatformSkillItem[]) => void
  platformAvailabilityLabel: (status?: ViewSkill['platform_status']) => string
  platformAvailabilityStyle: (status?: ViewSkill['platform_status']) => CSSProperties | null
}

export function BuiltinSkillsView({
  builtinSkills,
  builtinLoading,
  busySkillId,
  setBusySkillId,
  setError,
  query,
  accessToken,
  skillText,
  refreshInstalled,
  setBuiltinSkills,
  platformAvailabilityLabel,
  platformAvailabilityStyle,
}: Props) {
  const filtered = builtinSkills.filter((s) =>
    matchesSkillQuery({
      id: s.skill_key,
      skill_key: s.skill_key,
      display_name: s.display_name,
      description: s.description,
      source: 'platform',
      installed: true,
      enabled_by_default: s.platform_status === 'auto',
    } as ViewSkill, query.trim().toLowerCase())
  )

  return (
    <SettingsGroup title={skillText.builtinTitle}>
      {builtinLoading ? (
        <div className={`${SETTINGS_TWO_COLUMN_GRID_CLASS}`}>
          <div className="col-span-full flex h-40 items-center justify-center">
            <Loader2 size={16} className="animate-spin text-[var(--c-text-tertiary)]" />
          </div>
        </div>
      ) : filtered.length === 0 ? (
        <div className={`${SETTINGS_TWO_COLUMN_GRID_CLASS}`}>
          <div className="col-span-full flex flex-col items-center justify-center rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] px-4 py-12 text-center">
            <span className="text-sm font-medium text-[var(--c-text-primary)]">{skillText.builtinEmpty}</span>
          </div>
        </div>
      ) : (
        <div className={SETTINGS_TWO_COLUMN_GRID_CLASS}>
          {filtered.map((skill) => {
            const isRemoved = skill.platform_status === 'removed'
            const isEnabled = skill.platform_status === 'auto'
            const availabilityLabel = platformAvailabilityLabel(skill.platform_status)
            const availabilityStyle = platformAvailabilityStyle(skill.platform_status)
            const busy = busySkillId === `builtin:${skill.skill_key}@${skill.version}`
            return (
              <div
                key={`${skill.skill_key}@${skill.version}`}
                className={[
                  SETTINGS_CARD_SURFACE_OVERFLOW_VISIBLE_CLASS,
                  'flex h-full flex-col',
                  isRemoved ? 'opacity-55' : '',
                ].filter(Boolean).join(' ')}
              >
                {isRemoved ? (
                  <div className="flex flex-wrap items-center gap-2 px-5 pt-3 pb-1.5">
                    <span className="min-w-0 flex-1 truncate text-sm font-semibold text-[var(--c-text-heading)]">
                      {skill.display_name}
                    </span>
                    <div className="ml-auto shrink-0">
                      <button
                        type="button"
                        disabled={busy}
                        onClick={async () => {
                          setBusySkillId(`builtin:${skill.skill_key}@${skill.version}`)
                          try {
                            await setPlatformSkillOverride(accessToken, skill.skill_key, skill.version, 'auto')
                            const items = await listPlatformSkills(accessToken)
                            setBuiltinSkills(items)
                            await refreshInstalled()
                          } catch {
                            setError(skillText.importFailed)
                          } finally {
                            setBusySkillId(null)
                          }
                        }}
                        className={secondaryButtonXsCls}
                        style={secondaryButtonBorderStyle}
                      >
                        {busy ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />}
                        {skillText.restore}
                      </button>
                    </div>
                  </div>
                ) : (
                  <div className="grid items-center gap-2 px-5 pt-3 pb-1.5 sm:grid-cols-[minmax(0,1fr)_auto] sm:gap-6">
                    <span className="min-w-0 truncate text-sm font-semibold text-[var(--c-text-heading)]">
                      {skill.display_name}
                    </span>
                    <div className="flex min-w-0 items-center sm:justify-self-end">
                      <SettingsSwitch
                        checked={isEnabled}
                        disabled={busy}
                        size="sm"
                        onChange={async () => {
                          setBusySkillId(`builtin:${skill.skill_key}@${skill.version}`)
                          try {
                            const newStatus = isEnabled ? 'manual' : 'auto'
                            await setPlatformSkillOverride(accessToken, skill.skill_key, skill.version, newStatus)
                            const refreshed = await listPlatformSkills(accessToken)
                            setBuiltinSkills(refreshed)
                            await refreshInstalled()
                          } catch {
                            setError(skillText.disableFailed)
                          } finally {
                            setBusySkillId(null)
                          }
                        }}
                      />
                    </div>
                  </div>
                )}

                {skill.description ? (
                  <div className="flex min-h-0 flex-1 flex-col px-5 pb-2.5">
                    <span className="line-clamp-2 text-sm leading-snug text-[var(--c-text-tertiary)]">{skill.description}</span>
                  </div>
                ) : (
                  <div className="min-h-0 flex-1 px-5 pb-2.5" />
                )}

                <div className={SETTINGS_CARD_INSET_RULE_CLASS} aria-hidden />

                <div className="flex items-center gap-3 px-5 py-2.5">
                  <div className="flex min-h-5 min-w-0 flex-1 flex-wrap items-center gap-x-1.5 gap-y-1 text-[12px] leading-5 text-[var(--c-text-muted)]">
                    <span
                      className="inline-flex h-5 shrink-0 items-center rounded px-1.5 text-[10px] font-medium leading-none text-[var(--c-text-secondary)]"
                      style={{ background: 'var(--c-bg-deep)' }}
                    >
                      {skillText.sourceBuiltin}
                    </span>
                    {availabilityLabel && availabilityStyle ? (
                      <span className="inline-flex h-5 shrink-0 items-center rounded px-1.5 text-[10px] font-medium leading-none" style={availabilityStyle}>
                        {availabilityLabel}
                      </span>
                    ) : null}
                  </div>
                  {!isRemoved ? (
                    <div className={`relative ${builtinCardControlsCol}`}>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={async () => {
                          setBusySkillId(`builtin:${skill.skill_key}@${skill.version}`)
                          try {
                            await setPlatformSkillOverride(accessToken, skill.skill_key, skill.version, 'removed')
                            const refreshed = await listPlatformSkills(accessToken)
                            setBuiltinSkills(refreshed)
                            await refreshInstalled()
                          } catch {
                            setError(skillText.importFailed)
                          } finally {
                            setBusySkillId(null)
                          }
                        }}
                        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md transition-colors hover:bg-[var(--c-error-bg)]"
                        style={{ color: 'var(--c-status-error-text)' }}
                      >
                        {busy ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                      </button>
                    </div>
                  ) : (
                    <div className={builtinCardControlsCol} />
                  )}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </SettingsGroup>
  )
}
