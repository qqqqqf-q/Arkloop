import { useRef, useEffect, useState, useCallback, useLayoutEffect } from 'react'
import { ChevronDown, Brain, Check } from 'lucide-react'
import { PillToggle } from '@arkloop/shared'
import { listLlmProviders, type LlmProvider } from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { isDesktop } from '@arkloop/shared/desktop'
import { getAvailableCatalogFromAdvancedJson } from '@arkloop/shared/llm/available-catalog-advanced-json'

const REASONING_LEVELS = ['off', 'minimal', 'low', 'medium', 'high', 'max'] as const
const SUBMENU_INSET_LEFT = 10
const SUBMENU_INSET_RIGHT = 12
const MENU_SECTION_LABEL_STYLE = {
  padding: `6px ${SUBMENU_INSET_RIGHT}px 3px ${SUBMENU_INSET_LEFT}px`,
  fontSize: '11px',
  fontWeight: 500,
  color: 'var(--c-text-muted)',
  letterSpacing: '0.01em',
  userSelect: 'none' as const,
}
const SUBMENU_ROW_STYLE = {
  padding: `6px ${SUBMENU_INSET_RIGHT}px 6px ${SUBMENU_INSET_LEFT}px`,
}

// 模块级缓存：accessToken -> providers，打开时先展示缓存，后台静默刷新
const providersCache = new Map<string, LlmProvider[]>()

function pickFirstChatPickerModel(providers: LlmProvider[]): string | null {
  for (const p of providers) {
    for (const m of p.models) {
      if (m.show_in_picker && !m.tags.includes('embedding')) {
        return `${p.name}^${m.model}`
      }
    }
  }
  return null
}

type Props = {
  accessToken?: string
  value: string | null
  onChange: (model: string | null) => void
  onAddApiKey: () => void
  variant?: 'welcome' | 'chat'
  thinkingEnabled: string
  onThinkingChange: (mode: string) => void
}

