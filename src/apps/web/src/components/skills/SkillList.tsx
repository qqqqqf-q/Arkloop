import type { KeyboardEvent } from 'react'
import { useCallback, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { motion } from 'framer-motion'
import { ConfirmDialog } from '@arkloop/shared'
import { Download, Github, Loader2, MessageSquare, MoreHorizontal, RefreshCw, Trash2 } from 'lucide-react'
import { openExternal } from '../../openExternal'
import type { ViewSkill } from './types'
import { formatDate, formatSkillRegistryProviderLabel } from './types'
import { DropdownAction } from './DropdownAction'
import { SettingsSwitch } from '../settings/_SettingsSwitch'
import {
  SETTINGS_CARD_INSET_RULE_CLASS,
  SETTINGS_CARD_SURFACE_OVERFLOW_VISIBLE_CLASS,
  SETTINGS_TWO_COLUMN_GRID_CLASS,
} from '../settings/_SettingsLayout'

const SKILL_CARD_MENU_WIDTH = 180
/** 估高用于判断是否翻转到向上展开；实际略大于四行菜单 */
const SKILL_CARD_MENU_EST_HEIGHT = 200

type SkillCardMenuPlacement =
  | { dir: 'down'; left: number; top: number }
  | { dir: 'up'; left: number; bottom: number }

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
    cancelAction: string
    removeConfirmTitle: string
    removeConfirmBody: (displayName: string, skillKey: string, version: string) => string
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

/** 与 SettingsRow 控制列同高，便于与开关垂直对齐 */
const skillCardControlsColClass = 'flex h-7 w-11 shrink-0 items-center justify-end'

function SourceBadge({ children, style, className }: { children: React.ReactNode; style?: React.CSSProperties; className?: string }) {
  return (
    <span
      className={[
        'inline-flex h-5 shrink-0 items-center rounded px-1.5 text-[10px] font-medium leading-none',
        className ?? '',
      ].filter(Boolean).join(' ')}
      style={style}
    >
      {children}
    </span>
  )
}

type SkillListCardProps = {
  item: ViewSkill
  busySkillId: string | null
  menuSkillId: string | null
  setMenuSkillId: (id: string | null) => void
  onDetailSkill: (skill: ViewSkill) => void
  onEnable: (item: ViewSkill) => void
  onDisable: (item: ViewSkill) => void
  onRequestRemoveConfirm: (item: ViewSkill) => void
  onTrySkill?: (prompt: string) => void
  skillText: Props['skillText']
  locale: string
  platformAvailabilityLabel: Props['platformAvailabilityLabel']
  platformAvailabilityStyle: Props['platformAvailabilityStyle']
  scanStatusBadge: Props['scanStatusBadge']
  active: Props['active']
  cardMenuRef: React.RefObject<HTMLDivElement | null>
}

function SkillListCard({
  item,
  busySkillId,
  menuSkillId,
  setMenuSkillId,
  onDetailSkill,
  onEnable,
  onDisable,
  onRequestRemoveConfirm,
  onTrySkill,
  skillText,
  locale,
  platformAvailabilityLabel,
  platformAvailabilityStyle,
  scanStatusBadge,
  active,
  cardMenuRef,
}: SkillListCardProps) {
  const busy = busySkillId === item.id
  const enabled = active(item)
  const platformBadgeLabel = item.is_platform ? platformAvailabilityLabel(item.platform_status) : ''
  const platformBadgeStyle = item.is_platform ? platformAvailabilityStyle(item.platform_status) : null
  const scanBadge = scanStatusBadge(item)
  const providerDisplay = formatSkillRegistryProviderLabel(item.registry_provider, item.source, skillText.sourceOfficial)
  const metaTail = [item.owner_handle ? `@${item.owner_handle}` : '', item.version ? `v${item.version}` : '']
    .filter(Boolean)
    .join(' · ')
  const updatedLine = item.updated_at ? skillText.updatedAt(formatDate(item.updated_at, locale)) : ''
  const showOfficialBadge = item.source === 'official' && Boolean(providerDisplay)
  const showCustomProvider = item.source === 'custom' && Boolean(providerDisplay)
  const menuOpen = menuSkillId === item.id

  const menuWrapRef = useRef<HTMLDivElement | null>(null)
  const menuBtnRef = useRef<HTMLButtonElement | null>(null)
  const [menuPlacement, setMenuPlacement] = useState<SkillCardMenuPlacement | null>(null)

  useLayoutEffect(() => {
    if (!menuOpen) return
    cardMenuRef.current = menuWrapRef.current
    return () => {
      cardMenuRef.current = null
    }
  }, [menuOpen, cardMenuRef])

  const updateMenuPosition = useCallback(() => {
    const btn = menuBtnRef.current
    if (!btn) return
    const r = btn.getBoundingClientRect()
    // 与按钮左缘对齐，向右侧展开（原先右缘对齐会显得朝左下）
    const maxLeft = window.innerWidth - SKILL_CARD_MENU_WIDTH - 8
    const left = Math.min(Math.max(8, r.left), maxLeft)
    const spaceBelow = window.innerHeight - r.bottom - 8
    const spaceAbove = r.top - 8
    const preferDown = spaceBelow >= SKILL_CARD_MENU_EST_HEIGHT || spaceBelow >= spaceAbove
    if (preferDown) {
      setMenuPlacement({ dir: 'down', left, top: r.bottom + 4 })
    } else {
      setMenuPlacement({ dir: 'up', left, bottom: window.innerHeight - r.top + 4 })
    }
  }, [])

  useLayoutEffect(() => {
    if (!menuOpen) {
      setMenuPlacement(null)
      return
    }
    updateMenuPosition()
    window.addEventListener('scroll', updateMenuPosition, true)
    window.addEventListener('resize', updateMenuPosition)
    return () => {
      window.removeEventListener('scroll', updateMenuPosition, true)
      window.removeEventListener('resize', updateMenuPosition)
    }
  }, [menuOpen, updateMenuPosition])

  const handleCardKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Enter' && event.key !== ' ') return
    event.preventDefault()
    onDetailSkill(item)
  }

  const menuPanel =
    menuOpen && menuPlacement && typeof document !== 'undefined'
      ? createPortal(
          <div
            data-skill-card-menu
            className={menuPlacement.dir === 'up' ? 'dropdown-menu-up' : 'dropdown-menu'}
            style={{
              position: 'fixed',
              left: menuPlacement.left,
              width: SKILL_CARD_MENU_WIDTH,
              zIndex: 280,
              ...(menuPlacement.dir === 'down'
                ? { top: menuPlacement.top }
                : { bottom: menuPlacement.bottom }),
              border: '0.5px solid var(--c-border-subtle)',
              borderRadius: '10px',
              padding: '4px',
              background: 'var(--c-bg-menu)',
              boxShadow: 'var(--c-dropdown-shadow)',
            }}
            onMouseDown={(e) => e.stopPropagation()}
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
              onClick={() => {
                setMenuSkillId(null)
                onRequestRemoveConfirm(item)
              }}
            />
          </div>,
          document.body,
        )
      : null

  return (
    <>
      <motion.div
        role="button"
        tabIndex={0}
        onClick={() => onDetailSkill(item)}
        onKeyDown={handleCardKeyDown}
        whileTap={{ scale: 0.98 }}
        transition={{ type: 'spring', stiffness: 620, damping: 22, mass: 0.42 }}
        className={[
          SETTINGS_CARD_SURFACE_OVERFLOW_VISIBLE_CLASS,
          'flex h-full cursor-pointer flex-col outline-none',
          'focus-visible:ring-2 focus-visible:ring-[var(--c-accent)]',
        ].join(' ')}
      >
        <div className="grid items-center gap-2 px-5 pt-3 pb-1.5 sm:grid-cols-[minmax(0,1fr)_auto] sm:gap-6">
          <span className="min-w-0 truncate text-sm font-semibold text-[var(--c-text-heading)]">{item.display_name}</span>
          <div className="flex min-w-0 items-center sm:justify-self-end" onClick={(e) => e.stopPropagation()}>
            <SettingsSwitch
              checked={enabled}
              disabled={busy}
              onChange={() => {
                if (enabled) onDisable(item)
                else onEnable(item)
              }}
            />
          </div>
        </div>

        <div className="flex min-h-0 flex-1 flex-col px-5 pb-2.5">
          <span className="line-clamp-2 text-sm leading-snug text-[var(--c-text-tertiary)]">
            {item.description ?? item.skill_key}
          </span>
          {item.scan_summary ? (
            <span className="mt-1 line-clamp-2 text-[12px] leading-snug text-[var(--c-text-muted)]">{item.scan_summary}</span>
          ) : null}
        </div>

        <div className={SETTINGS_CARD_INSET_RULE_CLASS} aria-hidden />

        <div className="flex items-center gap-3 px-5 py-2.5">
          <div className="flex min-h-5 min-w-0 flex-1 flex-wrap items-center gap-x-1.5 gap-y-1 text-[12px] leading-5 text-[var(--c-text-muted)]">
            {showOfficialBadge ? (
              <SourceBadge style={{ background: 'var(--c-pro-bg)', color: '#6ba3f6' }}>{providerDisplay}</SourceBadge>
            ) : null}
            {showCustomProvider ? (
              <SourceBadge style={{ background: 'var(--c-bg-deep)', color: 'var(--c-text-secondary)' }}>{providerDisplay}</SourceBadge>
            ) : null}
            {item.source === 'github' ? (
              <SourceBadge className="flex items-center gap-0.5 text-[var(--c-text-tertiary)]" style={{ background: 'var(--c-bg-deep)' }}>
                <Github size={9} />
                {skillText.sourceGitHub}
              </SourceBadge>
            ) : null}
            {item.is_platform ? (
              <SourceBadge style={{ background: 'var(--c-bg-deep)', color: 'var(--c-text-secondary)' }}>{skillText.sourceBuiltin}</SourceBadge>
            ) : null}
            {scanBadge ? <SourceBadge style={scanBadge.style}>{scanBadge.label}</SourceBadge> : null}
            {platformBadgeLabel && platformBadgeStyle ? (
              <SourceBadge style={platformBadgeStyle}>{platformBadgeLabel}</SourceBadge>
            ) : null}
            {metaTail ? <span>{metaTail}</span> : null}
            {updatedLine ? (
              <>
                {(showOfficialBadge || showCustomProvider || item.source === 'github' || item.is_platform || scanBadge
                  || (platformBadgeLabel && platformBadgeStyle)
                  || metaTail) ? (
                  <span aria-hidden>·</span>
                ) : null}
                <span className="whitespace-nowrap">{updatedLine}</span>
              </>
            ) : null}
          </div>

          <div ref={menuWrapRef} className={skillCardControlsColClass} onClick={(e) => e.stopPropagation()}>
            <button
              ref={menuBtnRef}
              type="button"
              onClick={() => setMenuSkillId(menuSkillId === item.id ? null : item.id)}
              className="pointer-events-auto flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
            >
              {busy ? <Loader2 size={14} className="animate-spin" /> : <MoreHorizontal size={14} />}
            </button>
          </div>
        </div>
      </motion.div>
      {menuPanel}
    </>
  )
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
  const [removeConfirmItem, setRemoveConfirmItem] = useState<ViewSkill | null>(null)

  if (loading) {
    return (
      <div className={`${SETTINGS_TWO_COLUMN_GRID_CLASS}`}>
        <div className="col-span-full flex h-40 items-center justify-center">
          <Loader2 size={16} className="animate-spin text-[var(--c-text-tertiary)]" />
        </div>
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <div className={`${SETTINGS_TWO_COLUMN_GRID_CLASS}`}>
        <div className="col-span-full flex flex-col items-center justify-center gap-1 rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] px-4 py-12 text-center">
          <span className="text-sm font-medium text-[var(--c-text-primary)]">{skillText.emptyTitle}</span>
          <span className="text-xs text-[var(--c-text-muted)]">
            {viewMode === 'installed' ? skillText.emptyBodyNoMarket : skillText.emptyDesc}
          </span>
        </div>
      </div>
    )
  }

  return (
    <>
      <div className={SETTINGS_TWO_COLUMN_GRID_CLASS}>
        {items.map((item) => (
          <SkillListCard
            key={item.id}
            item={item}
            busySkillId={busySkillId}
            menuSkillId={menuSkillId}
            setMenuSkillId={setMenuSkillId}
            onDetailSkill={onDetailSkill}
            onEnable={onEnable}
            onDisable={onDisable}
            onRequestRemoveConfirm={setRemoveConfirmItem}
            onTrySkill={onTrySkill}
            skillText={skillText}
            locale={locale}
            platformAvailabilityLabel={platformAvailabilityLabel}
            platformAvailabilityStyle={platformAvailabilityStyle}
            scanStatusBadge={scanStatusBadge}
            active={active}
            cardMenuRef={cardMenuRef}
          />
        ))}
      </div>
      <ConfirmDialog
        open={removeConfirmItem != null}
        title={skillText.removeConfirmTitle}
        message={
          removeConfirmItem?.version
            ? skillText.removeConfirmBody(
              removeConfirmItem.display_name,
              removeConfirmItem.skill_key,
              removeConfirmItem.version,
            )
            : ''
        }
        confirmLabel={skillText.remove}
        cancelLabel={skillText.cancelAction}
        onClose={() => setRemoveConfirmItem(null)}
        onConfirm={() => {
          if (removeConfirmItem?.version) onRemove(removeConfirmItem)
          setRemoveConfirmItem(null)
        }}
      />
    </>
  )
}
