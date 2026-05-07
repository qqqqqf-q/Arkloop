import { createContext, useContext, useEffect, useRef, useState, useCallback } from 'react'
import type { ReactNode } from 'react'
import type { FontFamily, CodeFontFamily, FontSize, ThemePreset, ThemeColorVars, ThemeDefinition, ThemeBackgroundImage } from '../themes/types'
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
  readCustomBodyFontFromStorage,
  writeCustomBodyFontToStorage,
  readBackgroundImageFromStorage,
  writeBackgroundImageToStorage,
  readBackgroundImageOpacityFromStorage,
  writeBackgroundImageOpacityToStorage,
} from '../storage'

// Font stacks
const FONT_STACKS: Record<FontFamily, string> = {
  'default': "'MiSans Adjusted', system-ui, sans-serif",
  'inter': "'Inter', system-ui, sans-serif",
  'system': "system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
  'serif': "ui-serif, Georgia, Cambria, 'Times New Roman', Times, serif",
  'noto-sans': "'Noto Sans', system-ui, sans-serif",
  'source-sans': "'Source Sans 3', system-ui, sans-serif",
  'open-dyslexic': "'OpenDyslexicRegular', 'OpenDyslexic', system-ui, sans-serif",
  'custom': '', // resolved at runtime from customBodyFont
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

// Remote font loading for optional presets that are not bundled locally.
const REMOTE_FONT_URLS: Partial<Record<FontFamily | CodeFontFamily, string>> = {
  'noto-sans': 'https://fonts.googleapis.com/css2?family=Noto+Sans:wght@400;500;600&display=swap',
  'source-sans': 'https://fonts.googleapis.com/css2?family=Source+Sans+3:wght@400;500;600&display=swap',
  'open-dyslexic': 'https://cdn.jsdelivr.net/npm/open-dyslexic@1.0.3/open-dyslexic-regular.css',
  'fira-code': 'https://fonts.googleapis.com/css2?family=Fira+Code:wght@400;500&display=swap',
  'source-code-pro': 'https://fonts.googleapis.com/css2?family=Source+Code+Pro:wght@400;500&display=swap',
}

type PreviewVars = { dark: Partial<ThemeColorVars>; light: Partial<ThemeColorVars> } | null

type AppearanceContextValue = {
  fontFamily: FontFamily
  customBodyFont: string | null
  codeFontFamily: CodeFontFamily
  fontSize: FontSize
  setFontFamily: (f: FontFamily) => void
  setCustomBodyFont: (font: string | null) => void
  setCodeFontFamily: (f: CodeFontFamily) => void
  setFontSize: (s: FontSize) => void
  themePreset: ThemePreset
  setThemePreset: (p: ThemePreset) => void
  customThemeId: string | null
  setActiveCustomTheme: (id: string) => void
  customThemes: Record<string, ThemeDefinition>
  saveCustomTheme: (def: ThemeDefinition) => void
  deleteCustomTheme: (id: string) => void
  backgroundImage: ThemeBackgroundImage | null
  setBackgroundImage: (image: ThemeBackgroundImage | null) => boolean
  backgroundImageOpacity: number
  setBackgroundImageOpacity: (opacity: number) => void
  // Live preview for the color editor
  setPreviewVars: (vars: PreviewVars) => void
  // Resolved active theme vars (for initializing the editor)
  activeThemeVars: { dark: Partial<ThemeColorVars>; light: Partial<ThemeColorVars> }
}

const AppearanceContext = createContext<AppearanceContextValue | null>(null)

function buildStyleContent(
  fontFamily: FontFamily,
  customBodyFont: string | null,
  codeFontFamily: CodeFontFamily,
  fontSize: FontSize,
  preset: ThemePreset,
  customThemes: Record<string, ThemeDefinition>,
  customThemeId: string | null,
  previewVars: PreviewVars,
  backgroundImage: ThemeBackgroundImage | null,
  backgroundImageOpacity: number,
): string {
  const fontStack = fontFamily === 'custom' && customBodyFont
    ? `'${customBodyFont}', system-ui, sans-serif`
    : FONT_STACKS[fontFamily]
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

  const activeBackgroundImage = preset === 'background-image' ? backgroundImage : null
  const backgroundImageVar = activeBackgroundImage
    ? `  --c-background-image: url(${JSON.stringify(activeBackgroundImage.dataUrl)});`
    : '  --c-background-image: none;'
  const normalizedOpacity = Math.min(Math.max(Math.round(backgroundImageOpacity), 0), 100) / 100
  const fontVars = `  --c-font-body: ${fontStack};\n  --c-font-code: ${codeStack};\n  --c-font-size-base: ${sizeVal};\n${backgroundImageVar}\n  --c-background-image-opacity: ${normalizedOpacity};`

  const toCssVars = (vars: Partial<ThemeColorVars>) =>
    Object.entries(vars).map(([k, v]) => `  ${k}: ${v};`).join('\n')

  const darkVars = toCssVars(dark)
  const lightVars = toCssVars(light)

  const hasColors = Object.keys(dark).length > 0 || Object.keys(light).length > 0

  // html font-size scales all rem-based Tailwind utilities
  const htmlSize = `html { font-size: ${sizeVal}; }`

  if (!hasColors) {
    return `${htmlSize}\n:root {\n${fontVars}\n}`
  }

  const darkBlock = darkVars ? `${fontVars}\n${darkVars}` : fontVars
  const lightBlock = lightVars ? lightVars : ''

  let css = `${htmlSize}\n:root {\n${darkBlock}\n}`

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

function loadRemoteFont(key: string): void {
  const url = REMOTE_FONT_URLS[key as FontFamily | CodeFontFamily]
  if (!url) return
  const id = `remote-font-${key}`
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
  const [customBodyFont, setCustomBodyFontState] = useState<string | null>(readCustomBodyFontFromStorage)
  const [codeFontFamily, setCodeFontFamilyState] = useState<CodeFontFamily>(fontSettings.codeFontFamily)
  const [fontSize, setFontSizeState] = useState<FontSize>(fontSettings.fontSize)
  const [themePreset, setThemePresetState] = useState<ThemePreset>(readThemePresetFromStorage)
  const [customThemeId, setCustomThemeIdState] = useState<string | null>(readCustomThemeIdFromStorage)
  const [customThemes, setCustomThemesState] = useState<Record<string, ThemeDefinition>>(readCustomThemesFromStorage)
  const [backgroundImage, setBackgroundImageState] = useState<ThemeBackgroundImage | null>(readBackgroundImageFromStorage)
  const [backgroundImageOpacity, setBackgroundImageOpacityState] = useState(readBackgroundImageOpacityFromStorage)
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

  useEffect(() => {
    document.documentElement.dataset.bodyFont = fontFamily
    return () => {
      delete document.documentElement.dataset.bodyFont
    }
  }, [fontFamily])

  useEffect(() => {
    if (themePreset === 'background-image' && backgroundImage) {
      document.documentElement.dataset.backgroundImage = 'custom'
    } else {
      delete document.documentElement.dataset.backgroundImage
    }
    return () => {
      delete document.documentElement.dataset.backgroundImage
    }
  }, [backgroundImage, themePreset])

  // Inject remote font link tags for presets that are not bundled locally.
  useEffect(() => {
    if (fontFamily !== 'default' && fontFamily !== 'inter' && fontFamily !== 'system' && fontFamily !== 'custom' && fontFamily !== 'serif') loadRemoteFont(fontFamily)
    if (codeFontFamily !== 'jetbrains-mono' && codeFontFamily !== 'cascadia-code') loadRemoteFont(codeFontFamily)
  }, [fontFamily, codeFontFamily])

  // Rebuild and inject the style whenever any appearance state changes
  useEffect(() => {
    if (!styleRef.current) return
    styleRef.current.textContent = buildStyleContent(
      fontFamily, customBodyFont, codeFontFamily, fontSize,
      themePreset, customThemes, customThemeId,
      previewVars,
      backgroundImage,
      backgroundImageOpacity,
    )
  }, [fontFamily, customBodyFont, codeFontFamily, fontSize, themePreset, customThemes, customThemeId, previewVars, backgroundImage, backgroundImageOpacity])

  const setFontFamily = useCallback((f: FontFamily) => {
    setFontFamilyState(f)
    if (f !== 'default' && f !== 'inter' && f !== 'system' && f !== 'custom' && f !== 'serif') loadRemoteFont(f)
    writeFontSettingsToStorage({ fontFamily: f, codeFontFamily, fontSize })
  }, [codeFontFamily, fontSize])

  const setCodeFontFamily = useCallback((f: CodeFontFamily) => {
    setCodeFontFamilyState(f)
    if (f !== 'jetbrains-mono' && f !== 'cascadia-code') loadRemoteFont(f)
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

  const setCustomBodyFont = useCallback((font: string | null) => {
    setCustomBodyFontState(font)
    writeCustomBodyFontToStorage(font)
    if (font) {
      setFontFamilyState('custom')
      writeFontSettingsToStorage({ fontFamily: 'custom', codeFontFamily, fontSize })
    }
  }, [codeFontFamily, fontSize])

  const setBackgroundImage = useCallback((image: ThemeBackgroundImage | null) => {
    const ok = writeBackgroundImageToStorage(image)
    if (!ok) return false
    setBackgroundImageState(image)
    return true
  }, [])

  const setBackgroundImageOpacity = useCallback((opacity: number) => {
    const next = Math.min(Math.max(Math.round(opacity), 0), 100)
    setBackgroundImageOpacityState(next)
    writeBackgroundImageOpacityToStorage(next)
  }, [])

  const setPreviewVars = useCallback((vars: PreviewVars) => {
    setPreviewVarsState(vars)
  }, [])

  // Compute current active theme vars (used to initialize the editor)
  const activeThemeVars = (() => {
    if (themePreset === 'default' || themePreset === 'background-image') return { dark: {}, light: {} }
    if (themePreset === 'custom' && customThemeId && customThemes[customThemeId]) {
      return { dark: customThemes[customThemeId].dark, light: customThemes[customThemeId].light }
    }
    const preset = BUILTIN_PRESETS[themePreset]
    if (preset) return { dark: preset.dark, light: preset.light }
    return { dark: {}, light: {} }
  })()

  return (
    <AppearanceContext.Provider value={{
      fontFamily, customBodyFont, codeFontFamily, fontSize,
      setFontFamily, setCustomBodyFont, setCodeFontFamily, setFontSize,
      themePreset, setThemePreset,
      customThemeId, setActiveCustomTheme,
      customThemes, saveCustomTheme, deleteCustomTheme,
      backgroundImage, setBackgroundImage,
      backgroundImageOpacity, setBackgroundImageOpacity,
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
