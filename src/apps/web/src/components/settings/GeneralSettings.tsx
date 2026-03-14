import { Sun, Moon, Monitor, LogOut, HelpCircle } from 'lucide-react'
import type { MeResponse } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { useTheme } from '../../contexts/ThemeContext'
import type { Locale } from '../../locales'
import type { Theme } from '../../storage'

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
  const userInitial = me?.username?.charAt(0).toUpperCase() ?? '?'

  return (
    <div className="flex flex-col gap-8">
      {/* Profile Section */}
      <section>
        <h3 className="mb-4 text-sm font-semibold text-[var(--c-text-heading)]">
          {ds.profileSection}
        </h3>
        <div
          className="flex items-center gap-4 rounded-xl bg-[var(--c-bg-menu)] p-4"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <div
            className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full text-lg font-medium"
            style={{
              background: 'var(--c-avatar-bg)',
              color: 'var(--c-avatar-text)',
            }}
          >
            {userInitial}
          </div>
          <div className="flex min-w-0 flex-1 flex-col">
            <span className="truncate text-base font-semibold text-[var(--c-text-heading)]">
              {me?.username ?? t.loading}
            </span>
            {me?.email && (
              <span className="truncate text-xs text-[var(--c-text-tertiary)]">
                {me.email}
              </span>
            )}
          </div>
        </div>
      </section>

      {/* Appearance Section */}
      <section>
        <h3 className="mb-4 text-sm font-semibold text-[var(--c-text-heading)]">
          {ds.appearanceSection}
        </h3>
        <div className="flex flex-col gap-4">
          {/* Language */}
          <div className="flex items-center justify-between">
            <span className="text-sm text-[var(--c-text-primary)]">
              {t.language}
            </span>
            <div
              className="flex overflow-hidden rounded-lg"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              {(['en', 'zh'] as Locale[]).map((l) => (
                <button
                  key={l}
                  onClick={() => setLocale(l)}
                  className="px-3 py-1.5 text-xs font-medium transition-colors"
                  style={{
                    background:
                      locale === l ? 'var(--c-bg-deep)' : 'transparent',
                    color:
                      locale === l
                        ? 'var(--c-text-heading)'
                        : 'var(--c-text-muted)',
                  }}
                >
                  {l === 'en' ? 'English' : '中文'}
                </button>
              ))}
            </div>
          </div>
          {/* Theme */}
          <div className="flex items-center justify-between">
            <span className="text-sm text-[var(--c-text-primary)]">
              {t.appearance}
            </span>
            <div
              className="flex overflow-hidden rounded-lg"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              {(
                [
                  { key: 'system', icon: Monitor, label: t.themeSystem },
                  { key: 'light', icon: Sun, label: t.themeLight },
                  { key: 'dark', icon: Moon, label: t.themeDark },
                ] as const
              ).map(({ key, icon: Icon, label }) => (
                <button
                  key={key}
                  onClick={() => setTheme(key as Theme)}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium transition-colors"
                  style={{
                    background:
                      theme === key ? 'var(--c-bg-deep)' : 'transparent',
                    color:
                      theme === key
                        ? 'var(--c-text-heading)'
                        : 'var(--c-text-muted)',
                  }}
                >
                  <Icon size={13} />
                  {label}
                </button>
              ))}
            </div>
          </div>
        </div>
      </section>

      {/* Help */}
      <section>
        <div className="flex flex-col gap-2">
          <a
            href="https://arkloop.ai/docs"
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)] hover:text-[var(--c-text-primary)]"
          >
            <HelpCircle size={15} /> {t.getHelp}
          </a>
        </div>
      </section>

      {/* Logout */}
      <section>
        <button
          onClick={onLogout}
          className="flex items-center gap-2 text-sm text-[#ef4444] hover:text-[#f87171]"
        >
          <LogOut size={15} /> {t.logout}
        </button>
      </section>
    </div>
  )
}
