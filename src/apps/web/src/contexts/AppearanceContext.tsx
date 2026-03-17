import { createContext, useContext, useEffect, useRef, useState, useCallback } from 'react'
import type { ReactNode } from 'react'
import type { FontFamily, CodeFontFamily, FontSize, ThemePreset, ThemeColorVars, ThemeDefinition } from '../themes/types'
import { BUILTIN_PRESETS } from '../themes/presets'
import {
  readFontSettingsFromStorage,
  writeFontSettingsToStorage,
  readThemePresetFromStorage,
  writeThemePresetToStorage,
  readCustomThemeIdFromStorage,
  writeCustomThemeIdToStorage,
  readCustomThemesFromStorage,
  writeCustomThemesToStorage,
} from '../storage'

// Font stacks
const FONT_STACKS: Record<FontFamily, string> = {
  'inter': "'Inter', system-ui, sans-serif",
  'system': "system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
  'serif': "ui-serif, Georgia, Cambria, 'Times New Roman', Times, serif",
  'noto-sans': "'Noto Sans', system-ui, sans-serif",
  'source-sans': "'Source Sans 3', system-ui, sans-serif",
}

const CODE_FONT_STACKS: Record<CodeFontFamily, string> = {
  'jetbrains-mono': "'JetBrains Mono', 'Cascadia Code', 'Fira Code', monospace",
  'fira-code': "'Fira Code', 'JetBrains Mono', monospace",
  'cascadia-code': "'Cascadia Code', 'Cascadia Mono', 'Consolas', monospace",
  'source-code-pro': "'Source Code Pro', 'JetBrains Mono', monospace",
}

const FONT_SIZE_VALUES: Record<FontSize, string> = {
  compact: '13px',
  normal: '14px',
  relaxed: '15px',
}

// Google Fonts URLs for non-system fonts
const GOOGLE_FONT_URLS: Partial<Record<FontFamily | CodeFontFamily, string>> = {
  'noto-sans': 'https://fonts.googleapis.com/css2?family=Noto+Sans:wght@400;500;600&display=swap',
  'source-sans': 'https://fonts.googleapis.com/css2?family=Source+Sans+3:wght@400;500;600&display=swap',
  'fira-code': 'https://fonts.googleapis.com/css2?family=Fira+Code:wght@400;500&display=swap',
  'source-code-pro': 'https://fonts.googleapis.com/css2?family=Source+Code+Pro:wght@400;500&display=swap',
}

type PreviewVars = { dark: Partial<ThemeColorVars>; light: Partial<ThemeColorVars> } | null

type AppearanceContextValue = {
  fontFamily: FontFamily
  codeFontFamily: CodeFontFamily
  fontSize: FontSize
  setFontFamily: (f: FontFamily) => void
  setCodeFontFamily: (f: CodeFontFamily) => void
  setFontSize: (s: FontSize) => void
  themePreset: ThemePreset
  setThemePreset: (p: ThemePreset) => void
  customThemeId: string | null
  setActiveCustomTheme: (id: string) => void
  customThemes: Record<string, ThemeDefinition>
  saveCustomTheme: (def: ThemeDefinition) => void
  deleteCustomTheme: (id: string) => void
  // Live preview for the color editor
  setPreviewVars: (vars: PreviewVars) => void
  // Resolved active theme vars (for initializing the editor)
  activeThemeVars: { dark: Partial<ThemeColorVars>; light: Partial<ThemeColorVars> }
}

const AppearanceContext = createContext<AppearanceContextValue | null>(null)

function buildStyleContent(
  fontFamily: FontFamily,
  codeFontFamily: CodeFontFamily,
  fontSize: FontSize,
  preset: ThemePreset,
  customThemes: Record<string, ThemeDefinition>,
  customThemeId: string | null,
  previewVars: PreviewVars,
): string {
  const fontStack = FONT_STACKS[fontFamily]
  const codeStack = CODE_FONT_STACKS[codeFontFamily]
  const sizeVal = FONT_SIZE_VALUES[fontSize]

  // Resolve color vars
  let dark: Partial<ThemeColorVars> = {}
  let light: Partial<ThemeColorVars> = {}

  if (previewVars) {
    dark = previewVars.dark
    light = previewVars.light
  } else if (preset !== 'default') {
    const def = preset === 'custom' && customThemeId
      ? customThemes[customThemeId]
      : BUILTIN_PRESETS[preset]
    if (def) {
      dark = def.dark
      light = def.light
    }
  }

  const fontVars = `  --c-font-body: ${fontStack};\n  --c-font-code: ${codeStack};\n  --c-font-size-base: ${sizeVal};`

  const toCssVars = (vars: Partial<ThemeColorVars>) =>
    Object.entries(vars).map(([k, v]) => `  ${k}: ${v};`).join('\n')

  const darkVars = toCssVars(dark)
  const lightVars = toCssVars(light)

  const hasColors = Object.keys(dark).length > 0 || Object.keys(light).length > 0

  if (!hasColors) {
    return `:root {\n${fontVars}\n}`
  }

  const darkBlock = darkVars ? `${fontVars}\n${darkVars}` : fontVars
  const lightBlock = lightVars ? lightVars : ''

  let css = `:root {\n${darkBlock}\n}`

  // Explicit dark mode (data-theme="dark")
  if (darkVars) {
    css += `\n:root[data-theme="dark"] {\n${darkVars}\n}`
  }

  // System light mode
  if (lightBlock) {
    css += `\n@media (prefers-color-scheme: light) {\n  :root:not([data-theme="dark"]) {\n${lightBlock}\n  }\n}`
    css += `\n:root[data-theme="light"] {\n${lightBlock}\n}`
  }

  return css
}

