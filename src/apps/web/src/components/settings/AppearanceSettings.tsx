import React, { useState } from 'react'
import type { LucideIcon } from 'lucide-react'
import { Monitor, Sun, Moon } from 'lucide-react'
import type { Locale } from '../../locales'
import type { Theme } from '@arkloop/shared/contexts/theme'
import { useLocale } from '../../contexts/LocaleContext'
import { useTheme } from '../../contexts/ThemeContext'
import { readGtdEnabled, writeGtdEnabled } from '../../storage'
import { FontSettings } from './FontSettings'
import { ThemePresetPicker } from './ThemePresetPicker'
import { ThemeColorEditor } from './ThemeColorEditor'
import { SettingsSelect } from './_SettingsSelect'

const LOCALE_OPTIONS: { value: Locale; label: string }[] = [
  { value: 'zh', label: '\u4e2d\u6587' },
  { value: 'en', label: 'English' },
]

export function LanguageContent({
  locale,
  setLocale,
  label,
  showLabel = true,
  triggerClassName = 'h-9 w-[240px]',
}: {
  locale: Locale
  setLocale: (l: Locale) => void
  label: string
  showLabel?: boolean
  triggerClassName?: string
}) {
  return (
    <div className="flex flex-col gap-2">
      {showLabel && (
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{label}</span>
      )}
      <SettingsSelect
        value={locale}
        options={LOCALE_OPTIONS}
        onChange={(value) => setLocale(value as Locale)}
        triggerClassName={triggerClassName}
        fitContent
      />
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
    <div className="relative flex h-full w-full" style={{ background: '#F5F5F4' }}>
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

export function ThemeModePicker({ showLabel = true }: { showLabel?: boolean }) {
  const { t } = useLocale()
  const { theme, setTheme } = useTheme()
  const [hoveredValue, setHoveredValue] = useState<Theme | null>(null)

  const options: { value: Theme; label: string; Preview: () => React.JSX.Element }[] = [
    { value: 'light',  label: t.themeLight,  Preview: LightPreview  },
    { value: 'dark',   label: t.themeDark,   Preview: DarkPreview   },
    { value: 'system', label: t.themeSystem, Preview: SystemPreview },
  ]

  return (
    <div className="flex flex-col gap-2">
      {showLabel && (
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.appearance}</span>
      )}
      <div className="flex gap-3">
        {options.map(({ value, label, Preview }) => {
          const active = theme === value
          const hovered = hoveredValue === value
          return (
            <button
              key={value}
              type="button"
              onClick={() => setTheme(value)}
              onMouseEnter={() => setHoveredValue(value)}
              onMouseLeave={() => setHoveredValue(null)}
              className="flex flex-col items-center gap-2"
            >
              <div
                style={{
                  width: 96,
                  height: 64,
                  borderRadius: 10,
                  overflow: 'hidden',
                  border: active
                    ? '1.5px solid var(--c-btn-bg)'
                    : `1.5px solid ${hovered ? 'var(--c-input-border-color-hover)' : 'var(--c-input-border-color)'}`,
                  transition: 'border-color 0.14s ease-out',
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

function NormalPreview() {
  return (
    <div className="flex h-full w-full flex-col p-2" style={{ background: 'var(--c-bg-sidebar)' }}>
      <div style={{ height: 3, width: 22, background: 'var(--c-text-muted)', borderRadius: 2, opacity: 0.45, marginBottom: 6 }} />
      {[70, 52, 64, 48].map((width, index) => (
        <div
          key={width}
          style={{
            height: 8,
            width: `${width}%`,
            background: index === 1 ? 'var(--c-bg-deep)' : 'transparent',
            borderRadius: 4,
            marginBottom: 3,
            padding: '2px 4px',
          }}
        >
          <div style={{ height: 3, width: '100%', background: 'var(--c-border-mid)', borderRadius: 2 }} />
        </div>
      ))}
    </div>
  )
}

function GtdPreview() {
  const groups = [
    { label: 26, rows: [52, 38] },
    { label: 22, rows: [48] },
    { label: 30, rows: [44] },
  ]
  return (
    <div className="flex h-full w-full flex-col gap-[3px] p-2" style={{ background: 'var(--c-bg-sidebar)' }}>
      {groups.map((group, index) => (
        <div key={index} style={{ borderRadius: 5, background: index === 1 ? 'var(--c-bg-deep)' : 'transparent', padding: '2px 3px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 3, marginBottom: 3 }}>
            <div style={{ height: 3, width: group.label, background: 'var(--c-text-muted)', borderRadius: 2, opacity: 0.55 }} />
            <div style={{ width: 0, height: 0, borderTop: '3px solid transparent', borderBottom: '3px solid transparent', borderLeft: '4px solid var(--c-text-muted)', opacity: 0.35 }} />
          </div>
          {group.rows.map((width) => (
            <div key={width} style={{ display: 'flex', alignItems: 'center', gap: 4, paddingLeft: 4, marginBottom: 2 }}>
              <div style={{ width: 4, height: 4, border: '1px solid var(--c-text-muted)', borderRadius: 4, opacity: 0.35 }} />
              <div style={{ height: 3, width, background: 'var(--c-border-mid)', borderRadius: 2 }} />
            </div>
          ))}
        </div>
      ))}
    </div>
  )
}

export function SidebarGroupingPicker({ showLabel = true }: { showLabel?: boolean }) {
  const { t } = useLocale()
  const [gtdEnabled, setGtdEnabled] = useState(() => readGtdEnabled())
  const [hoveredValue, setHoveredValue] = useState<boolean | null>(null)

  const options: { value: boolean; label: string; Preview: () => React.JSX.Element }[] = [
    { value: false, label: t.sidebarGroupingNormal, Preview: NormalPreview },
    { value: true, label: t.sidebarGroupingGtd, Preview: GtdPreview },
  ]

  return (
    <div className="flex flex-col gap-2">
      {showLabel && (
        <>
          <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.sidebarGrouping}</span>
          <span className="text-xs text-[var(--c-text-tertiary)]">{t.sidebarGroupingDesc}</span>
        </>
      )}
      <div className="flex gap-3">
        {options.map(({ value, label, Preview }) => {
          const active = gtdEnabled === value
          const hovered = hoveredValue === value
          return (
            <button
              key={String(value)}
              type="button"
              onMouseEnter={() => setHoveredValue(value)}
              onMouseLeave={() => setHoveredValue(null)}
              onClick={() => {
                if (gtdEnabled === value) return
                setGtdEnabled(value)
                writeGtdEnabled(value)
                window.dispatchEvent(new CustomEvent('arkloop:gtd-enabled-changed', { detail: value }))
                window.dispatchEvent(new StorageEvent('storage', {
                  key: 'arkloop:web:gtd_enabled',
                  newValue: String(value),
                }))
              }}
              className="flex flex-col items-center gap-2"
            >
              <div
                style={{
                  width: 96,
                  height: 64,
                  borderRadius: 10,
                  overflow: 'hidden',
                  border: active
                    ? '1.5px solid var(--c-btn-bg)'
                    : `1.5px solid ${hovered ? 'var(--c-input-border-color-hover)' : 'var(--c-input-border-color)'}`,
                  transition: 'border-color 0.14s ease-out',
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
      <SidebarGroupingPicker />
      <FontSettings />
    </div>
  )
}
