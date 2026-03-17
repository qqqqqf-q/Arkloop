import { useState, useRef, useEffect } from 'react'
import type { LucideIcon } from 'lucide-react'
import { ChevronDown, Monitor, Sun, Moon } from 'lucide-react'
import type { Locale } from '../../locales'
import type { Theme } from '@arkloop/shared/contexts/theme'
import { FontSettings } from './FontSettings'
import { ThemePresetPicker } from './ThemePresetPicker'
import { ThemeColorEditor } from './ThemeColorEditor'

const LOCALE_OPTIONS: { value: Locale; label: string }[] = [
  { value: 'zh', label: '\u4e2d\u6587' },
  { value: 'en', label: 'English' },
]

export function LanguageContent({
  locale,
  setLocale,
  label,
}: {
  locale: Locale
  setLocale: (l: Locale) => void
  label: string
}) {
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const btnRef = useRef<HTMLButtonElement>(null)
  const currentLabel = LOCALE_OPTIONS.find(o => o.value === locale)?.label ?? locale

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

  return (
    <div className="flex flex-col gap-2">
      <span className="text-sm font-medium text-[var(--c-text-heading)]">{label}</span>
      <div className="relative">
        <button
          ref={btnRef}
          type="button"
          onClick={() => setOpen(v => !v)}
          className="flex h-9 w-[240px] items-center justify-between rounded-lg px-3 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
        >
          <span>{currentLabel}</span>
          <ChevronDown size={13} />
        </button>
        {open && (
          <div
            ref={menuRef}
            className="dropdown-menu absolute left-0 top-[calc(100%+4px)] z-50"
            style={{
              border: '0.5px solid var(--c-border-subtle)',
              borderRadius: '10px',
              padding: '4px',
              background: 'var(--c-bg-menu)',
              width: '240px',
              boxShadow: 'var(--c-dropdown-shadow)',
            }}
          >
            {LOCALE_OPTIONS.map(({ value, label: optLabel }) => (
              <button
                key={value}
                type="button"
                onClick={() => { setLocale(value); setOpen(false) }}
                className="flex w-full items-center px-3 py-2 text-sm transition-colors duration-100 bg-[var(--c-bg-menu)] hover:bg-[var(--c-bg-deep)]"
                style={{
                  borderRadius: '8px',
                  fontWeight: locale === value ? 600 : 400,
                  color: locale === value ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
                }}
              >
                {optLabel}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

type ThemeOption = { value: Theme; label: string; icon: LucideIcon }

export function ThemeContent({
  theme,
  setTheme,
  label,
  t,
}: {
  theme: Theme
  setTheme: (t: Theme) => void
  label: string
  t: { themeSystem: string; themeLight: string; themeDark: string }
}) {
  const options: ThemeOption[] = [
    { value: 'system', label: t.themeSystem, icon: Monitor },
    { value: 'light',  label: t.themeLight,  icon: Sun     },
    { value: 'dark',   label: t.themeDark,   icon: Moon    },
  ]

  return (
    <div className="flex flex-col gap-2">
      <span className="text-sm font-medium text-[var(--c-text-heading)]">{label}</span>
      <div
        className="flex w-[240px] rounded-lg p-[3px]"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
      >
        {options.map(({ value, label: optLabel, icon: Icon }) => {
          const active = theme === value
          return (
            <button
              key={value}
              type="button"
              onClick={() => setTheme(value)}
              className="flex flex-1 items-center justify-center gap-1.5 rounded-md py-1.5 text-xs transition-colors duration-100"
              style={{
                background: active ? 'var(--c-bg-deep)' : 'transparent',
                color: active ? 'var(--c-text-heading)' : 'var(--c-text-tertiary)',
                fontWeight: active ? 500 : 400,
              }}
            >
              <Icon size={13} />
              <span>{optLabel}</span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

type AppearanceContentProps = {
  locale: Locale
  setLocale: (l: Locale) => void
  theme: Theme
  setTheme: (t: Theme) => void
  t: {
    language: string
    appearance: string
    themeSystem: string
    themeLight: string
    themeDark: string
    themePresetSection: string
    fontSection: string
  }
}

export function AppearanceContent({ locale, setLocale, theme, setTheme, t }: AppearanceContentProps) {
  const [showEditor, setShowEditor] = useState(false)

  return (
    <div className="flex flex-col gap-6">
      <LanguageContent locale={locale} setLocale={setLocale} label={t.language} />
      <ThemeContent theme={theme} setTheme={setTheme} label={t.appearance} t={t} />
      <ThemePresetPicker onEditColors={() => setShowEditor(v => !v)} />
      {showEditor && (
        <ThemeColorEditor onClose={() => setShowEditor(false)} />
      )}
      <FontSettings />
    </div>
  )
}
