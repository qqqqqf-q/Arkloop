import { useState, useCallback, useEffect, useRef } from 'react'
import { useAppearance } from '../../contexts/AppearanceContext'
import { COLOR_GROUPS } from '../../themes/types'
import type { ThemeColorVars, ThemeDefinition } from '../../themes/types'
import { BUILTIN_PRESETS } from '../../themes/presets'
import { useLocale } from '../../contexts/LocaleContext'
import { X, Download, Upload } from 'lucide-react'

type Mode = 'dark' | 'light'


// Try to get a hex-like value from an arbitrary CSS color string for the color input
function toInputColor(value: string): string {
  const trimmed = value.trim()
  // Only pass through hex values — browser color input only accepts hex
  if (/^#[0-9a-fA-F]{3,8}$/.test(trimmed)) return trimmed
  return '#888888'
}

type VarRowProps = {
  varKey: keyof ThemeColorVars
  value: string
  onChange: (key: keyof ThemeColorVars, val: string) => void
}

function VarRow({ varKey, value, onChange }: VarRowProps) {
  const [hexInput, setHexInput] = useState(value)

  useEffect(() => {
    setHexInput(value)
  }, [value])

  const label = varKey.replace('--c-', '').replace(/-/g, ' ')

  const handleHexChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const v = e.target.value
    setHexInput(v)
    if (/^#[0-9a-fA-F]{3,8}$/.test(v)) {
      onChange(varKey, v)
    }
  }

  const handleHexBlur = () => {
    if (/^#[0-9a-fA-F]{3,8}$/.test(hexInput)) {
      onChange(varKey, hexInput)
    } else {
      setHexInput(value) // revert
    }
  }

  const handleColorPicker = (e: React.ChangeEvent<HTMLInputElement>) => {
    const v = e.target.value
    setHexInput(v)
    onChange(varKey, v)
  }

  return (
    <div className="flex items-center gap-3 py-1.5">
      {/* Color swatch + picker */}
      <div className="relative shrink-0" style={{ width: '24px', height: '24px' }}>
        <div
          className="rounded"
          style={{
            width: '24px', height: '24px',
            background: value,
            border: '0.5px solid var(--c-border-subtle)',
          }}
        />
        <input
          type="color"
          value={toInputColor(value)}
          onChange={handleColorPicker}
          className="absolute inset-0 opacity-0 cursor-pointer"
          style={{ width: '24px', height: '24px' }}
          title={label}
        />
      </div>

      {/* Var name */}
      <span
        className="flex-1 text-xs truncate"
        style={{ color: 'var(--c-text-tertiary)', fontFamily: 'var(--c-font-code)', minWidth: 0 }}
      >
        {varKey}
      </span>

      {/* Hex input */}
      <input
        type="text"
        value={hexInput}
        onChange={handleHexChange}
        onBlur={handleHexBlur}
        spellCheck={false}
        className="rounded-md px-2 py-1 text-xs outline-none transition-colors"
        style={{
          width: '90px',
          background: 'var(--c-bg-input)',
          border: '0.5px solid var(--c-border-subtle)',
          color: 'var(--c-text-primary)',
          fontFamily: 'var(--c-font-code)',
        }}
      />
    </div>
  )
}

