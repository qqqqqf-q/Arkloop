import { useState, useEffect } from 'react'
import { ChevronLeft } from 'lucide-react'
import { getDesktopApi } from '@arkloop/shared/desktop'
import { useLocale } from '../../contexts/LocaleContext'
import { readDeveloperShowRunEvents, writeDeveloperShowRunEvents } from '../../storage'
import { RunsSettings } from './RunsSettings'

type Props = {
  accessToken?: string
}

export function DeveloperSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const [appVersion, setAppVersion] = useState('')
  const [resetDone, setResetDone] = useState(false)
  const [showRunEvents, setShowRunEvents] = useState(() => readDeveloperShowRunEvents())
  const [runsOpen, setRunsOpen] = useState(false)

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

  if (runsOpen && accessToken) {
    return (
      <div className="flex flex-col gap-5">
        <button
          onClick={() => setRunsOpen(false)}
          className="flex items-center gap-1 self-start text-sm text-[var(--c-text-secondary)] hover:text-[var(--c-text-primary)] transition-colors"
        >
          <ChevronLeft size={15} />
          {ds.developerTitle}
        </button>
        <RunsSettings accessToken={accessToken} />
      </div>
    )
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
        {/* Show run events toggle */}
        <div
          className="flex items-center justify-between rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <div>
            <div className="text-sm font-medium text-[var(--c-text-primary)]">
              {ds.showRunEvents}
            </div>
            <div className="text-xs text-[var(--c-text-muted)]">
              {ds.showRunEventsDesc}
            </div>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={showRunEvents}
            onClick={() => {
              const next = !showRunEvents
              setShowRunEvents(next)
              writeDeveloperShowRunEvents(next)
            }}
            className={[
              'relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200',
              showRunEvents ? 'bg-[var(--c-accent)]' : 'bg-[var(--c-border)]',
            ].join(' ')}
          >
            <span
              className={[
                'pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow ring-0 transition-transform duration-200',
                showRunEvents ? 'translate-x-4' : 'translate-x-0',
              ].join(' ')}
            />
          </button>
        </div>

        {/* Run history */}
        <div
          className="flex items-center justify-between rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <div>
            <div className="text-sm font-medium text-[var(--c-text-primary)]">
              {ds.runsHistory}
            </div>
            <div className="text-xs text-[var(--c-text-muted)]">
              {ds.runsHistoryDesc}
            </div>
          </div>
          <button
            onClick={() => setRunsOpen(true)}
            disabled={!accessToken}
            className="rounded-md bg-[var(--c-bg-deep)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:text-[var(--c-text-primary)] disabled:opacity-40"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            {ds.runsHistoryOpen}
          </button>
        </div>

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
