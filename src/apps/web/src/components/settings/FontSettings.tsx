import { useState, useEffect, useRef, useCallback } from 'react'
import type { CSSProperties, ReactNode } from 'react'
import { useAppearance } from '../../contexts/AppearanceContext'
import type { FontFamily, CodeFontFamily, FontSize } from '../../themes/types'
import { useLocale } from '../../contexts/LocaleContext'
import { Search, ChevronDown } from 'lucide-react'
import { SettingsSelect } from './_SettingsSelect'

const BODY_FONTS: { value: FontFamily; label: string; fontFamily: string }[] = [
  { value: 'default',     label: 'Default',       fontFamily: "'MiSans Adjusted', system-ui, sans-serif" },
  { value: 'inter',       label: 'Inter',         fontFamily: "'Inter', system-ui, sans-serif" },
  { value: 'system',      label: 'System UI',     fontFamily: "system-ui, sans-serif" },
  { value: 'serif',       label: 'Serif',         fontFamily: "ui-serif, Georgia, serif" },
  { value: 'noto-sans',   label: 'Noto Sans',     fontFamily: "'Noto Sans', sans-serif" },
  { value: 'source-sans', label: 'Source Sans 3', fontFamily: "'Source Sans 3', sans-serif" },
  { value: 'open-dyslexic', label: 'OpenDyslexic',  fontFamily: "'OpenDyslexicRegular', 'OpenDyslexic', sans-serif" },
]

// Each entry has its own font stack so preview renders in the correct font
const CODE_FONTS: { value: CodeFontFamily; label: string; fontFamily: string }[] = [
  { value: 'jetbrains-mono',  label: 'JetBrains Mono',  fontFamily: "'JetBrains Mono', 'Cascadia Code', monospace" },
  { value: 'fira-code',       label: 'Fira Code',        fontFamily: "'Fira Code', monospace" },
  { value: 'cascadia-code',   label: 'Cascadia Code',    fontFamily: "'Cascadia Code', 'Cascadia Mono', Consolas, monospace" },
  { value: 'source-code-pro', label: 'Source Code Pro',  fontFamily: "'Source Code Pro', monospace" },
]

// Popular system fonts as fallback when queryLocalFonts() is unavailable
const FALLBACK_SYSTEM_FONTS = [
  'Arial', 'Arial Black', 'Calibri', 'Candara', 'Comic Sans MS', 'Consolas',
  'Courier New', 'Franklin Gothic Medium', 'Futura', 'Georgia', 'Gill Sans',
  'Helvetica', 'Helvetica Neue', 'Impact', 'Lucida Grande', 'Menlo', 'Monaco',
  'Optima', 'Palatino', 'Segoe UI', 'SF Pro Display', 'SF Pro Text',
  'Tahoma', 'Times New Roman', 'Trebuchet MS', 'Verdana',
]

