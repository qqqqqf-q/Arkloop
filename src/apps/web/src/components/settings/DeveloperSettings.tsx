import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { ChevronLeft } from 'lucide-react'
import { getDesktopApi } from '@arkloop/shared/desktop'
import { useLocale } from '../../contexts/LocaleContext'
import { readDeveloperShowRunEvents, writeDeveloperShowRunEvents } from '../../storage'
import { RunsSettings } from './RunsSettings'
import { PillToggle } from '@arkloop/shared'
import type { DesktopSettingsKey } from '../DesktopSettings'

type Props = {
  accessToken?: string
  onNavigate?: (key: DesktopSettingsKey) => void
}

type PanelBtnProps = {
  onClick: () => void
  disabled?: boolean
  children: ReactNode
}

function PanelButton({ onClick, disabled, children }: PanelBtnProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className="inline-flex items-center justify-center gap-1.5 rounded-lg px-4 py-1.5 text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:cursor-not-allowed disabled:opacity-40"
      style={{ border: '0.5px solid var(--c-border-subtle)' }}
    >
      {children}
    </button>
  )
}

export function DeveloperSettings({ accessToken, onNavigate }: Props) {
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
          <PillToggle
            checked={showRunEvents}
            onChange={(next) => {
              setShowRunEvents(next)
              writeDeveloperShowRunEvents(next)
            }}
          />
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
          <PanelButton
            onClick={() => setRunsOpen(true)}
            disabled={!accessToken}
          >
            {ds.runsHistoryOpen}
          </PanelButton>
        </div>

        {/* Design Tokens */}
        {onNavigate && (
          <div
            className="flex items-center justify-between rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <div>
              <div className="text-sm font-medium text-[var(--c-text-primary)]">
                Design Tokens
              </div>
              <div className="text-xs text-[var(--c-text-muted)]">
                All CSS variables resolved for the current theme.
              </div>
            </div>
            <PanelButton onClick={() => onNavigate('design-tokens')}>
              查看
            </PanelButton>
          </div>
        )}

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
          <PanelButton onClick={handleResetOnboarding}>
            {resetDone ? '✓' : ds.resetOnboardingBtn}
          </PanelButton>
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