function loadGoogleFont(key: string): void {
  const url = GOOGLE_FONT_URLS[key as FontFamily | CodeFontFamily]
  if (!url) return
  const id = `gf-${key}`
  if (document.getElementById(id)) return
  const link = document.createElement('link')
  link.id = id
  link.rel = 'stylesheet'
  link.href = url
  document.head.appendChild(link)
}

export function AppearanceProvider({ children }: { children: ReactNode }) {
  const fontSettings = readFontSettingsFromStorage()
  const [fontFamily, setFontFamilyState] = useState<FontFamily>(fontSettings.fontFamily)
  const [codeFontFamily, setCodeFontFamilyState] = useState<CodeFontFamily>(fontSettings.codeFontFamily)
  const [fontSize, setFontSizeState] = useState<FontSize>(fontSettings.fontSize)
  const [themePreset, setThemePresetState] = useState<ThemePreset>(readThemePresetFromStorage)
  const [customThemeId, setCustomThemeIdState] = useState<string | null>(readCustomThemeIdFromStorage)
  const [customThemes, setCustomThemesState] = useState<Record<string, ThemeDefinition>>(readCustomThemesFromStorage)
  const [previewVars, setPreviewVarsState] = useState<PreviewVars>(null)
  const styleRef = useRef<HTMLStyleElement | null>(null)

  // Ensure the style element exists
  useEffect(() => {
    let el = document.getElementById('appearance-override') as HTMLStyleElement | null
    if (!el) {
      el = document.createElement('style')
      el.id = 'appearance-override'
      document.head.appendChild(el)
    }
    styleRef.current = el
  }, [])

  // Inject font link tags on mount for non-default fonts
  useEffect(() => {
    if (fontFamily !== 'inter' && fontFamily !== 'system') loadGoogleFont(fontFamily)
    if (codeFontFamily !== 'jetbrains-mono' && codeFontFamily !== 'cascadia-code') loadGoogleFont(codeFontFamily)
  }, [fontFamily, codeFontFamily])

  // Rebuild and inject the style whenever any appearance state changes
  useEffect(() => {
    if (!styleRef.current) return
    styleRef.current.textContent = buildStyleContent(
      fontFamily, codeFontFamily, fontSize,
      themePreset, customThemes, customThemeId,
      previewVars,
    )
  }, [fontFamily, codeFontFamily, fontSize, themePreset, customThemes, customThemeId, previewVars])

  const setFontFamily = useCallback((f: FontFamily) => {
    setFontFamilyState(f)
    if (f !== 'inter' && f !== 'system') loadGoogleFont(f)
    writeFontSettingsToStorage({ fontFamily: f, codeFontFamily, fontSize })
  }, [codeFontFamily, fontSize])

  const setCodeFontFamily = useCallback((f: CodeFontFamily) => {
    setCodeFontFamilyState(f)
    if (f !== 'jetbrains-mono' && f !== 'cascadia-code') loadGoogleFont(f)
    writeFontSettingsToStorage({ fontFamily, codeFontFamily: f, fontSize })
  }, [fontFamily, fontSize])

  const setFontSize = useCallback((s: FontSize) => {
    setFontSizeState(s)
    writeFontSettingsToStorage({ fontFamily, codeFontFamily, fontSize: s })
  }, [fontFamily, codeFontFamily])

  const setThemePreset = useCallback((p: ThemePreset) => {
    setThemePresetState(p)
    writeThemePresetToStorage(p)
  }, [])

  const setActiveCustomTheme = useCallback((id: string) => {
    setCustomThemeIdState(id)
    writeCustomThemeIdToStorage(id)
    setThemePresetState('custom')
    writeThemePresetToStorage('custom')
  }, [])

  const saveCustomTheme = useCallback((def: ThemeDefinition) => {
    setCustomThemesState(prev => {
      const next = { ...prev, [def.id]: def }
      writeCustomThemesToStorage(next)
      return next
    })
  }, [])

  const deleteCustomTheme = useCallback((id: string) => {
    setCustomThemesState(prev => {
      const next = { ...prev }
      delete next[id]
      writeCustomThemesToStorage(next)
      return next
    })
    if (customThemeId === id) {
      setCustomThemeIdState(null)
      writeCustomThemeIdToStorage(null)
      setThemePresetState('default')
      writeThemePresetToStorage('default')
    }
  }, [customThemeId])

  const setPreviewVars = useCallback((vars: PreviewVars) => {
    setPreviewVarsState(vars)
  }, [])

  // Compute current active theme vars (used to initialize the editor)
  const activeThemeVars = (() => {
    if (themePreset === 'default') return { dark: {}, light: {} }
    if (themePreset === 'custom' && customThemeId && customThemes[customThemeId]) {
      return { dark: customThemes[customThemeId].dark, light: customThemes[customThemeId].light }
    }
    const preset = BUILTIN_PRESETS[themePreset]
    if (preset) return { dark: preset.dark, light: preset.light }
    return { dark: {}, light: {} }
  })()

  return (
    <AppearanceContext.Provider value={{
      fontFamily, codeFontFamily, fontSize,
      setFontFamily, setCodeFontFamily, setFontSize,
      themePreset, setThemePreset,
      customThemeId, setActiveCustomTheme,
      customThemes, saveCustomTheme, deleteCustomTheme,
      setPreviewVars,
      activeThemeVars,
    }}>
      {children}
    </AppearanceContext.Provider>
  )
}

export function useAppearance(): AppearanceContextValue {
  const ctx = useContext(AppearanceContext)
  if (!ctx) throw new Error('useAppearance must be used within AppearanceProvider')
  return ctx
}