export function ModelPicker({ accessToken, value, onChange, onAddApiKey, variant = 'chat', thinkingEnabled, onThinkingChange }: Props) {
  const { t } = useLocale()
  const mp = t.modelPicker
  const desktopShell = isDesktop()
  const [open, setOpen] = useState(false)
  const cached = accessToken ? (providersCache.get(accessToken) ?? null) : null
  const [providers, setProviders] = useState<LlmProvider[]>(cached ?? [])
  const [loading, setLoading] = useState(false)
  const [search, setSearch] = useState('')
  const [hovered, setHovered] = useState(false)
  const [hoveredCombo, setHoveredCombo] = useState<string | null>(null)
  const [optionsFor, setOptionsFor] = useState<string | null>(null)
  const [optionsAnchorY, setOptionsAnchorY] = useState<number | null>(null)
  const [optionsTop, setOptionsTop] = useState<number | null>(null)
  const rootRef = useRef<HTMLDivElement>(null)
  const btnRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const optionsRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)

  const load = useCallback(async (silent: boolean) => {
    if (!accessToken) return
    if (!silent) setLoading(true)
    try {
      const list = await listLlmProviders(accessToken)
      providersCache.set(accessToken, list)
      setProviders(list)
    } catch {
      // 静默失败
    } finally {
      if (!silent) setLoading(false)
    }
  }, [accessToken])

  // 桌面壳：无需点开下拉即可拿到模型列表，供自动选第一条
  useEffect(() => {
    if (!accessToken || !desktopShell) return
    void load(true)
  }, [accessToken, desktopShell, load])

  // 桌面壳：未选手动值时自动落第一条可选模型（不保留「Persona / 默认」空选项语义）
  useEffect(() => {
    if (!desktopShell || value != null || !accessToken) return
    if (loading && providers.length === 0) return
    const first = pickFirstChatPickerModel(providers)
    if (first) onChange(first)
  }, [desktopShell, value, accessToken, loading, providers, onChange])

  // 打开时：有缓存则静默刷新，无缓存则显示 loading
  useEffect(() => {
    if (open) {
      const hasCached = accessToken ? providersCache.has(accessToken) : false
      void load(!hasCached ? false : true)
      setSearch('')
      setTimeout(() => searchRef.current?.focus(), 30)
    } else {
      setOptionsFor(null)
      setOptionsAnchorY(null)
      setOptionsTop(null)
    }
  }, [open, load, accessToken])

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node) ||
        btnRef.current?.contains(e.target as Node) ||
        optionsRef.current?.contains(e.target as Node)
      ) return
      setOpen(false)
      setOptionsFor(null)
      setOptionsAnchorY(null)
      setOptionsTop(null)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  useLayoutEffect(() => {
    if (!optionsFor || optionsAnchorY == null || !optionsRef.current || !menuRef.current || !rootRef.current) return
    const rootRect = rootRef.current.getBoundingClientRect()
    const menuRect = menuRef.current.getBoundingClientRect()
    const optionsRect = optionsRef.current.getBoundingClientRect()
    const minCenterY = menuRect.top - rootRect.top + optionsRect.height / 2
    const nextTop = Math.max(optionsAnchorY, minCenterY)
    if (optionsTop != null && Math.abs(nextTop - optionsTop) < 0.5) return
    setOptionsTop(nextTop)
  }, [open, optionsFor, optionsAnchorY, optionsTop, thinkingEnabled])

  const anyPickerModel = pickFirstChatPickerModel(providers) !== null

  const displayLabel = (() => {
    if (value) {
      const parts = value.split('^')
      const modelName = parts[parts.length - 1]
      if (thinkingEnabled !== 'off') {
        const effortName = thinkingEnabled.charAt(0).toUpperCase() + thinkingEnabled.slice(1)
        return `${modelName} · ${effortName}`
      }
      return modelName
    }
    if (desktopShell) {
      if (anyPickerModel) return '…'
      if (loading && providers.length === 0) return '…'
      return mp.addProviderFirst
    }
    return mp.defaultLabel
  })()

  const handleSelect = (model: string | null) => {
    if (model !== value) onChange(model)
    setOptionsFor(null)
    setOptionsAnchorY(null)
    setOptionsTop(null)
  }

  const q = search.trim().toLowerCase()
  const visibleProviders = providers
    .map((p) => ({
      ...p,
      models: p.models.filter((m) => m.show_in_picker && !m.tags.includes('embedding') && (!q || m.model.toLowerCase().includes(q))),
    }))
    .filter((p) => p.models.length > 0)

  const selectedProviderName = value ? value.split('^')[0] : null
  const selectedModelId = value ? value.split('^').slice(1).join('^') : null

  const sortedProviders = selectedProviderName
    ? [
        ...visibleProviders.filter(p => p.name === selectedProviderName).map(p => ({
          ...p,
          models: selectedModelId
            ? [...p.models.filter(m => m.model === selectedModelId), ...p.models.filter(m => m.model !== selectedModelId)]
            : p.models,
        })),
        ...visibleProviders.filter(p => p.name !== selectedProviderName),
      ]
    : visibleProviders

  const hasModels = sortedProviders.length > 0

  const showWebDefaultRow = !desktopShell

  return (
    <div ref={rootRef} className="relative" style={{ flexShrink: 0 }}>
      <button
        ref={btnRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        className="relative flex h-[33px] items-center gap-1 rounded-lg"
        style={{
          padding: '0 8px 0 10px',
          overflow: 'hidden',
          whiteSpace: 'nowrap',
          cursor: 'pointer',
          fontWeight: 400,
          fontSize: '14px',
          background: hovered ? 'var(--c-bg-deep)' : 'transparent',
          color: hovered && value
            ? 'var(--c-text-primary)'
            : value
              ? 'var(--c-text-secondary)'
              : 'var(--c-text-tertiary)',
          opacity: hovered ? 1 : 0.8,
          transition: 'background-color 120ms ease, color 120ms ease, opacity 120ms ease',
        }}
      >
        <span style={{ maxWidth: '240px', overflow: 'hidden', textOverflow: 'ellipsis', paddingLeft: '1px' }}>
          {displayLabel}
        </span>
        <ChevronDown size={14} style={{ opacity: 0.6, flexShrink: 0 }} />
      </button>

      {open && (
        <>
          <div
            ref={menuRef}
            className={`absolute right-0 z-50 ${variant === 'welcome' ? 'dropdown-menu' : 'dropdown-menu-up'}`}
            style={{
              ...(variant === 'welcome'
                ? { top: 'calc(100% + 8px)' }
                : { bottom: 'calc(100% + 8px)' }),
              border: '0.5px solid var(--c-border-subtle)',
              borderRadius: '10px',
              padding: '4px',
              background: 'var(--c-bg-menu)',
              minWidth: '220px',
              maxWidth: '280px',
              maxHeight: 'min(420px, calc(100vh - 120px))',
              overflowY: 'auto',
              boxShadow: 'var(--c-dropdown-shadow)',
            }}
          >
            <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
              <div style={{ padding: '4px 4px 2px' }}>
                <input
                  ref={searchRef}
                  type="text"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder={mp.searchPlaceholder}
                  className="w-full rounded-md px-3 py-1.5 text-sm outline-none"
                  style={{
                    border: '0.5px solid var(--c-border-subtle)',
                    background: 'var(--c-bg-deep)',
                    color: 'var(--c-text-primary)',
                  }}
                />
              </div>

              {showWebDefaultRow && (
                <button
                  type="button"
                  onClick={() => handleSelect(null)}
                  className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm hover:bg-[var(--c-bg-deep)]"
                  style={{
                    color: value === null ? 'var(--c-text-primary)' : 'var(--c-text-secondary)',
                    fontWeight: value === null ? 600 : 400,
                  }}
                >
                  <span>{mp.defaultLabel}</span>
                  {value === null && (
                    <span style={{ marginLeft: 'auto', display: 'inline-flex', color: 'var(--c-text-muted)' }}>
                      <Check size={13} strokeWidth={1.75} />
                    </span>
                  )}
                </button>
              )}

              {desktopShell && !hasModels && !loading && !search && (
                <button
                  type="button"
                  onClick={() => { setOpen(false); onAddApiKey() }}
                  className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm hover:bg-[var(--c-bg-deep)]"
                  style={{ color: 'var(--c-text-secondary)' }}
                >
                  <span>{mp.addProviderFirst}</span>
                </button>
              )}

              {loading && providers.length === 0 && (
                <p className="px-3 py-2 text-xs" style={{ color: 'var(--c-text-muted)' }}>...</p>
              )}

              {hasModels && (
                <>
                  {(showWebDefaultRow || search) && (
                    <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 4px' }} />
                  )}
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '1px' }}>
                    {sortedProviders.map((provider) => (
                      <div key={provider.id}>
                        <div
                          style={{
                            padding: '6px 12px 3px',
                            fontSize: '11px',
                            fontWeight: 600,
                            color: 'var(--c-text-muted)',
                            letterSpacing: '0.01em',
                            userSelect: 'none',
                          }}
                        >
                          {provider.name}
                        </div>
                        {provider.models.map((m) => {
                          const combo = `${provider.name}^${m.model}`
                          const isSelected = value === combo
                          const supportsReasoning = getAvailableCatalogFromAdvancedJson(m.advanced_json)?.reasoning === true
                          const isOptionsOpen = optionsFor === combo

                          return (
                            <div
                              key={combo}
                              onMouseEnter={() => setHoveredCombo(combo)}
                              onMouseLeave={() => setHoveredCombo(null)}
                              className="flex w-full items-center rounded-lg text-sm hover:bg-[var(--c-bg-deep)]"
                              style={{
                                background: isOptionsOpen ? 'var(--c-bg-deep)' : undefined,
                              }}
                            >
                              <button
                                type="button"
                                onClick={() => handleSelect(combo)}
                                className="flex min-w-0 flex-1 items-center bg-transparent py-[6px] pl-3 pr-2 text-left"
                                style={{
                                  color: isSelected ? 'var(--c-text-primary)' : 'var(--c-text-secondary)',
                                  fontWeight: isSelected ? 600 : 400,
                                }}
                              >
                                <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                  {m.model}
                                </span>
                                {isSelected && thinkingEnabled !== 'off' && (
                                  <span style={{ marginLeft: '6px', display: 'inline-flex', alignItems: 'center', gap: '3px', fontSize: '12px', color: 'var(--c-text-muted)', fontWeight: 400, flexShrink: 0 }}>
                                    <Brain size={12} />
                                    {thinkingEnabled.charAt(0).toUpperCase() + thinkingEnabled.slice(1)}
                                  </span>
                                )}
                              </button>
                              <div className="flex shrink-0 items-center gap-1 pr-3">
                                {supportsReasoning && (hoveredCombo === combo || isOptionsOpen) && (
                                  <span
                                    role="button"
                                    tabIndex={0}
                                    className="text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-primary)]"
                                    onMouseDown={(e) => e.stopPropagation()}
                                    onClick={(e) => {
                                      e.stopPropagation()
                                      if (optionsFor === combo) {
                                        setOptionsFor(null)
                                        setOptionsAnchorY(null)
                                        setOptionsTop(null)
                                        return
                                      }
                                      const triggerRect = e.currentTarget.getBoundingClientRect()
                                      const rootRect = rootRef.current?.getBoundingClientRect()
                                      if (!rootRect) return
                                      setOptionsFor(combo)
                                      setOptionsAnchorY(triggerRect.top - rootRect.top + triggerRect.height / 2)
                                      setOptionsTop(null)
                                    }}
                                    onKeyDown={(e) => {
                                      if (e.key !== 'Enter' && e.key !== ' ') return
                                      e.preventDefault()
                                      e.stopPropagation()
                                      const triggerRect = e.currentTarget.getBoundingClientRect()
                                      const rootRect = rootRef.current?.getBoundingClientRect()
                                      if (!rootRect) return
                                      if (optionsFor === combo) {
                                        setOptionsFor(null)
                                        setOptionsAnchorY(null)
                                        setOptionsTop(null)
                                        return
                                      }
                                      setOptionsFor(combo)
                                      setOptionsAnchorY(triggerRect.top - rootRect.top + triggerRect.height / 2)
                                      setOptionsTop(null)
                                    }}
                                    style={{
                                      fontWeight: 400,
                                      lineHeight: 1,
                                      cursor: 'pointer',
                                    }}
                                  >
                                    {mp.options}
                                  </span>
                                )}
                                {isSelected && (
                                  <Check size={13} strokeWidth={1.75} style={{ color: 'var(--c-text-muted)', flexShrink: 0 }} />
                                )}
                              </div>
                            </div>
                          )
                        })}
                      </div>
                    ))}
                  </div>
                </>
              )}

              {!hasModels && search && (
                <p className="px-3 py-2 text-xs" style={{ color: 'var(--c-text-muted)' }}>{mp.noByok}</p>
              )}

              <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 4px' }} />

              <button
                type="button"
                onClick={() => { setOpen(false); onAddApiKey() }}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm hover:bg-[var(--c-bg-deep)]"
                style={{ color: 'var(--c-text-secondary)' }}
              >
                <span>+ {mp.addApiKey}</span>
              </button>
            </div>
          </div>

          {optionsFor && (() => {
            const optProvider = providers.find(p => optionsFor.startsWith(p.name + '^'))
            const optModelId = optionsFor.split('^').slice(1).join('^')
            const optModel = optProvider?.models.find(m => m.model === optModelId)
            if (!optModel) return null
            const supportsReasoning = getAvailableCatalogFromAdvancedJson(optModel.advanced_json)?.reasoning === true
            if (!supportsReasoning) return null

            const effortLabels: Record<string, string> = {
              off: mp.effortOff,
              minimal: mp.effortMinimal,
              low: mp.effortLow,
              medium: mp.effortMedium,
              high: mp.effortHigh,
              max: mp.effortMax,
            }

            return (
              <div
                ref={optionsRef}
                className="z-50"
                style={{
                  position: 'absolute',
                  left: 'calc(100% + 2px)',
                  top: optionsTop ?? optionsAnchorY ?? 0,
                  transform: 'translateY(-50%)',
                  border: '0.5px solid var(--c-border-subtle)',
                  borderRadius: '10px',
                  padding: '4px',
                  background: 'var(--c-bg-menu)',
                  width: '180px',
                  boxShadow: 'var(--c-dropdown-shadow)',
                }}
              >
                <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
                  <div style={MENU_SECTION_LABEL_STYLE}>
                    {mp.options}
                  </div>
                  <div
                    role="button"
                    tabIndex={0}
                    onClick={() => {
                      if (thinkingEnabled === 'off') onThinkingChange('medium')
                      else onThinkingChange('off')
                    }}
                    onKeyDown={(e) => {
                      if (e.key !== 'Enter' && e.key !== ' ') return
                      e.preventDefault()
                      if (thinkingEnabled === 'off') onThinkingChange('medium')
                      else onThinkingChange('off')
                    }}
                    className="flex w-full cursor-pointer items-center justify-between rounded-lg text-sm hover:bg-[var(--c-bg-deep)]"
                    style={{ ...SUBMENU_ROW_STYLE, color: 'var(--c-text-secondary)' }}
                  >
                    <span>{mp.thinking}</span>
                    <span onClick={(e) => e.stopPropagation()} style={{ lineHeight: 0 }}>
                      <PillToggle
                        checked={thinkingEnabled !== 'off'}
                        onChange={(v) => {
                          if (v) onThinkingChange('medium')
                          else onThinkingChange('off')
                        }}
                        size="sm"
                      />
                    </span>
                  </div>
                  {thinkingEnabled !== 'off' && (
                    <>
                      <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 4px' }} />
                      <div style={MENU_SECTION_LABEL_STYLE}>
                        {mp.effort}
                      </div>
                      {REASONING_LEVELS.filter(l => l !== 'off').map(level => (
                        <button
                          key={level}
                          type="button"
                          onClick={() => onThinkingChange(level)}
                          className="flex w-full items-center rounded-lg text-sm hover:bg-[var(--c-bg-deep)]"
                          style={{
                            ...SUBMENU_ROW_STYLE,
                            color: thinkingEnabled === level ? 'var(--c-text-primary)' : 'var(--c-text-secondary)',
                            fontWeight: thinkingEnabled === level ? 600 : 400,
                          }}
                        >
                          <span style={{ flex: 1, textAlign: 'left' }}>{effortLabels[level]}</span>
                          {thinkingEnabled === level && (
                            <Check size={13} strokeWidth={1.75} style={{ color: 'var(--c-text-muted)', flexShrink: 0 }} />
                          )}
                        </button>
                      ))}
                    </>
                  )}
                </div>
              </div>
            )
          })()}
        </>
      )}
    </div>
  )
}
