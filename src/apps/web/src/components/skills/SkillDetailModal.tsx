import { Download, Github, MessageSquare, Trash2, X } from 'lucide-react'
import { PillToggle } from '@arkloop/shared'
import type { ViewSkill } from './types'
import { formatDate } from './types'
import { openExternal } from '../../openExternal'

type SkillTextSubset = {
  sourceOfficial: string
  sourceGitHub: string
  sourceBuiltin: string
  sourceCustom: string
  enabledByDefault: string
  manualAvailable: string
  disable: string
  trySkill: string
  trySkillPrompt: (skillKey: string) => string
  download: string
  remove: string
  detailDescription: string
  noDescription: string
  detailVersion: string
  detailSource: string
  detailUpdatedAt: string
  scanStatusLabel: (status: string) => string
}

type Props = {
  item: ViewSkill
  onClose: () => void
  onEnable: (item: ViewSkill) => void
  onDisable: (item: ViewSkill) => void
  onRemove: (item: ViewSkill) => void
  onTrySkill?: (prompt: string) => void
  skillText: SkillTextSubset
  locale: string
  active: (item: ViewSkill) => boolean
  platformAvailabilityLabel: (status?: ViewSkill['platform_status']) => string
  platformAvailabilityStyle: (status?: ViewSkill['platform_status']) => React.CSSProperties | null
  scanStatusBadge: (item: ViewSkill) => { label: string; style: React.CSSProperties } | null
}

export function SkillDetailModal({
  item,
  onClose,
  onEnable,
  onDisable,
  onRemove,
  onTrySkill,
  skillText,
  locale,
  active,
  platformAvailabilityLabel,
  platformAvailabilityStyle,
  scanStatusBadge,
}: Props) {
  const enabled = active(item)
  const platformBadgeLabel = item.is_platform ? platformAvailabilityLabel(item.platform_status) : ''
  const platformBadgeStyle = item.is_platform ? platformAvailabilityStyle(item.platform_status) : null
  const scanBadge = scanStatusBadge(item)
  const providerLabel = item.registry_provider?.trim().toLowerCase() === 'clawhub'
    ? 'ClawHub'
    : item.registry_provider?.trim() || (
      item.source === 'official' ? skillText.sourceOfficial
      : item.source === 'github' ? skillText.sourceGitHub
      : item.is_platform ? skillText.sourceBuiltin
      : skillText.sourceCustom
    )

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center"
      style={{ background: 'rgba(0,0,0,0.12)', backdropFilter: 'blur(2px)', WebkitBackdropFilter: 'blur(2px)' }}
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}
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
              {item.is_platform && (
                <span className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight text-[var(--c-text-secondary)]" style={{ background: 'var(--c-bg-deep)' }}>
                  {skillText.sourceBuiltin}
                </span>
              )}
              {platformBadgeLabel && platformBadgeStyle && (
                <span className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight" style={platformBadgeStyle}>
                  {platformBadgeLabel}
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
                onClose()
                onTrySkill?.(skillText.trySkillPrompt(item.skill_key))
              }}
              disabled={!item.installed}
              className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors disabled:opacity-40"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)', color: 'var(--c-text-heading)' }}
            >
              <MessageSquare size={13} />
              {skillText.trySkill}
            </button>
            <button
              type="button"
              onClick={onClose}
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
            {!item.is_platform && (
              <button
                type="button"
                disabled={!item.detail_url}
                onClick={() => item.detail_url && openExternal(item.detail_url)}
                className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-transparent"
                style={{ border: '0.5px solid var(--c-border-subtle)' }}
              >
                <Download size={12} />
                {skillText.download}
              </button>
            )}
            {item.installed && item.version && (
              <button
                type="button"
                onClick={() => { onClose(); onRemove(item) }}
                className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors hover:bg-[var(--c-error-bg)]"
                style={{ color: 'var(--c-status-error-text)' }}
              >
                <Trash2 size={12} />
                {skillText.remove}
              </button>
            )}
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <span className="text-xs text-[var(--c-text-tertiary)]">
              {platformAvailabilityLabel(item.platform_status) || (enabled ? skillText.enabledByDefault : skillText.disable)}
            </span>
            <PillToggle
              checked={enabled}
              onChange={() => {
                if (enabled) onDisable(item)
                else onEnable(item)
              }}
            />
          </div>
        </div>
      </div>
    </div>
  )
}
