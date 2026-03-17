import { useAppearance } from '../../contexts/AppearanceContext'
import type { FontFamily, CodeFontFamily, FontSize } from '../../themes/types'
import { useLocale } from '../../contexts/LocaleContext'

const BODY_FONTS: { value: FontFamily; label: string; fontFamily: string }[] = [
  { value: 'inter',       label: 'Inter',         fontFamily: "'Inter', system-ui, sans-serif" },
  { value: 'system',      label: 'System UI',     fontFamily: "system-ui, sans-serif" },
  { value: 'serif',       label: 'Serif',         fontFamily: "ui-serif, Georgia, serif" },
  { value: 'noto-sans',   label: 'Noto Sans',     fontFamily: "'Noto Sans', sans-serif" },
  { value: 'source-sans', label: 'Source Sans 3', fontFamily: "'Source Sans 3', sans-serif" },
]

const CODE_FONTS: { value: CodeFontFamily; label: string }[] = [
  { value: 'jetbrains-mono',   label: 'JetBrains Mono' },
  { value: 'fira-code',        label: 'Fira Code' },
  { value: 'cascadia-code',    label: 'Cascadia Code' },
  { value: 'source-code-pro',  label: 'Source Code Pro' },
]

export function FontSettings() {
  const { t } = useLocale()
  const { fontFamily, setFontFamily, codeFontFamily, setCodeFontFamily, fontSize, setFontSize } = useAppearance()

  const sizes: { value: FontSize; label: string; px: string }[] = [
    { value: 'compact',  label: t.fontSizeCompact, px: '13px' },
    { value: 'normal',   label: t.fontSizeNormal,  px: '14px' },
    { value: 'relaxed',  label: t.fontSizeRelaxed, px: '15px' },
  ]

  return (
    <div className="flex flex-col gap-5">
      <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.fontSection}</span>

      {/* Body font */}
      <div className="flex flex-col gap-2">
        <span className="text-xs text-[var(--c-text-secondary)]">{t.fontBody}</span>
        <div className="flex flex-wrap gap-2" style={{ maxWidth: '380px' }}>
          {BODY_FONTS.map(({ value, label, fontFamily: ff }) => (
            <button
              key={value}
              type="button"
              onClick={() => setFontFamily(value)}
              className="rounded-lg px-3 py-2 text-sm transition-colors"
              style={{
                border: `0.5px solid ${fontFamily === value ? 'var(--c-border-mid)' : 'var(--c-border-subtle)'}`,
                background: fontFamily === value ? 'var(--c-bg-deep)' : 'var(--c-bg-page)',
                color: fontFamily === value ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
                fontWeight: fontFamily === value ? 500 : 400,
                fontFamily: ff,
              }}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      {/* Code font */}
      <div className="flex flex-col gap-2">
        <span className="text-xs text-[var(--c-text-secondary)]">{t.fontCode}</span>
        <div className="grid grid-cols-2 gap-2" style={{ maxWidth: '360px' }}>
          {CODE_FONTS.map(({ value, label }) => (
            <button
              key={value}
              type="button"
              onClick={() => setCodeFontFamily(value)}
              className="flex items-center gap-2 rounded-lg px-3 py-2 text-xs transition-colors"
              style={{
                border: `0.5px solid ${codeFontFamily === value ? 'var(--c-border-mid)' : 'var(--c-border-subtle)'}`,
                background: codeFontFamily === value ? 'var(--c-bg-deep)' : 'var(--c-bg-page)',
                color: codeFontFamily === value ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
                fontWeight: codeFontFamily === value ? 500 : 400,
                fontFamily: 'var(--c-font-code)',
              }}
            >
              {label}
            </button>
          ))}
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
