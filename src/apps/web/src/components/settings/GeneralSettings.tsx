import { useEffect, useState } from 'react'
import { Monitor, LogOut, HelpCircle } from 'lucide-react'
import type { MeResponse } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { useTheme } from '../../contexts/ThemeContext'
import { isLocalMode, getDesktopApi } from '@arkloop/shared/desktop'
import { LanguageContent, ThemeContent } from './AppearanceSettings'

type Props = {
  me: MeResponse | null
  accessToken: string
  onLogout: () => void
  onMeUpdated?: (me: MeResponse) => void
}

export function GeneralSettings({ me, accessToken: _accessToken, onLogout, onMeUpdated: _onMeUpdated }: Props) {
  const { t, locale, setLocale } = useLocale()
  const { theme, setTheme } = useTheme()
  const ds = t.desktopSettings
  const localMode = isLocalMode()

  const [osUsername, setOsUsername] = useState<string | null>(null)
  useEffect(() => {
    if (!localMode) return
    getDesktopApi()?.app.getOsUsername?.().then(setOsUsername).catch(() => {})
  }, [localMode])

  const displayName = localMode ? (osUsername ?? me?.username ?? '?') : (me?.username ?? '?')
  const userInitial = displayName.charAt(0).toUpperCase()

  return (
    <div className="flex flex-col gap-6">
      {/* Profile */}
      <div
        className="flex items-center gap-3 rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <div
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-sm font-semibold"
          style={{ background: 'var(--c-avatar-bg)', color: 'var(--c-avatar-text)' }}
        >
          {userInitial}
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-sm font-semibold text-[var(--c-text-heading)]">
            {displayName === '?' ? t.loading : displayName}
          </span>
          {localMode ? (
            <span className="flex items-center gap-1 text-xs text-[var(--c-text-tertiary)]">
              <Monitor size={11} />
              {ds.localModeLabel ?? 'Local'}
            </span>
          ) : me?.email ? (
            <span className="truncate text-xs text-[var(--c-text-tertiary)]">{me.email}</span>
          ) : null}
        </div>
      </div>

      {/* Appearance */}
      <section>
        <p className="mb-2 px-1 text-[11px] font-semibold uppercase tracking-wider text-[var(--c-text-muted)]">
          {ds.appearanceSection}
        </p>
        <div className="flex flex-col gap-4">
          <LanguageContent locale={locale} setLocale={setLocale} label={t.language} />
          <ThemeContent theme={theme} setTheme={setTheme} label={t.appearance} t={t} />
        </div>
      </section>

      {/* Footer */}
      <div className="flex flex-col gap-1.5">
        <a
          href="https://arkloop.ai/docs"
          target="_blank"
          rel="noopener noreferrer"
          className="flex w-fit items-center gap-1.5 rounded-lg px-1 py-1 text-sm text-[var(--c-text-secondary)] hover:text-[var(--c-text-primary)]"
        >
          <HelpCircle size={14} /> {t.getHelp}
        </a>
        {!isLocalMode() && (
          <button
            onClick={onLogout}
            className="flex w-fit items-center gap-1.5 rounded-lg px-1 py-1 text-sm text-[#ef4444] hover:text-[#f87171]"
          >
            <LogOut size={14} /> {t.logout}
          </button>
        )}
      </div>
    </div>
  )
}