// Default dark/light base values (mirrors index.css) used when a var is missing from a preset
const DEFAULT_DARK: ThemeColorVars = {
  '--c-bg-page': '#1E1D1C',
  '--c-bg-sidebar': '#242422',
  '--c-bg-deep': '#141413',
  '--c-bg-deep2': '#0f0f0e',
  '--c-bg-sub': '#1e1e1c',
  '--c-bg-card': '#242422',
  '--c-bg-input': '#30302e',
  '--c-bg-menu': '#2a2a28',
  '--c-text-primary': '#faf9f5',
  '--c-text-heading': '#faf9f5',
  '--c-text-secondary': '#c2c0b6',
  '--c-text-tertiary': '#9c9a92',
  '--c-text-muted': '#6b6b68',
  '--c-text-icon': '#7b7970',
  '--c-placeholder': 'rgba(255,255,255,0.42)',
  '--c-border': '#40403d',
  '--c-border-subtle': '#3a3a38',
  '--c-border-mid': '#5e5e5c',
  '--c-border-auth': '#5E5A5A',
  '--c-accent': '#FAF9F6',
  '--c-btn-bg': '#FAF9F6',
  '--c-btn-text': '#141413',
  '--c-accent-send': '#1A1A19',
  '--c-accent-send-hover': '#2e2e2c',
  '--c-status-success-text': '#4ade80',
  '--c-status-error-text': '#f87171',
  '--c-status-warning-text': '#fbbf24',
  '--c-error-bg': 'rgba(69,10,10,0.30)',
  '--c-error-text': '#fca5a5',
  '--c-error-border': 'rgba(127,29,29,0.40)',
  '--c-status-ok-bg': 'rgba(34,197,94,0.10)',
  '--c-status-danger-bg': 'rgba(239,68,68,0.10)',
  '--c-md-code-block-bg': '#2B2B29',
  '--c-md-inline-code-bg': '#242322',
  '--c-md-inline-code-color': '#ED8784',
  '--c-md-table-head-bg': '#242322',
  '--c-avatar-bg': '#c2c0b6',
  '--c-avatar-text': '#242422',
  '--c-accent-fg': '#141413',
  '--c-accent-send-text': '#e8e8e3',
  '--c-bg-plus': '#363633',
  '--c-pro-bg': 'rgba(70,145,246,0.12)',
  '--c-status-ok-text': '#4ade80',
  '--c-status-danger-text': '#f87171',
  '--c-status-warn-bg': 'rgba(245,158,11,0.12)',
  '--c-status-warn-text': '#fbbf24',
  '--c-code-panel-bg': '#141413',
  '--c-code-panel-output-bg': 'rgba(255,255,255,0.04)',
  '--c-lightbox-overlay': 'rgba(255,255,255,0.12)',
  '--c-attachment-bg': '#30302E',
  '--c-attachment-border': 'rgba(255,255,255,0.11)',
  '--c-attachment-border-hover': 'rgba(255,255,255,0.28)',
  '--c-attachment-close-bg': '#303030',
  '--c-attachment-close-border': 'rgba(255,255,255,0.14)',
  '--c-attachment-badge-border': 'rgba(255,255,255,0.14)',
  '--c-scrollbar': '#40403d',
  '--c-scrollbar-hover': '#5e5e5c',
  '--c-bg-card-hover': '#2a2a28',
  '--c-overlay': 'rgba(0,0,0,0.60)',
  '--c-modal-ring': 'rgba(255,255,255,0.07)',
  '--c-sidebar-btn-hover': 'rgba(255,255,255,0.14)',
  '--c-input-border-color': 'rgba(255,255,255,0.06)',
  '--c-input-border-color-hover': 'rgba(255,255,255,0.25)',
  '--c-input-border-color-focus': 'transparent',
  '--c-mode-switch-track': 'rgba(255,255,255,0.06)',
  '--c-mode-switch-border': 'rgba(255,255,255,0.08)',
  '--c-mode-switch-pill': 'rgba(255,255,255,0.10)',
  '--c-mode-switch-active-text': '#faf9f5',
  '--c-mode-switch-inactive-text': '#6b6b68',
  '--c-claw-card-border': 'rgba(255,255,255,0.12)',
  '--c-claw-card-hover': 'rgba(255,255,255,0.04)',
  '--c-claw-step-pending': 'rgba(255,255,255,0.12)',
  '--c-claw-step-line': 'rgba(255,255,255,0.10)',
  '--c-claw-file-bg': 'rgba(255,255,255,0.03)',
  '--c-claw-file-border': 'rgba(255,255,255,0.06)',
}

