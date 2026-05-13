import React from 'react'
import type { ViewSkill } from './types'
import { SkillList } from './SkillList'
import { SettingsGroup } from '../settings/_SettingsLayout'

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
}

type Props = {
  items: ViewSkill[]
  loading: boolean
  marketLoading: boolean
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
}

export function MarketplaceView(props: Props) {
  const {
    items, loading, busySkillId, menuSkillId, setMenuSkillId,
    onDetailSkill, onEnable, onDisable, onRemove, onTrySkill,
    skillText, locale, platformAvailabilityLabel, platformAvailabilityStyle,
    scanStatusBadge, active, cardMenuRef,
  } = props

  return (
    <SettingsGroup title={skillText.searchResults(items.length)}>
      <SkillList
        items={items}
        loading={loading}
        viewMode="marketplace"
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
  )
}
