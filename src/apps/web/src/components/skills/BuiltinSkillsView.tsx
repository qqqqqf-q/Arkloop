import { Loader2, RefreshCw, Trash2 } from 'lucide-react'
import { PillToggle } from '@arkloop/shared'
import { listPlatformSkills, setPlatformSkillOverride, type PlatformSkillItem } from '../../api'
import type { ViewSkill } from './types'
import { matchesSkillQuery } from './types'

type SkillTextSubset = {
  builtinTitle: string
  builtinEmpty: string
  sourceBuiltin: string
  enabledByDefault: string
  manualAvailable: string
  restore: string
  disableFailed: string
  importFailed: string
  scanStatusLabel: (status: string) => string
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
  platformAvailabilityStyle: (status?: ViewSkill['platform_status']) => React.CSSProperties | null
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
    <div className="flex flex-col gap-2">
      <span className="text-xs font-medium text-[var(--c-text-tertiary)]">
        {skillText.builtinTitle}
      </span>
      {builtinLoading ? (
        <div className="flex h-40 items-center justify-center">
          <Loader2 size={16} className="animate-spin text-[var(--c-text-tertiary)]" />
        </div>
      ) : filtered.length === 0 ? (
        <div
          className="flex flex-col items-center justify-center gap-1 rounded-xl py-12 text-center"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <span className="text-sm font-medium text-[var(--c-text-heading)]">{skillText.builtinEmpty}</span>
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          {filtered.map((skill) => {
            const isRemoved = skill.platform_status === 'removed'
            const isEnabled = skill.platform_status === 'auto'
            const availabilityLabel = platformAvailabilityLabel(skill.platform_status)
            const availabilityStyle = platformAvailabilityStyle(skill.platform_status)
            const busy = busySkillId === `builtin:${skill.skill_key}@${skill.version}`
            return (
              <div
                key={`${skill.skill_key}@${skill.version}`}
                className="flex items-center gap-3 rounded-xl p-3 transition-colors duration-100"
                style={{
                  border: '0.5px solid var(--c-border-subtle)',
                  background: isRemoved ? 'var(--c-bg-page)' : 'var(--c-bg-menu)',
                  opacity: isRemoved ? 0.55 : 1,
                }}
              >
                <div className="flex min-w-0 flex-1 flex-col gap-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-medium text-[var(--c-text-heading)]">
                      {skill.display_name}
                    </span>
                    <span
                      className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight text-[var(--c-text-secondary)]"
                      style={{ background: 'var(--c-bg-deep)' }}
                    >
                      {skillText.sourceBuiltin}
                    </span>
                    {availabilityLabel && availabilityStyle && (
                      <span
                        className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight"
                        style={availabilityStyle}
                      >
                        {availabilityLabel}
                      </span>
                    )}
                  </div>
                  {skill.description && (
                    <span className="line-clamp-2 text-xs text-[var(--c-text-tertiary)]">{skill.description}</span>
                  )}
                </div>

                {isRemoved ? (
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
                    className="flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors hover:bg-[var(--c-bg-deep)]"
                    style={{ border: '0.5px solid var(--c-border-subtle)', color: 'var(--c-text-heading)' }}
                  >
                    {busy ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />}
                    {skillText.restore}
                  </button>
                ) : (
                  <>
                    <PillToggle
                      checked={isEnabled}
                      disabled={busy}
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
                  </>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