const DEFAULT_LIGHT: ThemeColorVars = {
  '--c-bg-page': '#F5F5F4',
  '--c-bg-sidebar': '#FAFAFA',
  '--c-bg-deep': '#EBEBEB',
  '--c-bg-deep2': '#E5E5E3',
  '--c-bg-sub': '#EBEBEB',
  '--c-bg-card': '#FFFFFF',
  '--c-bg-input': '#FFFFFF',
  '--c-bg-menu': '#FFFFFF',
  '--c-text-primary': '#141412',
  '--c-text-heading': '#1A1A18',
  '--c-text-secondary': '#3D3D3B',
  '--c-text-tertiary': '#6B6B68',
  '--c-text-muted': '#8C8C8A',
  '--c-text-icon': '#7A7A78',
  '--c-placeholder': 'rgba(0,0,0,0.45)',
  '--c-border': '#B9B9B7',
  '--c-border-subtle': '#DEDEDE',
  '--c-border-mid': '#C0C0BE',
  '--c-border-auth': '#C8C8C8',
  '--c-accent': '#1A1A18',
  '--c-btn-bg': '#1A1A18',
  '--c-btn-text': '#FAFAFA',
  '--c-accent-send': '#FFFFFF',
  '--c-accent-send-hover': '#EBEBEB',
  '--c-status-success-text': '#16a34a',
  '--c-status-error-text': '#dc2626',
  '--c-status-warning-text': '#d97706',
  '--c-error-bg': '#fef2f2',
  '--c-error-text': '#b91c1c',
  '--c-error-border': '#fecaca',
  '--c-status-ok-bg': '#f0fdf4',
  '--c-status-danger-bg': 'rgba(220,38,38,0.08)',
  '--c-md-code-block-bg': '#F6F4F2',
  '--c-md-inline-code-bg': '#F0EEEC',
  '--c-md-inline-code-color': '#7F2C29',
  '--c-md-table-head-bg': '#F8F7F6',
  '--c-avatar-bg': '#3D3D3B',
  '--c-avatar-text': '#FAFAFA',
  '--c-accent-fg': '#FAFAFA',
  '--c-accent-send-text': '#1A1A19',
  '--c-bg-plus': '#E5E5E3',
  '--c-pro-bg': '#EAF2FC',
  '--c-status-ok-text': '#15803d',
  '--c-status-danger-text': '#dc2626',
  '--c-status-warn-bg': '#fff7ed',
  '--c-status-warn-text': '#c2410c',
  '--c-code-panel-bg': '#F6F4F2',
  '--c-code-panel-output-bg': 'rgba(0,0,0,0.04)',
  '--c-lightbox-overlay': 'rgba(255,255,255,0.55)',
  '--c-attachment-bg': '#F0EEEC',
  '--c-attachment-border': 'rgba(0,0,0,0.08)',
  '--c-attachment-border-hover': 'rgba(0,0,0,0.20)',
  '--c-attachment-close-bg': '#E5E5E3',
  '--c-attachment-close-border': 'rgba(0,0,0,0.10)',
  '--c-attachment-badge-border': 'rgba(0,0,0,0.10)',
  '--c-scrollbar': '#B9B9B7',
  '--c-scrollbar-hover': '#9A9A98',
  '--c-bg-card-hover': '#EFEFED',
  '--c-overlay': 'rgba(0,0,0,0.25)',
  '--c-modal-ring': 'rgba(0,0,0,0.09)',
  '--c-sidebar-btn-hover': 'rgba(0,0,0,0.12)',
  '--c-input-border-color': 'rgba(0,0,0,0.16)',
  '--c-input-border-color-hover': 'rgba(0,0,0,0.20)',
  '--c-input-border-color-focus': 'rgba(0,0,0,0.28)',
  '--c-mode-switch-track': 'rgba(0,0,0,0.05)',
  '--c-mode-switch-border': 'rgba(0,0,0,0.08)',
  '--c-mode-switch-pill': '#FFFFFF',
  '--c-mode-switch-active-text': '#141412',
  '--c-mode-switch-inactive-text': '#8C8C8A',
  '--c-claw-card-border': 'rgba(0,0,0,0.10)',
  '--c-claw-card-hover': 'rgba(0,0,0,0.03)',
  '--c-claw-step-pending': 'rgba(0,0,0,0.12)',
  '--c-claw-step-line': 'rgba(0,0,0,0.10)',
  '--c-claw-file-bg': 'rgba(0,0,0,0.02)',
  '--c-claw-file-border': 'rgba(0,0,0,0.05)',
}

function resolveVars(partial: Partial<ThemeColorVars>, base: ThemeColorVars): ThemeColorVars {
  return { ...base, ...partial }
}

type Props = {
  onClose: () => void
}

