import { type ReactNode, useState } from 'react'
import { Button } from '../Button'
import { PillToggle } from '../PillToggle'
import type { Layer, PromptInjectionTexts } from './types'

export interface LayerCardProps {
  layer: Layer
  enabled: boolean
  toggling: boolean
  texts: PromptInjectionTexts
  semanticConfigured: boolean
  semanticProvider: string
  localModelInstalled: boolean
  semanticCanEnable: boolean
  onToggle: () => void
  onReconfigure: () => void
  onSetupToggle: () => void
  setupPanel?: ReactNode
  variant?: 'card' | 'row'
}

export function LayerCard({
  layer,
  enabled,
  toggling,
  texts,
  semanticConfigured,
  semanticProvider,
  localModelInstalled,
  semanticCanEnable,
  onToggle,
  onReconfigure,
  onSetupToggle,
  setupPanel,
  variant = 'card',
}: LayerCardProps) {
  const isSemantic = layer.id === 'semantic'
  const canToggle = !isSemantic || (semanticConfigured && semanticCanEnable)
  const clickable = !isSemantic && canToggle
  const interactable = clickable && !toggling
  const [cardHovered, setCardHovered] = useState(false)
  const isRow = variant === 'row'

  const badge = () => {
    if (isSemantic) {
      if (!semanticConfigured) return (
        <span className="rounded-md bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-text-muted)]">
          {texts.statusNotConfigured}
        </span>
      )
      if (semanticProvider === 'local' && !localModelInstalled) return (
        <span className="rounded-md bg-[var(--c-status-warning-bg)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-status-warning-text)]">
          {texts.statusPendingInstall}
        </span>
      )
    }
    return null
  }

  const innerClassName = [
    'flex items-center justify-between px-5 py-4 outline-none transition-colors',
    !isRow && 'rounded-lg',
    clickable ? 'cursor-pointer hover:bg-[var(--c-bg-deep)]/25 focus-visible:ring-2 focus-visible:ring-[var(--c-accent)]' : '',
  ].filter(Boolean).join(' ')

  const innerStyle = isRow
    ? undefined
    : { border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }

  const outerClassName = isRow
    ? "relative [&+&]:before:absolute [&+&]:before:left-5 [&+&]:before:right-5 [&+&]:before:top-0 [&+&]:before:h-px [&+&]:before:bg-[var(--c-border-subtle)] [&+&]:before:content-['']"
    : ''

  return (
    <div className={outerClassName}>
      <div
        role={clickable ? 'button' : undefined}
        tabIndex={clickable ? 0 : undefined}
        className={innerClassName}
        style={innerStyle}
        onMouseEnter={() => setCardHovered(true)}
        onMouseLeave={() => setCardHovered(false)}
        onClick={interactable ? onToggle : undefined}
        onKeyDown={interactable ? (e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            onToggle()
          }
        } : undefined}
      >
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-[var(--c-text-primary)]">
              {texts[layer.nameKey]}
            </span>
            {badge()}
            {isSemantic && semanticConfigured && (
              <span className="text-[10px] text-[var(--c-text-muted)]">
                ({semanticProvider === 'api' ? 'API' : 'Local'})
              </span>
            )}
          </div>
          <p className="mt-1 text-xs text-[var(--c-text-muted)]">
            {texts[layer.descKey]}
          </p>
        </div>

        <div className="flex shrink-0 items-center gap-3" onClick={interactable ? (e) => e.stopPropagation() : undefined}>
          {isSemantic && semanticConfigured && (
            <Button variant="ghost" size="sm" onClick={onReconfigure}>
              {texts.actionReconfigure}
            </Button>
          )}
          {isSemantic && !semanticConfigured ? (
            <Button variant="outline" size="sm" onClick={onSetupToggle}>
              {texts.actionConfigure}
            </Button>
          ) : (
            <PillToggle
              checked={enabled}
              onChange={() => onToggle()}
              disabled={!canToggle || toggling}
              forceHover={clickable && cardHovered}
            />
          )}
        </div>
      </div>
      {setupPanel}
    </div>
  )
}
