import { Download, Github, Loader2, MessageSquare, MoreHorizontal, RefreshCw, Trash2 } from 'lucide-react'
import { PillToggle } from '@arkloop/shared'
import { openExternal } from '../../openExternal'
import type { ViewSkill } from './types'
import { formatDate } from './types'
import { DropdownAction } from './DropdownAction'

type Props = {
  items: ViewSkill[]
  loading: boolean
  viewMode: 'installed' | 'marketplace'
  busySkillId: string | null
  menuSkillId: string | null
  setMenuSkillId: (id: string | null) => void
  onDetailSkill: (skill: ViewSkill) => void
  onEnable: (item: ViewSkill) => void
  onDisable: (item: ViewSkill) => void
  onRemove: (item: ViewSkill) => void
  onTrySkill?: (prompt: string) => void
  skillText: {
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
  }
  locale: string
  platformAvailabilityLabel: (status?: ViewSkill['platform_status']) => string
  platformAvailabilityStyle: (status?: ViewSkill['platform_status']) => React.CSSProperties | null
  scanStatusBadge: (item: ViewSkill) => { label: string; style: React.CSSProperties } | null
  active: (item: ViewSkill) => boolean
  cardMenuRef: React.RefObject<HTMLDivElement | null>
}

export function SkillList({
  items,
  loading,
  viewMode,
  busySkillId,
  menuSkillId,
  setMenuSkillId,
  onDetailSkill,
  onEnable,
  onDisable,
  onRemove,
  onTrySkill,
  skillText,
  locale,
  platformAvailabilityLabel,
  platformAvailabilityStyle,
  scanStatusBadge,
  active,
  cardMenuRef,
}: Props) {
  if (loading) {
    return (
      <div className="flex h-40 items-center justify-center">
        <Loader2 size={16} className="animate-spin text-[var(--c-text-tertiary)]" />
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <div
        className="flex flex-col items-center justify-center gap-1 rounded-xl py-12 text-center"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{skillText.emptyTitle}</span>
        <span className="text-xs text-[var(--c-text-tertiary)]">
          {viewMode === 'installed' ? skillText.emptyBodyNoMarket : skillText.emptyDesc}
        </span>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      {items.map((item) => {
        const busy = busySkillId === item.id
        const enabled = active(item)
        const platformBadgeLabel = item.is_platform ? platformAvailabilityLabel(item.platform_status) : ''
        const platformBadgeStyle = item.is_platform ? platformAvailabilityStyle(item.platform_status) : null
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
            className="flex items-start gap-3 rounded-xl p-3 cursor-pointer transition-colors duration-100 bg-[var(--c-bg-menu)] hover:bg-[var(--c-bg-deep)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
            onClick={() => onDetailSkill(item)}
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
                {item.is_platform && (
                  <span
                    className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight text-[var(--c-text-secondary)]"
                    style={{ background: 'var(--c-bg-deep)' }}
                  >
                    {skillText.sourceBuiltin}
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
                {platformBadgeLabel && platformBadgeStyle ? (
                  <span
                    className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight"
                    style={platformBadgeStyle}
                  >
                    {platformBadgeLabel}
                  </span>
                ) : enabled && (
                  <span
                    className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight"
                    style={{ background: 'var(--c-status-ok-bg)', color: 'var(--c-status-ok-text)' }}
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

            <div className="mt-0.5" onClick={(e) => e.stopPropagation()}>
              <PillToggle
                checked={enabled}
                disabled={busy}
                onChange={() => {
                  if (enabled) onDisable(item)
                  else onEnable(item)
                }}
              />
            </div>

            <div className="relative" ref={menuSkillId === item.id ? cardMenuRef : undefined} onClick={(e) => e.stopPropagation()}>
              <button
                type="button"
                onClick={() => setMenuSkillId(menuSkillId === item.id ? null : item.id)}
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
                    disabled={!item.installed}
                    onClick={() => {
                      setMenuSkillId(null)
                      onTrySkill?.(skillText.trySkillPrompt(item.skill_key))
                    }}
                  />
                  {!item.is_platform && (
                    <DropdownAction
                      icon={<Download size={14} />}
                      label={skillText.download}
                      disabled={!item.detail_url}
                      onClick={() => {
                        setMenuSkillId(null)
                        if (item.detail_url) openExternal(item.detail_url)
                      }}
                    />
                  )}
                  {!item.is_platform && (
                    <DropdownAction
                      icon={<RefreshCw size={14} />}
                      label={skillText.replace}
                      disabled={item.source === 'custom' || (!item.detail_url && !item.repository_url)}
                      onClick={() => { setMenuSkillId(null); onEnable(item) }}
                    />
                  )}
                  <DropdownAction
                    icon={<Trash2 size={14} />}
                    label={skillText.remove}
                    disabled={!item.installed || !item.version}
                    destructive
                    onClick={() => { setMenuSkillId(null); onRemove(item) }}
                  />
                </div>
              )}
            </div>
          </div>
        )
      })}
    </div>
  )
}