function SystemFontPicker({
  value,
  onSelect,
}: {
  value: string | null
  onSelect: (font: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [hovered, setHovered] = useState(false)
  const [query, setQuery] = useState('')
  const [fonts, setFonts] = useState<string[]>([])
  const inputRef = useRef<HTMLInputElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  // Load system fonts
  useEffect(() => {
    const load = async () => {
      try {
        const w = window as Window & { queryLocalFonts?: () => Promise<Array<{ family: string }>> }
        if (typeof w.queryLocalFonts === 'function') {
          const raw = await w.queryLocalFonts()
          const families = [...new Set(raw.map(f => f.family))].sort()
          setFonts(families)
        } else {
          setFonts(FALLBACK_SYSTEM_FONTS)
        }
      } catch {
        setFonts(FALLBACK_SYSTEM_FONTS)
      }
    }
    load()
  }, [])

  // Close on outside click
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (!containerRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  const filtered = query.trim()
    ? fonts.filter(f => f.toLowerCase().includes(query.toLowerCase()))
    : fonts

  const handleOpen = useCallback(() => {
    setOpen(true)
    setTimeout(() => inputRef.current?.focus(), 50)
  }, [])

  const handleSelect = useCallback((font: string) => {
    onSelect(font)
    setQuery('')
    setOpen(false)
  }, [onSelect])

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={handleOpen}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        className="flex h-9 items-center justify-between gap-2 rounded-lg px-3 text-sm"
        style={{
          width: '240px',
          border: `0.5px solid ${hovered ? 'var(--c-border-mid)' : 'var(--c-border-subtle)'}`,
          background: hovered ? 'var(--c-bg-deep)' : 'var(--c-bg-page)',
          color: value ? 'var(--c-text-heading)' : 'var(--c-text-tertiary)',
          fontFamily: value ? `'${value}', system-ui, sans-serif` : undefined,
          transition: 'border-color 0.15s, background-color 0.15s',
        }}
      >
        <span className="truncate">{value ?? 'System font...'}</span>
        <ChevronDown size={13} className="shrink-0 text-[var(--c-text-muted)]" />
      </button>

      {open && (
        <div
          className="absolute left-0 top-[calc(100%+4px)] z-50 flex flex-col overflow-hidden rounded-xl"
          style={{
            width: '240px',
            border: '0.5px solid var(--c-border-subtle)',
            background: 'var(--c-bg-menu)',
            boxShadow: 'var(--c-dropdown-shadow)',
          }}
        >
          {/* Search */}
          <div
            className="flex items-center gap-2 px-3 py-2"
            style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
          >
            <Search size={13} className="shrink-0 text-[var(--c-text-muted)]" />
            <input
              ref={inputRef}
              type="text"
              placeholder="Search fonts..."
              value={query}
              onChange={e => setQuery(e.target.value)}
              className="flex-1 bg-transparent text-xs outline-none text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)]"
            />
          </div>

          {/* Font list */}
          <div className="overflow-y-auto" style={{ maxHeight: '240px' }}>
            {filtered.length === 0 ? (
              <div className="px-3 py-2 text-xs text-[var(--c-text-muted)]">No fonts found</div>
            ) : (
              filtered.map(font => (
                <button
                  key={font}
                  type="button"
                  onClick={() => handleSelect(font)}
                  className="flex w-full items-center px-3 py-2 text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
                  style={{
                    fontFamily: `'${font}', system-ui, sans-serif`,
                    color: font === value ? 'var(--c-text-heading)' : 'var(--c-text-primary)',
                    fontWeight: font === value ? 500 : 400,
                  }}
                >
                  {font}
                </button>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  )
}

// 字体/代码字体的选项按钮，统一 active/hover/静止 描边行为
function FontOptionButton({
  active,
  onClick,
  style,
  className,
  children,
}: {
  active: boolean
  onClick: () => void
  style?: CSSProperties
  className?: string
  children: ReactNode
}) {
  const [hovered, setHovered] = useState(false)
  return (
    <button
      type="button"
      onClick={onClick}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      className={className}
      style={{
        border: active
          ? '1.5px solid var(--c-btn-bg)'
          : `1.5px solid ${hovered ? 'var(--c-input-border-color-hover)' : 'var(--c-input-border-color)'}`,
        background: 'var(--c-bg-page)',
        color: active ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
        fontWeight: active ? 500 : 400,
        transition: 'border-color 0.14s ease-out',
        ...style,
      }}
    >
      {children}
    </button>
  )
}

export function FontSettings({ showTitle = true }: { showTitle?: boolean }) {
  const { t } = useLocale()
  const {
    fontFamily, setFontFamily,
    customBodyFont, setCustomBodyFont,
    codeFontFamily, setCodeFontFamily,
    fontSize, setFontSize,
  } = useAppearance()

  const sizes: { value: FontSize; label: string; px: string }[] = [
    { value: 'compact',  label: t.fontSizeCompact, px: '13px' },
    { value: 'normal',   label: t.fontSizeNormal,  px: '14px' },
    { value: 'relaxed',  label: t.fontSizeRelaxed, px: '15px' },
  ]

  return (
    <div className="flex flex-col gap-5">
      {showTitle && (
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.fontSection}</span>
      )}

      {/* Body font — preset buttons */}
      <div className="flex flex-col gap-2">
        <span className="text-xs text-[var(--c-text-secondary)]">{t.fontBody}</span>
        <div className="flex flex-wrap gap-2" style={{ maxWidth: '400px' }}>
          {BODY_FONTS.map(({ value, label, fontFamily: ff }) => {
            const active = fontFamily === value
            return (
              <FontOptionButton
                key={value}
                active={active}
                onClick={() => setFontFamily(value)}
                className="rounded-lg px-3 py-2 text-sm"
                style={{ fontFamily: ff }}
              >
                {label}
              </FontOptionButton>
            )
          })}
        </div>
        {/* System font picker */}
        <SystemFontPicker
          value={fontFamily === 'custom' ? customBodyFont : null}
          onSelect={setCustomBodyFont}
        />
      </div>

      {/* Code font — each button renders in its own font */}
      <div className="flex flex-col gap-2">
        <span className="text-xs text-[var(--c-text-secondary)]">{t.fontCode}</span>
        <div className="grid grid-cols-2 gap-2" style={{ maxWidth: '360px' }}>
          {CODE_FONTS.map(({ value, label, fontFamily: ff }) => {
            const active = codeFontFamily === value
            return (
              <FontOptionButton
                key={value}
                active={active}
                onClick={() => setCodeFontFamily(value)}
                className="rounded-lg px-3 py-2 text-xs"
                style={{ fontFamily: ff }}
              >
                {label}
              </FontOptionButton>
            )
          })}
        </div>
      </div>

      {/* Font size */}
      <div className="flex flex-col gap-2">
        <span className="text-xs text-[var(--c-text-secondary)]">{t.fontSize}</span>
        <div
          className="flex rounded-lg p-[3px]"
          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)', width: '240px' }}
        >
          {sizes.map(({ value, label, px }) => {
            const active = fontSize === value
            return (
              <button
                key={value}
                type="button"
                onClick={() => setFontSize(value)}
                className="flex flex-1 flex-col items-center justify-center rounded-md py-1.5 transition-colors duration-100"
                style={{
                  background: active ? 'var(--c-bg-deep)' : 'transparent',
                  color: active ? 'var(--c-text-heading)' : 'var(--c-text-tertiary)',
                  fontWeight: active ? 500 : 400,
                  gap: '1px',
                }}
              >
                <span style={{ fontSize: px, lineHeight: 1 }}>A</span>
                <span style={{ fontSize: '10px' }}>{label}</span>
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}

function DesktopFontRow({
  title,
  control,
}: {
  title: string
  control: ReactNode
}) {
  return (
    <div className="relative grid items-center gap-3 px-5 py-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:gap-6 [&+&]:before:absolute [&+&]:before:left-5 [&+&]:before:right-5 [&+&]:before:top-0 [&+&]:before:h-px [&+&]:before:bg-[var(--c-border-subtle)] [&+&]:before:content-['']">
      <div className="min-w-0 text-[13px] font-medium text-[var(--c-text-primary)]">{title}</div>
      <div className="flex min-w-0 items-center sm:justify-self-end">{control}</div>
    </div>
  )
}

export function DesktopFontSettings() {
  const { t } = useLocale()
  const {
    fontFamily, setFontFamily,
    customBodyFont, setCustomBodyFont,
    codeFontFamily, setCodeFontFamily,
    fontSize, setFontSize,
  } = useAppearance()

  const bodyOptions = [
    ...BODY_FONTS.map((font) => ({ value: font.value, label: font.label, fontFamily: font.fontFamily })),
    {
      value: 'custom',
      label: customBodyFont || 'Custom system font',
      fontFamily: customBodyFont ? `'${customBodyFont}', system-ui, sans-serif` : undefined,
    },
  ]
  const codeOptions = CODE_FONTS.map((font) => ({
    value: font.value,
    label: font.label,
    fontFamily: font.fontFamily,
  }))
  const sizeOptions: { value: FontSize; label: string }[] = [
    { value: 'compact', label: t.fontSizeCompact },
    { value: 'normal', label: t.fontSizeNormal },
    { value: 'relaxed', label: t.fontSizeRelaxed },
  ]

  return (
    <div className="overflow-hidden rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]">
      <DesktopFontRow
        title={t.fontBody}
        control={(
          <SettingsSelect
            value={fontFamily}
            options={bodyOptions}
            onChange={(value) => setFontFamily(value as FontFamily)}
            fitContent
          />
        )}
      />
      {fontFamily === 'custom' && (
        <div className="border-t border-[var(--c-border-subtle)] px-5 py-4">
          <SystemFontPicker value={customBodyFont} onSelect={setCustomBodyFont} />
        </div>
      )}
      <DesktopFontRow
        title={t.fontCode}
        control={(
          <SettingsSelect
            value={codeFontFamily}
            options={codeOptions}
            onChange={(value) => setCodeFontFamily(value as CodeFontFamily)}
            fitContent
          />
        )}
      />
      <DesktopFontRow
        title={t.fontSize}
        control={(
          <SettingsSelect
            value={fontSize}
            options={sizeOptions}
            onChange={(value) => setFontSize(value as FontSize)}
            fitContent
          />
        )}
      />
    </div>
  )
}
