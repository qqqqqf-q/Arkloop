import { useEffect, useState } from 'react'
import { ExternalLink, Github, HardDrive } from 'lucide-react'
import { getDesktopApi, getDesktopAppVersion, type DesktopAdvancedOverview } from '@arkloop/shared/desktop'
import { useLocale } from '../../contexts/LocaleContext'
import { openExternal } from '../../openExternal'
import { readDeveloperMode, writeDeveloperMode } from '../../storage'
import { SettingsSection } from './_SettingsSection'
import { SettingsSectionHeader } from './_SettingsSectionHeader'
import { UpdateSettingsContent } from './UpdateSettings'
import { SettingsSwitch } from './_SettingsSwitch'
import { SettingsButton } from './_SettingsButton'

export function AboutSettings({ accessToken: _accessToken }: { accessToken: string }) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const api = getDesktopApi()
  const localAppVersion = getDesktopAppVersion() ?? ''
  const [devMode, setDevMode] = useState(() => readDeveloperMode())
  const [fallbackVersion, setFallbackVersion] = useState('')
  const [overview, setOverview] = useState<DesktopAdvancedOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!api?.advanced) {
      setLoading(false)
      return
    }
    let active = true
    void api.advanced.getOverview()
      .then((data) => {
        if (active) setOverview(data)
      })
      .catch((err) => {
        if (active) setError(err instanceof Error ? err.message : t.requestFailed)
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [api, t.requestFailed])

  useEffect(() => {
    if (overview?.appVersion) return
    if (!api?.app) {
      setFallbackVersion(localAppVersion)
      return
    }
    let active = true
    void api.app.getVersion()
      .then((version) => {
        if (active) setFallbackVersion(version)
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [api, localAppVersion, overview?.appVersion])

  const appName = overview?.appName ?? 'Arkloop'
  const appVersion = overview?.appVersion ?? fallbackVersion
  const links = overview?.links ?? []
  const iconDataUrl = overview?.iconDataUrl ?? null

  return (
    <div className="flex flex-col gap-6">
      <SettingsSectionHeader title={ds.about} description={ds.aboutDesc} />

      <SettingsSection>
        <div className="flex flex-wrap items-start gap-4">
          <div
            className="flex h-14 w-14 items-center justify-center overflow-hidden rounded-2xl bg-[var(--c-bg-deep)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            {iconDataUrl ? (
              <img src={iconDataUrl} alt={appName} className="h-full w-full object-cover" />
            ) : (
              <HardDrive size={22} className="text-[var(--c-text-muted)]" />
            )}
          </div>
          <div className="min-w-[12rem] flex-1">
            <div className="text-lg font-semibold text-[var(--c-text-heading)]">{appName}</div>
            <div className="mt-0.5 text-sm text-[var(--c-text-secondary)]">
              {appVersion || (loading ? '...' : '')}
            </div>
          </div>
          <div className="flex basis-full flex-wrap gap-2 xl:ml-auto xl:basis-auto xl:justify-end">
            {links.map((link) => (
              <SettingsButton
                key={link.url}
                onClick={() => openExternal(link.url)}
                variant="secondary"
                className="shrink-0"
                icon={link.label === 'GitHub' ? <Github size={14} /> : <ExternalLink size={14} />}
              >
                {link.label}
              </SettingsButton>
            ))}
          </div>
        </div>
      </SettingsSection>

      {error && (
        <SettingsSection>
          <p className="text-sm" style={{ color: 'var(--c-status-error)' }}>{error}</p>
        </SettingsSection>
      )}

      <SettingsSection>
        <UpdateSettingsContent />
      </SettingsSection>

      <SettingsSection>
        <div className="flex items-center justify-between">
          <div>
            <div className="text-sm font-medium text-[var(--c-text-primary)]">{ds.developerTitle}</div>
            <div className="text-xs text-[var(--c-text-muted)]">{ds.developerDesc}</div>
          </div>
          <SettingsSwitch
            checked={devMode}
            onChange={(next) => {
              setDevMode(next)
              writeDeveloperMode(next)
            }}
          />
        </div>
      </SettingsSection>
    </div>
  )
}