export function ThemeColorEditor({ onClose }: Props) {
  const { t } = useLocale()
  const {
    themePreset,
    activeThemeVars,
    setPreviewVars,
    saveCustomTheme,
    setActiveCustomTheme,
  } = useAppearance()

  // The editing mode (dark/light)
  const [editMode, setEditMode] = useState<Mode>('dark')
  // Active group tab
  const [activeGroup, setActiveGroup] = useState(COLOR_GROUPS[0].key)

  // Full resolved vars for editing
  const [darkVars, setDarkVars] = useState<ThemeColorVars>(() =>
    resolveVars(activeThemeVars.dark, DEFAULT_DARK)
  )
  const [lightVars, setLightVars] = useState<ThemeColorVars>(() =>
    resolveVars(activeThemeVars.light, DEFAULT_LIGHT)
  )

  // Save-as dialog
  const [showSaveDialog, setShowSaveDialog] = useState(false)
  const [themeName, setThemeName] = useState('')
  const fileInputRef = useRef<HTMLInputElement>(null)

  // Push live preview on every change
  useEffect(() => {
    setPreviewVars({ dark: darkVars, light: lightVars })
    return () => { setPreviewVars(null) }
  }, [darkVars, lightVars, setPreviewVars])

  const handleChange = useCallback((key: keyof ThemeColorVars, val: string) => {
    if (editMode === 'dark') {
      setDarkVars(prev => ({ ...prev, [key]: val }))
    } else {
      setLightVars(prev => ({ ...prev, [key]: val }))
    }
  }, [editMode])

  const handleReset = useCallback(() => {
    const preset = themePreset !== 'custom' && themePreset !== 'default' ? BUILTIN_PRESETS[themePreset] : null
    setDarkVars(preset ? resolveVars(preset.dark, DEFAULT_DARK) : { ...DEFAULT_DARK })
    setLightVars(preset ? resolveVars(preset.light, DEFAULT_LIGHT) : { ...DEFAULT_LIGHT })
  }, [themePreset])

  const handleSave = useCallback(() => {
    const name = themeName.trim()
    if (!name) return
    const id = `custom-${Date.now()}`
    const def: ThemeDefinition = { id, name, dark: darkVars, light: lightVars }
    saveCustomTheme(def)
    setActiveCustomTheme(id)
    setThemeName('')
    setShowSaveDialog(false)
    onClose()
  }, [themeName, darkVars, lightVars, saveCustomTheme, setActiveCustomTheme, onClose])

  const handleExport = useCallback(() => {
    const def: ThemeDefinition = {
      id: `custom-export-${Date.now()}`,
      name: themeName.trim() || 'custom',
      dark: darkVars,
      light: lightVars,
    }
    const json = JSON.stringify(def, null, 2)
    const blob = new Blob([json], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${def.name.replace(/\s+/g, '-')}.json`
    a.click()
    URL.revokeObjectURL(url)
  }, [darkVars, lightVars, themeName])

  const handleImport = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = (ev) => {
      try {
        const def = JSON.parse(ev.target?.result as string) as ThemeDefinition
        if (def.dark && def.light) {
          setDarkVars(resolveVars(def.dark, DEFAULT_DARK))
          setLightVars(resolveVars(def.light, DEFAULT_LIGHT))
        }
      } catch {
        // silently ignore invalid JSON
      }
    }
    reader.readAsText(file)
    e.target.value = ''
  }, [])

  const currentVars = editMode === 'dark' ? darkVars : lightVars
  const activeGroupDef = COLOR_GROUPS.find(g => g.key === activeGroup) ?? COLOR_GROUPS[0]

  const groupLabels: Record<string, string> = {
    backgrounds: t.colorGroupBackgrounds,
    text: t.colorGroupText,
    borders: t.colorGroupBorders,
    accent: t.colorGroupAccent,
    status: t.colorGroupStatus,
    code: t.colorGroupCode,
    interactive: t.colorGroupInteractive,
    input: t.colorGroupInput,
    claw: t.colorGroupClaw,
  }

  return (
    <div
      className="flex flex-col rounded-xl overflow-hidden"
      style={{
        border: '0.5px solid var(--c-border-subtle)',
        background: 'var(--c-bg-sidebar)',
      }}
    >
      {/* Header */}
      <div
        className="flex items-center justify-between px-4 py-3"
        style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
      >
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.themeColorEditor}</span>
        <div className="flex items-center gap-2">
          {/* Dark / Light mode toggle */}
          <div
            className="flex rounded-md p-[2px]"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
          >
            {(['dark', 'light'] as Mode[]).map(m => (
              <button
                key={m}
                type="button"
                onClick={() => setEditMode(m)}
                className="px-2.5 py-1 text-xs rounded transition-colors duration-100"
                style={{
                  background: editMode === m ? 'var(--c-bg-deep)' : 'transparent',
                  color: editMode === m ? 'var(--c-text-heading)' : 'var(--c-text-tertiary)',
                  fontWeight: editMode === m ? 500 : 400,
                }}
              >
                {m === 'dark' ? t.themeDark : t.themeLight}
              </button>
            ))}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="flex h-6 w-6 items-center justify-center rounded text-[var(--c-text-tertiary)] hover:text-[var(--c-text-primary)] transition-colors"
          >
            <X size={14} />
          </button>
        </div>
      </div>

      {/* Group tabs */}
      <div
        className="flex gap-1 overflow-x-auto px-4 py-2"
        style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
      >
        {COLOR_GROUPS.map(({ key }) => (
          <button
            key={key}
            type="button"
            onClick={() => setActiveGroup(key)}
            className="shrink-0 rounded-md px-2.5 py-1 text-xs transition-colors duration-100"
            style={{
              background: activeGroup === key ? 'var(--c-bg-deep)' : 'transparent',
              color: activeGroup === key ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
              fontWeight: activeGroup === key ? 500 : 400,
            }}
          >
            {groupLabels[key]}
          </button>
        ))}
      </div>

      {/* Variable list */}
      <div className="flex-1 overflow-y-auto px-4 py-2" style={{ maxHeight: '280px' }}>
        {activeGroupDef.vars.map((key) => (
          <VarRow
            key={key}
            varKey={key}
            value={currentVars[key] ?? '#888888'}
            onChange={handleChange}
          />
        ))}
      </div>

      {/* Save-as dialog */}
      {showSaveDialog && (
        <div
          className="flex items-center gap-2 px-4 py-2"
          style={{ borderTop: '0.5px solid var(--c-border-subtle)' }}
        >
          <input
            type="text"
            placeholder={t.customThemeNamePlaceholder}
            value={themeName}
            onChange={e => setThemeName(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') handleSave(); if (e.key === 'Escape') setShowSaveDialog(false) }}
            autoFocus
            className="flex-1 rounded-lg px-3 py-1.5 text-xs outline-none"
            style={{
              background: 'var(--c-bg-input)',
              border: '0.5px solid var(--c-border-subtle)',
              color: 'var(--c-text-primary)',
            }}
          />
          <button
            type="button"
            onClick={handleSave}
            className="rounded-lg px-3 py-1.5 text-xs transition-colors"
            style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
          >
            {t.saveAsCustom.replace('...', '')}
          </button>
          <button
            type="button"
            onClick={() => setShowSaveDialog(false)}
            className="text-xs text-[var(--c-text-tertiary)] hover:text-[var(--c-text-primary)] transition-colors"
          >
            <X size={14} />
          </button>
        </div>
      )}

      {/* Footer actions */}
      {!showSaveDialog && (
        <div
          className="flex items-center justify-between px-4 py-3"
          style={{ borderTop: '0.5px solid var(--c-border-subtle)' }}
        >
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={handleReset}
              className="text-xs text-[var(--c-text-tertiary)] hover:text-[var(--c-text-primary)] transition-colors"
            >
              {t.resetToDefault}
            </button>
          </div>
          <div className="flex items-center gap-2">
            {/* Import */}
            <button
              type="button"
              onClick={() => fileInputRef.current?.click()}
              className="flex items-center gap-1 text-xs text-[var(--c-text-secondary)] hover:text-[var(--c-text-primary)] transition-colors"
            >
              <Upload size={12} />
              {t.importTheme}
            </button>
            <input ref={fileInputRef} type="file" accept=".json" className="hidden" onChange={handleImport} />
            {/* Export */}
            <button
              type="button"
              onClick={handleExport}
              className="flex items-center gap-1 text-xs text-[var(--c-text-secondary)] hover:text-[var(--c-text-primary)] transition-colors"
            >
              <Download size={12} />
              {t.exportTheme}
            </button>
            {/* Save As */}
            <button
              type="button"
              onClick={() => setShowSaveDialog(true)}
              className="rounded-lg px-3 py-1.5 text-xs transition-colors"
              style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
            >
              {t.saveAsCustom}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
