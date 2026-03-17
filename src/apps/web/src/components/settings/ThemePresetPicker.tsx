import { useAppearance } from '../../contexts/AppearanceContext'
import { BUILTIN_PRESETS } from '../../themes/presets'
import type { ThemePreset, ThemeDefinition } from '../../themes/types'
import { useLocale } from '../../contexts/LocaleContext'
import { Check, Trash2 } from 'lucide-react'

type SwatchVars = Partial<{ '--c-bg-page': string; '--c-bg-sidebar': string; '--c-accent': string; '--c-text-primary': string }>

type PresetCardProps = {
  name: string
  dark: SwatchVars
  active: boolean
  onClick: () => void
}

function PresetCard({ name, dark, active, onClick }: PresetCardProps) {
  // Use dark preview colors for swatch
  const bg = dark['--c-bg-page'] ?? '#1e1d1c'
  const sidebar = dark['--c-bg-sidebar'] ?? '#242422'
  const accent = dark['--c-accent'] ?? '#faf9f6'
  const text = dark['--c-text-primary'] ?? '#faf9f5'

  return (
    <button
      type="button"
      onClick={onClick}
      className="relative flex flex-col overflow-hidden rounded-xl transition-all"
      style={{
        border: `0.5px solid ${active ? 'var(--c-border-mid)' : 'var(--c-border-subtle)'}`,
        outline: active ? '1.5px solid var(--c-accent)' : 'none',
        outlineOffset: '-1px',
        width: '120px',
        background: 'var(--c-bg-page)',
      }}
    >
      {/* Mini preview */}
      <div
        className="flex w-full"
        style={{ height: '60px', background: bg, position: 'relative', overflow: 'hidden' }}
      >
        {/* Sidebar strip */}
        <div style={{ width: '28px', height: '100%', background: sidebar }} />
        {/* Content area */}
        <div className="flex flex-1 flex-col gap-1 p-2">
          <div style={{ height: '5px', width: '80%', background: text, borderRadius: '2px', opacity: 0.7 }} />
          <div style={{ height: '5px', width: '60%', background: text, borderRadius: '2px', opacity: 0.4 }} />
          <div style={{ height: '5px', width: '70%', background: text, borderRadius: '2px', opacity: 0.3 }} />
          <div
            style={{
              marginTop: 'auto',
              height: '8px',
              width: '40px',
              background: accent,
              borderRadius: '3px',
            }}
          />
        </div>
        {active && (
          <div
            className="absolute right-1 top-1 flex items-center justify-center rounded-full"
            style={{ width: '16px', height: '16px', background: accent }}
          >
            <Check size={10} style={{ color: bg }} strokeWidth={3} />
          </div>
        )}
      </div>
      <div
        className="px-2 py-1.5 text-xs truncate"
        style={{
          color: active ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
          fontWeight: active ? 500 : 400,
          borderTop: '0.5px solid var(--c-border-subtle)',
        }}
      >
        {name}
      </div>
    </button>
  )
}

export function ThemePresetPicker({ onEditColors }: { onEditColors: () => void }) {
  const { t } = useLocale()
  const {
    themePreset, setThemePreset,
    customThemeId, setActiveCustomTheme,
    customThemes, deleteCustomTheme,
  } = useAppearance()

  const builtins: { id: ThemePreset; name: string; def: ThemeDefinition | null }[] = [
    { id: 'default',      name: t.themePresetDefault, def: null },
    { id: 'terra',        name: 'Terra',              def: BUILTIN_PRESETS['terra'] },
    { id: 'github',       name: 'GitHub',             def: BUILTIN_PRESETS['github'] },
    { id: 'nord',         name: 'Nord',               def: BUILTIN_PRESETS['nord'] },
    { id: 'catppuccin',   name: 'Catppuccin',         def: BUILTIN_PRESETS['catppuccin'] },
    { id: 'tokyo-night',  name: 'Tokyo Night',        def: BUILTIN_PRESETS['tokyo-night'] },
  ]

  const defaultDark = { '--c-bg-page': '#1e1d1c', '--c-bg-sidebar': '#242422', '--c-accent': '#faf9f6', '--c-text-primary': '#faf9f5' } as const
  const customThemeList = Object.values(customThemes)

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.themePresetSection}</span>
        <button
          type="button"
          onClick={onEditColors}
          className="text-xs text-[var(--c-text-secondary)] hover:text-[var(--c-text-primary)] transition-colors"
        >
          {t.editColors}
        </button>
      </div>

      {/* Built-in presets */}
      <div className="flex flex-wrap gap-3">
        {builtins.map(({ id, name, def }) => {
          const dark = def ? (def.dark as typeof defaultDark) : defaultDark
          return (
            <PresetCard
              key={id}
              name={name}
              dark={dark}
              active={themePreset === id}
              onClick={() => setThemePreset(id)}
            />
          )
        })}
      </div>

      {/* Custom themes */}
      {customThemeList.length > 0 && (
        <div className="flex flex-col gap-2">
          <span className="text-xs text-[var(--c-text-tertiary)]">{t.myThemes}</span>
          <div className="flex flex-wrap gap-3">
            {customThemeList.map((theme) => (
              <div key={theme.id} className="relative group">
                <PresetCard
                  name={theme.name}
                  dark={theme.dark as typeof defaultDark}
                  active={themePreset === 'custom' && customThemeId === theme.id}
                  onClick={() => setActiveCustomTheme(theme.id)}
                />
                <button
                  type="button"
                  onClick={(e) => { e.stopPropagation(); deleteCustomTheme(theme.id) }}
                  className="absolute -right-1.5 -top-1.5 hidden group-hover:flex items-center justify-center rounded-full transition-colors"
                  style={{
                    width: '18px', height: '18px',
                    background: 'var(--c-status-danger-bg)',
                    border: '0.5px solid var(--c-border-subtle)',
                  }}
                >
                  <Trash2 size={10} style={{ color: 'var(--c-status-error-text)' }} />
                </button>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
