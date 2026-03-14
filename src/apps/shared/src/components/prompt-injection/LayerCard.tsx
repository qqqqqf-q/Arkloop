import type { ReactNode } from 'react'
import { Loader2 } from 'lucide-react'
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
}: LayerCardProps) {
  const isSemantic = layer.id === 'semantic'
  const canToggle = !isSemantic || (semanticConfigured && semanticCanEnable)

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
    return (
      <span
        className={[
          'rounded-md px-1.5 py-0.5 text-[10px] font-medium',
          enabled
            ? 'bg-[var(--c-status-success-bg)] text-[var(--c-status-success-text)]'
            : 'bg-[var(--c-status-warning-bg)] text-[var(--c-status-warning-text)]',
        ].join(' ')}
      >
        {enabled ? texts.statusEnabled : texts.statusDisabled}
      </span>
    )
  }

  return (
    <div>
      <div className="flex items-center justify-between rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] px-5 py-4">
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

        <div className="flex shrink-0 items-center gap-2">
          {isSemantic && semanticConfigured && (
            <button
              onClick={onReconfigure}
              className="rounded-md px-2 py-1 text-[10px] text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)]"
            >
              {texts.actionReconfigure}
            </button>
          )}
          {isSemantic && !semanticConfigured ? (
            <button
              onClick={onSetupToggle}
              className="rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
            >
              {texts.actionConfigure}
            </button>
          ) : (
            <button
              onClick={onToggle}
              disabled={toggling || !canToggle}
              className={[
                'rounded-md border px-3 py-1.5 text-xs font-medium transition-colors',
                enabled
                  ? 'border-[var(--c-border-console)] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-sub)]'
                  : 'border-[var(--c-status-success-text)] text-[var(--c-status-success-text)] hover:bg-[var(--c-status-success-bg)]',
                (toggling || !canToggle) ? 'opacity-50 cursor-not-allowed' : '',
              ].join(' ')}
            >
              {toggling
                ? <Loader2 size={12} className="inline animate-spin" />
                : enabled ? texts.actionDisable : texts.actionEnable
              }
            </button>
          )}
        </div>
      </div>
      {setupPanel}
    </div>
  )
}
