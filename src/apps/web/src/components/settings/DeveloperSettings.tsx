import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { ChevronLeft } from 'lucide-react'
import { getDesktopApi } from '@arkloop/shared/desktop'
import { useToast } from '@arkloop/shared'
import { useLocale } from '../../contexts/LocaleContext'
import { getAccountSettings, updateAccountSettings } from '../../api'
import { readDeveloperShowRunEvents, writeDeveloperShowRunEvents, readDeveloperShowDebugPanel, writeDeveloperShowDebugPanel, readDeveloperPipelineTraceEnabled, writeDeveloperPipelineTraceEnabled } from '../../storage'
import { RunsSettings } from './RunsSettings'
import { PillToggle } from '@arkloop/shared'
import type { DesktopSettingsKey } from '../DesktopSettings'
import { secondaryButtonBorderStyle, secondaryButtonSmCls } from '../buttonStyles'

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
      className={secondaryButtonSmCls}
      style={secondaryButtonBorderStyle}
    >
      {children}
    </button>
  )
}

export function DeveloperSettings({ accessToken, onNavigate }: Props) {
  const { t } = useLocale()
  const { addToast } = useToast()
  const ds = t.desktopSettings
  const [appVersion, setAppVersion] = useState('')
  const [resetDone, setResetDone] = useState(false)
  const [showRunEvents, setShowRunEvents] = useState(() => readDeveloperShowRunEvents())
  const [showDebugPanel, setShowDebugPanel] = useState(() => readDeveloperShowDebugPanel())
  const [pipelineTraceEnabled, setPipelineTraceEnabled] = useState(() => readDeveloperPipelineTraceEnabled())
  const [pipelineTraceLoading, setPipelineTraceLoading] = useState(() => !!accessToken)
  const [pipelineTraceSaving, setPipelineTraceSaving] = useState(false)
  const [runsOpen, setRunsOpen] = useState(false)

  useEffect(() => {
    const api = getDesktopApi()
    if (api) {
      api.app.getVersion().then(setAppVersion).catch(() => {})
    }
  }, [])

  useEffect(() => {
    if (!accessToken) {
      setPipelineTraceEnabled(false)
      setPipelineTraceLoading(false)
      return
    }

    let cancelled = false
    setPipelineTraceLoading(true)
    void getAccountSettings(accessToken)
      .then((settings) => {
        if (cancelled) return
        setPipelineTraceEnabled(settings.pipeline_trace_enabled)
        writeDeveloperPipelineTraceEnabled(settings.pipeline_trace_enabled)
      })
      .catch((error) => {
        if (cancelled) return
        addToast(error instanceof Error ? error.message : t.requestFailed, 'error')
      })
      .finally(() => {
        if (!cancelled) setPipelineTraceLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [accessToken, addToast, t.requestFailed])

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

  const handlePipelineTraceChange = async (next: boolean) => {
    if (!accessToken || pipelineTraceSaving) return

    const previous = pipelineTraceEnabled
    setPipelineTraceEnabled(next)
    setPipelineTraceSaving(true)
    try {
      const settings = await updateAccountSettings(accessToken, {
        pipeline_trace_enabled: next,
      })
      setPipelineTraceEnabled(settings.pipeline_trace_enabled)
      writeDeveloperPipelineTraceEnabled(settings.pipeline_trace_enabled)
    } catch (error) {
      setPipelineTraceEnabled(previous)
      addToast(error instanceof Error ? error.message : t.requestFailed, 'error')
    } finally {
      setPipelineTraceSaving(false)
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
        <div
          className="flex items-center justify-between rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <div>
            <div className="text-sm font-medium text-[var(--c-text-primary)]">
              {ds.pipelineTrace}
            </div>
            <div className="text-xs text-[var(--c-text-muted)]">
              {ds.pipelineTraceDesc}
            </div>
          </div>
          <PillToggle
            checked={pipelineTraceEnabled}
            disabled={!accessToken || pipelineTraceLoading || pipelineTraceSaving}
            onChange={(next) => {
              void handlePipelineTraceChange(next)
            }}
          />
        </div>

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

        {/* Debug panel toggle */}
        <div
          className="flex items-center justify-between rounded-xl bg-[var(--c-bg-menu)] px-4 py-3"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <div>
            <div className="text-sm font-medium text-[var(--c-text-primary)]">
              {ds.showDebugPanel}
            </div>
            <div className="text-xs text-[var(--c-text-muted)]">
              {ds.showDebugPanelDesc}
            </div>
          </div>
          <PillToggle
            checked={showDebugPanel}
            onChange={(next) => {
              setShowDebugPanel(next)
              writeDeveloperShowDebugPanel(next)
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
