import { useState } from 'react'
import { Download, Github, MessageSquare, Trash2 } from 'lucide-react'
import { ConfirmDialog } from '@arkloop/shared'
import type { ViewSkill } from './types'
import { formatDate, formatSkillRegistryProviderLabel } from './types'
import { openExternal } from '../../openExternal'
import { SettingsModalFrame } from '../settings/_SettingsModalFrame'
import { SettingsButton } from '../settings/_SettingsButton'
import { SettingsSwitch } from '../settings/_SettingsSwitch'

const fieldLabelCls = 'text-[11px] font-medium text-[var(--c-placeholder)]'
const statCardCls =
  'flex flex-col gap-1 rounded-[10px] border-[0.5px] border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] p-3'

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
  cancelAction: string
  removeConfirmTitle: string
  removeConfirmBody: (displayName: string, skillKey: string, version: string) => string
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
  const [removeConfirmOpen, setRemoveConfirmOpen] = useState(false)
  const enabled = active(item)
  const platformBadgeLabel = item.is_platform ? platformAvailabilityLabel(item.platform_status) : ''
  const platformBadgeStyle = item.is_platform ? platformAvailabilityStyle(item.platform_status) : null
  const scanBadge = scanStatusBadge(item)
  const providerDisplay = formatSkillRegistryProviderLabel(
    item.registry_provider,
    item.source,
    skillText.sourceOfficial,
  )
  const detailSourceLabel =
    providerDisplay ||
    (item.source === 'github'
      ? skillText.sourceGitHub
      : item.is_platform
        ? skillText.sourceBuiltin
        : item.source === 'custom'
          ? skillText.sourceCustom
          : item.source)
  const showOfficialBadge = item.source === 'official' && Boolean(providerDisplay)
  const showCustomProvider = item.source === 'custom' && Boolean(providerDisplay)

  const tryThenClose = () => {
    onClose()
    onTrySkill?.(skillText.trySkillPrompt(item.skill_key))
  }

  const confirmRemove = () => {
    if (!item.version) return
    setRemoveConfirmOpen(false)
    onClose()
    onRemove(item)
  }

  return (
    <>
      <SettingsModalFrame open title={item.display_name} onClose={onClose} width={510}
        footer={(
          <div className="flex w-full min-w-0 flex-wrap items-center justify-between gap-3">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <SettingsButton
                size="modal"
                variant="secondary"
                disabled={!item.installed}
                icon={<MessageSquare size={14} />}
                onClick={() => tryThenClose()}
              >
                {skillText.trySkill}
              </SettingsButton>
              {!item.is_platform && (
                <SettingsButton
                  size="modal"
                  variant="secondary"
                  disabled={!item.detail_url}
                  icon={<Download size={14} />}
                  onClick={() => item.detail_url && openExternal(item.detail_url)}
                >
                  {skillText.download}
                </SettingsButton>
              )}
              {item.installed && item.version && (
                <SettingsButton
                  size="modal"
                  variant="danger"
                  icon={<Trash2 size={14} />}
                  onClick={() => setRemoveConfirmOpen(true)}
                >
                  {skillText.remove}
                </SettingsButton>
              )}
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <span className="text-xs text-[var(--c-text-tertiary)]">
                {platformAvailabilityLabel(item.platform_status) ||
                  (enabled ? skillText.enabledByDefault : skillText.disable)}
              </span>
              <SettingsSwitch
                checked={enabled}
                onChange={() => {
                  if (enabled) onDisable(item)
                  else onEnable(item)
                }}
              />
            </div>
          </div>
        )}
      >
        <div className="mt-4 flex flex-col gap-4">
          <div className="flex flex-wrap items-center gap-1.5">
            {showOfficialBadge && (
              <span
                className="shrink-0 rounded-[6px] px-2 py-0.5 text-[10px] font-medium leading-tight"
                style={{ background: 'var(--c-pro-bg)', color: '#6ba3f6' }}
              >
                {providerDisplay}
              </span>
            )}
            {item.source === 'github' && (
              <span
                className="flex shrink-0 items-center gap-0.5 rounded-[6px] px-2 py-0.5 text-[10px] font-medium leading-tight text-[var(--c-text-tertiary)]"
                style={{ background: 'var(--c-bg-input)', border: '0.5px solid var(--c-border-subtle)' }}
              >
                <Github size={9} />
                {skillText.sourceGitHub}
              </span>
            )}
            {item.is_platform && (
              <span
                className="shrink-0 rounded-[6px] px-2 py-0.5 text-[10px] font-medium leading-tight text-[var(--c-text-secondary)]"
                style={{ background: 'var(--c-bg-input)', border: '0.5px solid var(--c-border-subtle)' }}
              >
                {skillText.sourceBuiltin}
              </span>
            )}
            {showCustomProvider && (
              <span
                className="shrink-0 rounded-[6px] px-2 py-0.5 text-[10px] font-medium leading-tight text-[var(--c-text-secondary)]"
                style={{ background: 'var(--c-bg-input)', border: '0.5px solid var(--c-border-subtle)' }}
              >
                {providerDisplay}
              </span>
            )}
            {platformBadgeLabel && platformBadgeStyle && (
              <span
                className="shrink-0 rounded-[6px] px-2 py-0.5 text-[10px] font-medium leading-tight"
                style={platformBadgeStyle}
              >
                {platformBadgeLabel}
              </span>
            )}
            {scanBadge && (
              <span
                className="shrink-0 rounded-[6px] px-2 py-0.5 text-[10px] font-medium leading-tight"
                style={scanBadge.style}
              >
                {scanBadge.label}
              </span>
            )}
          </div>

          <div className="max-h-[min(52vh,480px)] space-y-4 overflow-y-auto pr-0.5">
            <div className="flex flex-col gap-1.5">
              <span className={fieldLabelCls}>{skillText.detailDescription}</span>
              <p className="text-sm leading-relaxed text-[var(--c-text-secondary)]">
                {item.description || skillText.noDescription}
              </p>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className={statCardCls}>
                <span className="text-[10px] font-medium text-[var(--c-text-muted)]">{skillText.detailVersion}</span>
                <span className="text-sm text-[var(--c-text-heading)]">{item.version || '-'}</span>
              </div>
              <div className={statCardCls}>
                <span className="text-[10px] font-medium text-[var(--c-text-muted)]">{skillText.detailSource}</span>
                <span className="text-sm text-[var(--c-text-heading)]">{detailSourceLabel}</span>
              </div>
            </div>

            {item.updated_at && (
              <div className={statCardCls}>
                <span className="text-[10px] font-medium text-[var(--c-text-muted)]">{skillText.detailUpdatedAt}</span>
                <span className="text-sm text-[var(--c-text-heading)]">{formatDate(item.updated_at, locale)}</span>
              </div>
            )}

            {item.scan_summary && (
              <div className={statCardCls}>
                <p className="text-xs leading-relaxed text-[var(--c-text-tertiary)]">{item.scan_summary}</p>
              </div>
            )}
          </div>
        </div>
      </SettingsModalFrame>

      <ConfirmDialog
        open={removeConfirmOpen}
        title={skillText.removeConfirmTitle}
        message={
          item.version
            ? skillText.removeConfirmBody(item.display_name, item.skill_key, item.version)
            : ''
        }
        confirmLabel={skillText.remove}
        cancelLabel={skillText.cancelAction}
        onClose={() => setRemoveConfirmOpen(false)}
        onConfirm={confirmRemove}
      />
    </>
  )
}
