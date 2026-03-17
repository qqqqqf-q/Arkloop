import React, { useState, useRef, useEffect } from 'react'
import type { LucideIcon } from 'lucide-react'
import { ChevronDown, Monitor, Sun, Moon } from 'lucide-react'
import type { Locale } from '../../locales'
import type { Theme } from '@arkloop/shared/contexts/theme'
import { useLocale } from '../../contexts/LocaleContext'
import { useTheme } from '../../contexts/ThemeContext'
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

// Mini preview renders ─────────────────────────────────────────────────────

function LightPreview() {
  return (
    <div className="flex h-full w-full" style={{ background: '#F5F5F4' }}>
      <div style={{ width: 22, flexShrink: 0, background: '#EBEBEB' }} />
      <div className="flex flex-1 flex-col gap-1.5 p-2">
        <div style={{ height: 5, width: '70%', background: '#C8C8C6', borderRadius: 2 }} />
        <div style={{ height: 5, width: '55%', background: '#C8C8C6', borderRadius: 2 }} />
        <div style={{ height: 5, width: '65%', background: '#C8C8C6', borderRadius: 2 }} />
        <div style={{ marginTop: 'auto', height: 9, background: '#E0E0DE', borderRadius: 4 }} />
      </div>
    </div>
  )
}

function DarkPreview() {
  return (
    <div className="flex h-full w-full" style={{ background: '#1E1D1C' }}>
      <div style={{ width: 22, flexShrink: 0, background: '#141413' }} />
      <div className="flex flex-1 flex-col gap-1.5 p-2">
        <div style={{ height: 5, width: '70%', background: '#4A4A48', borderRadius: 2 }} />
        <div style={{ height: 5, width: '55%', background: '#4A4A48', borderRadius: 2 }} />
        <div style={{ height: 5, width: '65%', background: '#4A4A48', borderRadius: 2 }} />
        <div style={{ marginTop: 'auto', height: 9, background: '#383836', borderRadius: 4 }} />
      </div>
    </div>
  )
}

function SystemPreview() {
  return (
    <div className="flex h-full w-full overflow-hidden">
      {/* light half */}
      <div className="flex flex-1 overflow-hidden" style={{ background: '#F5F5F4' }}>
        <div style={{ width: 11, flexShrink: 0, background: '#EBEBEB' }} />
        <div className="flex flex-1 flex-col gap-1 p-1.5">
          <div style={{ height: 4, width: '70%', background: '#C8C8C6', borderRadius: 2 }} />
          <div style={{ height: 4, width: '55%', background: '#C8C8C6', borderRadius: 2 }} />
          <div style={{ marginTop: 'auto', height: 7, background: '#E0E0DE', borderRadius: 3 }} />
        </div>
      </div>
      {/* divider */}
      <div style={{ width: 0.5, flexShrink: 0, background: 'rgba(0,0,0,0.15)' }} />
      {/* dark half */}
      <div className="flex flex-1 overflow-hidden" style={{ background: '#1E1D1C' }}>
        <div style={{ width: 11, flexShrink: 0, background: '#141413' }} />
        <div className="flex flex-1 flex-col gap-1 p-1.5">
          <div style={{ height: 4, width: '70%', background: '#4A4A48', borderRadius: 2 }} />
          <div style={{ height: 4, width: '55%', background: '#4A4A48', borderRadius: 2 }} />
          <div style={{ marginTop: 'auto', height: 7, background: '#383836', borderRadius: 3 }} />
        </div>
      </div>
    </div>
  )
}

export function ThemeModePicker() {
  const { t } = useLocale()
  const { theme, setTheme } = useTheme()

  const options: { value: Theme; label: string; Preview: () => React.JSX.Element }[] = [
    { value: 'light',  label: t.themeLight,  Preview: LightPreview  },
    { value: 'dark',   label: t.themeDark,   Preview: DarkPreview   },
    { value: 'system', label: t.themeSystem, Preview: SystemPreview },
  ]

  return (
    <div className="flex flex-col gap-2">
      <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.appearance}</span>
      <div className="flex gap-3">
        {options.map(({ value, label, Preview }) => {
          const active = theme === value
          return (
            <button
              key={value}
              type="button"
              onClick={() => setTheme(value)}
              className="flex flex-col items-center gap-2"
            >
              <div
                style={{
                  width: 96,
                  height: 64,
                  borderRadius: 10,
                  overflow: 'hidden',
                  border: active
                    ? '2px solid #3B82F6'
                    : '1px solid var(--c-border-subtle)',
                  boxShadow: active ? '0 0 0 3px rgba(59,130,246,0.15)' : 'none',
                  transition: 'border-color 0.15s, box-shadow 0.15s',
                  flexShrink: 0,
                }}
              >
                <Preview />
              </div>
              <span
                className="text-xs"
                style={{
                  color: active ? 'var(--c-text-heading)' : 'var(--c-text-tertiary)',
                  fontWeight: active ? 500 : 400,
                }}
              >
                {label}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

export function AppearanceContent() {
  const [showEditor, setShowEditor] = useState(false)

  return (
    <div className="flex flex-col gap-6">
      <ThemeModePicker />
      <ThemePresetPicker onEditColors={() => setShowEditor(v => !v)} />
      {showEditor && (
        <ThemeColorEditor onClose={() => setShowEditor(false)} />
      )}
      <FontSettings />
    </div>
  )
}
