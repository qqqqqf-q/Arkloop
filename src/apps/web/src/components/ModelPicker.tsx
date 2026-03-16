import { useRef, useEffect, useState, useCallback } from 'react'
import { ChevronDown } from 'lucide-react'
import { listLlmProviders, type LlmProvider } from '../api'
import { useLocale } from '../contexts/LocaleContext'

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
  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [hovered, setHovered] = useState(false)
  const btnRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  const load = useCallback(async () => {
    if (!accessToken) return
    try {
      const list = await listLlmProviders(accessToken)
      setProviders(list)
    } catch {
      // 静默失败，model picker 不影响聊天主流程
    }
  }, [accessToken])

  useEffect(() => {
    void load()
  }, [load])

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

  const hasModels = providers.some((p) => p.models.length > 0)

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
          fontWeight: 450,
          fontSize: '14px',
          background: hovered ? 'var(--c-bg-deep)' : 'transparent',
          color: value ? 'var(--c-text-primary)' : 'var(--c-text-secondary)',
          opacity: hovered ? 1 : 0.85,
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
            minWidth: '200px',
            maxWidth: '260px',
            boxShadow: 'var(--c-dropdown-shadow)',
          }}
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
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

            {hasModels && (
              <>
                <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 4px' }} />
                <div
                  className="px-3 py-1 text-xs"
                  style={{ color: 'var(--c-text-muted)', fontWeight: 500, letterSpacing: '0.02em' }}
                >
                  {mp.byokSection}
                </div>
                {providers.map((provider) =>
                  provider.models.map((m) => {
                    const combo = `${provider.name}^${m.model}`
                    const isSelected = value === combo
                    return (
                      <button
                        key={combo}
                        type="button"
                        onClick={() => handleSelect(combo)}
                        className="flex w-full items-center rounded-lg px-3 py-2 text-sm hover:bg-[var(--c-bg-deep)]"
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
                        <span
                          style={{
                            fontSize: '11px',
                            color: 'var(--c-text-muted)',
                            flexShrink: 0,
                            marginLeft: '8px',
                          }}
                        >
                          {provider.name}
                        </span>
                        {isSelected && (
                          <span style={{ marginLeft: '6px', fontSize: '12px', color: 'var(--c-text-muted)', flexShrink: 0 }}>✓</span>
                        )}
                      </button>
                    )
                  })
                )}
              </>
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
