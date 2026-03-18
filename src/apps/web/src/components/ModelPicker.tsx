import { useRef, useEffect, useState, useCallback } from 'react'
import { ChevronDown } from 'lucide-react'
import { listLlmProviders, type LlmProvider } from '../api'
import { useLocale } from '../contexts/LocaleContext'

// 模块级缓存：accessToken -> providers，打开时先展示缓存，后台静默刷新
const providersCache = new Map<string, LlmProvider[]>()

type Props = {
  accessToken?: string
  value: string | null
  onChange: (model: string | null) => void
  onAddApiKey: () => void
  variant?: 'welcome' | 'chat'
}

export function ModelPicker({ accessToken, value, onChange, onAddApiKey, variant = 'chat' }: Props) {
  const { t } = useLocale()
  const mp = t.modelPicker
  const [open, setOpen] = useState(false)
  const cached = accessToken ? (providersCache.get(accessToken) ?? null) : null
  const [providers, setProviders] = useState<LlmProvider[]>(cached ?? [])
  const [loading, setLoading] = useState(false)
  const [search, setSearch] = useState('')
  const [hovered, setHovered] = useState(false)
  const btnRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
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

  // 打开时：有缓存则静默刷新，无缓存则显示 loading
  useEffect(() => {
    if (open) {
      const hasCached = accessToken ? providersCache.has(accessToken) : false
      void load(!hasCached ? false : true)
      setSearch('')
      setTimeout(() => searchRef.current?.focus(), 30)
    }
  }, [open, load, accessToken])

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node) ||
        btnRef.current?.contains(e.target as Node)
      ) return
      setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  // 从 value "credName^model" 中提取 model 部分作为显示名
  const displayLabel = (() => {
    if (!value) return mp.defaultLabel
    const parts = value.split('^')
    return parts[parts.length - 1]
  })()

  const handleSelect = (model: string | null) => {
    onChange(model)
    setOpen(false)
  }

  const q = search.trim().toLowerCase()
  const visibleProviders = providers
    .map((p) => ({
      ...p,
      models: p.models.filter((m) => m.show_in_picker && !m.tags.includes('embedding') && (!q || m.model.toLowerCase().includes(q))),
    }))
    .filter((p) => p.models.length > 0)

  const hasModels = visibleProviders.length > 0

  return (
    <div className="relative" style={{ flexShrink: 0 }}>
      <button
        ref={btnRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        className="relative top-px flex h-8 items-center gap-1 rounded-lg"
        style={{
          padding: '0 8px 0 10px',
          overflow: 'hidden',
          whiteSpace: 'nowrap',
          cursor: 'pointer',
          fontWeight: 400,
          fontSize: '13px',
          background: hovered ? 'var(--c-bg-deep)' : 'transparent',
          color: value ? 'var(--c-text-secondary)' : 'var(--c-text-tertiary)',
          opacity: hovered ? 1 : 0.8,
          transition: 'background-color 60ms ease, color 60ms ease, opacity 60ms ease',
        }}
      >
        <span style={{ maxWidth: '160px', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {displayLabel}
        </span>
        <ChevronDown size={14} style={{ opacity: 0.6, flexShrink: 0 }} />
      </button>

      {open && (
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
            {/* 搜索框 */}
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

            {/* Default 选项 */}
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
                <span style={{ marginLeft: 'auto', fontSize: '12px', color: 'var(--c-text-muted)' }}>✓</span>
              )}
            </button>

            {/* 无缓存时的首次加载态 */}
            {loading && providers.length === 0 && (
              <p className="px-3 py-2 text-xs" style={{ color: 'var(--c-text-muted)' }}>...</p>
            )}

            {/* 可滚动模型列表 */}
            {hasModels && (
              <>
                <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 4px' }} />
                <div style={{ display: 'flex', flexDirection: 'column', gap: '1px' }}>
                  {visibleProviders.map((provider) => (
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
                        return (
                          <button
                            key={combo}
                            type="button"
                            onClick={() => handleSelect(combo)}
                            className="flex w-full items-center rounded-lg px-3 py-[6px] text-sm hover:bg-[var(--c-bg-deep)]"
                            style={{
                              color: isSelected ? 'var(--c-text-primary)' : 'var(--c-text-secondary)',
                              fontWeight: isSelected ? 600 : 400,
                            }}
                          >
                            <span
                              style={{
                                flex: 1,
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                whiteSpace: 'nowrap',
                                textAlign: 'left',
                              }}
                            >
                              {m.model}
                            </span>
                            {isSelected && (
                              <span style={{ marginLeft: '6px', fontSize: '12px', color: 'var(--c-text-muted)', flexShrink: 0 }}>✓</span>
                            )}
                          </button>
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
      )}
    </div>
  )
}
