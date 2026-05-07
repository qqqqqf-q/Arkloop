import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown } from 'lucide-react'
import type { CSSProperties } from 'react'
import { getAdaptiveMenuLeft, settingsSelectBorderColor } from './_SettingsSelectUtils'

export type SettingsSelectOption = { value: string; label: string; fontFamily?: string }

type Props = {
  value: string
  options: SettingsSelectOption[]
  onChange: (value: string) => void
  disabled?: boolean
  placeholder?: string
  triggerClassName?: string
  fitContent?: boolean
}

export function SettingsSelect({
  value,
  options,
  onChange,
  disabled,
  placeholder,
  triggerClassName,
  fitContent = false,
}: Props) {
  const [open, setOpen] = useState(false)
  const [highlighted, setHighlighted] = useState(value)
  const [menuStyle, setMenuStyle] = useState<CSSProperties>({})
  const [menuDirection, setMenuDirection] = useState<'up' | 'down'>('down')
  const openDirectionRef = useRef<'up' | 'down'>('down')
  const menuRef = useRef<HTMLDivElement>(null)
  const btnRef = useRef<HTMLButtonElement>(null)

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

  const handleOpen = () => {
    if (disabled) return
    if (!open && btnRef.current) {
      setHighlighted(value)
      const rect = btnRef.current.getBoundingClientRect()
      const viewportWidth = typeof window === 'undefined' ? rect.width + 32 : window.innerWidth
      const viewportHeight = typeof window === 'undefined' ? rect.bottom + 260 : window.innerHeight
      const viewportMargin = 16
      const menuGap = 6
      const longestLabel = Math.max(
        currentLabel.length,
        ...options.map((option) => option.label.length),
      )
      const estimatedMenuWidth = fitContent
        ? Math.min(Math.max(rect.width, longestLabel * 8 + 56), 320)
        : rect.width
      const width = Math.min(estimatedMenuWidth, Math.max(160, viewportWidth - 32))
      const preferredMaxHeight = 220
      const minUsefulHeight = 88
      const estimatedMenuHeight = Math.min(preferredMaxHeight, options.length * 37 + 8)
      const spaceBelow = viewportHeight - rect.bottom - viewportMargin - menuGap
      const spaceAbove = rect.top - viewportMargin - menuGap
      const openAbove = spaceBelow < Math.min(estimatedMenuHeight, 150) && spaceAbove > spaceBelow
      openDirectionRef.current = openAbove ? 'up' : 'down'
      setMenuDirection(openDirectionRef.current)
      const availableHeight = Math.max(minUsefulHeight, openAbove ? spaceAbove : spaceBelow)
      const maxHeight = Math.min(preferredMaxHeight, availableHeight, estimatedMenuHeight)
      setMenuStyle({
        position: 'fixed',
        top: openAbove ? rect.top - menuGap - maxHeight : rect.bottom + menuGap,
        left: getAdaptiveMenuLeft(rect, width, viewportWidth),
        width,
        maxHeight,
        zIndex: 9999,
      })
    }
    setOpen((v) => !v)
  }

  useLayoutEffect(() => {
    if (!open || openDirectionRef.current !== 'up') return
    const button = btnRef.current
    const menu = menuRef.current
    if (!button || !menu) return
    const rect = button.getBoundingClientRect()
    const menuRect = menu.getBoundingClientRect()
    const nextTop = rect.top - 6 - menuRect.height
    if (Math.abs(menuRect.top - nextTop) < 1) return
    setMenuStyle((style) => ({ ...style, top: nextTop }))
  }, [open, options.length])

  const currentOption = options.find((o) => o.value === value)
  const currentLabel = currentOption?.label ?? placeholder ?? value

  const menu = open ? (
    <div
      ref={menuRef}
      className={menuDirection === 'up' ? 'dropdown-menu-up' : 'dropdown-menu'}
      style={{
        ...menuStyle,
        border: `0.65px solid ${settingsSelectBorderColor}`,
        borderRadius: 10,
        padding: '4px',
        background: 'var(--c-bg-menu)',
        boxShadow: 'var(--c-dropdown-shadow)',
        overflowY: 'auto',
      }}
    >
      {options.map((opt) => {
        const selected = opt.value === value
        const active = opt.value === highlighted
        return (
          <button
            key={opt.value}
            type="button"
            onMouseEnter={() => setHighlighted(opt.value)}
            onClick={() => { onChange(opt.value); setOpen(false) }}
            className={[
              'flex w-full items-center justify-between rounded-[6.5px] px-3 py-2 text-sm font-[450] transition-colors duration-[140ms]',
              active
                ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]'
                : 'bg-[var(--c-bg-menu)] text-[var(--c-text-secondary)]',
            ].join(' ')}
          >
            <span className="truncate" style={{ fontFamily: opt.fontFamily }}>{opt.label}</span>
            {selected && <Check size={13} className="ml-2 shrink-0" />}
          </button>
        )
      })}
    </div>
  ) : null

  return (
    <div className="relative">
      <button
        ref={btnRef}
        type="button"
        disabled={disabled}
        onClick={handleOpen}
        className={[
          fitContent ? 'inline-flex max-w-full' : 'flex w-full',
          'h-[32px] items-center justify-between rounded-[6.5px] border-[0.65px] bg-[var(--c-bg-input)] px-3 text-sm font-[450] text-[var(--c-text-primary)] [background-clip:padding-box] transition-colors duration-[180ms] hover:bg-[var(--c-bg-deep)] disabled:cursor-not-allowed disabled:opacity-50',
          triggerClassName,
        ].filter(Boolean).join(' ')}
        style={{ borderColor: settingsSelectBorderColor }}
      >
        <span className="truncate" style={{ fontFamily: currentOption?.fontFamily }}>{currentLabel}</span>
        <ChevronDown size={13} className="ml-2 shrink-0 text-[var(--c-text-muted)]" />
      </button>
      {menu && createPortal(menu, document.body)}
    </div>
  )
}
