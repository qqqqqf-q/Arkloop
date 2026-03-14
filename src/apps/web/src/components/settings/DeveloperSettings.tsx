import { useState, useEffect } from 'react'
import { getDesktopApi } from '@arkloop/shared/desktop'
import { useLocale } from '../../contexts/LocaleContext'

export function DeveloperSettings() {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const [appVersion, setAppVersion] = useState('')
  const [resetDone, setResetDone] = useState(false)

  useEffect(() => {
    const api = getDesktopApi()
    if (api) {
      api.app.getVersion().then(setAppVersion).catch(() => {})
    }
  }, [])

  const handleResetOnboarding = async () => {
    const api = getDesktopApi()
    if (!api) return
    try {
      const config = await api.config.get()
      await api.config.set({ ...config, onboarding_completed: false })
      setResetDone(true)
      setTimeout(() => setResetDone(false), 3000)
    } catch {
      /* ignore */
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h3 className="text-base font-semibold text-[var(--c-text-heading)]">
          {ds.developerTitle}
        </h3>
        <p className="mt-1 text-sm text-[var(--c-text-secondary)]">
          {ds.developerDesc}
        </p>
      </div>

      <div className="flex flex-col gap-4">
        {/* Reset onboarding */}
        <div
          className="flex items-center justify-between rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <div>
            <div className="text-sm font-medium text-[var(--c-text-primary)]">
              {ds.resetOnboarding}
            </div>
            <div className="text-xs text-[var(--c-text-muted)]">
              {ds.resetOnboardingDesc}
            </div>
          </div>
          <button
            onClick={handleResetOnboarding}
            className="rounded-md bg-[var(--c-bg-deep)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:text-[var(--c-text-primary)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            {resetDone ? '✓' : ds.resetOnboardingBtn}
          </button>
        </div>

        {/* App version */}
        {appVersion && (
          <div
            className="flex items-center justify-between rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <div className="text-sm font-medium text-[var(--c-text-primary)]">
              {ds.appVersion}
            </div>
            <span className="text-sm text-[var(--c-text-muted)]">
              {appVersion}
            </span>
          </div>
        )}
      </div>
    </div>
  )
}
