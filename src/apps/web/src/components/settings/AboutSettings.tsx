import { useEffect, useState, type ReactNode } from 'react'
import { ExternalLink, Github, HardDrive } from 'lucide-react'
import { getDesktopApi, getDesktopAppVersion, type DesktopAdvancedOverview } from '@arkloop/shared/desktop'
import { useLocale } from '../../contexts/LocaleContext'
import { openExternal } from '../../openExternal'
import { readDeveloperMode, writeDeveloperMode } from '../../storage'
import { UpdateSettingsContent } from './UpdateSettings'
import { SettingsSwitch } from './_SettingsSwitch'
import { SettingsButton } from './_SettingsButton'

function AboutSection({
  title,
  children,
}: {
  title: string
  children: ReactNode
}) {
  return (
    <section className="flex flex-col gap-2.5">
      <h3 className="pl-2.5 text-[13px] font-normal text-[var(--c-text-secondary)]">{title}</h3>
      {children}
    </section>
  )
}

function AboutCard({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-hidden rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]">
      {children}
    </div>
  )
}

function AboutRow({
  title,
  description,
  leading,
  control,
}: {
  title: string
  description?: ReactNode
  leading?: ReactNode
  control?: ReactNode
}) {
  const hasControl = control !== undefined

  return (
    <div
      className={[
        'relative grid items-center gap-3 px-5 py-4 sm:gap-6',
        hasControl ? 'sm:grid-cols-[minmax(0,1fr)_auto]' : '',
        "[&+&]:before:absolute [&+&]:before:left-5 [&+&]:before:right-5 [&+&]:before:top-0 [&+&]:before:h-px [&+&]:before:bg-[var(--c-border-subtle)] [&+&]:before:content-['']",
      ].join(' ')}
    >
      <div className="flex min-w-0 items-center gap-3">
        {leading && <div className="shrink-0">{leading}</div>}
        <div className="min-w-0">
          <div className="text-[13px] font-medium text-[var(--c-text-primary)]">{title}</div>
          {description && (
            <div className="mt-1 text-xs leading-5 text-[var(--c-text-tertiary)]">{description}</div>
          )}
        </div>
      </div>
      {hasControl && <div className="min-w-0 sm:justify-self-end">{control}</div>}
    </div>
  )
}

export function AboutSettings({ accessToken: _accessToken }: { accessToken: string }) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const api = getDesktopApi()
  const localAppVersion = getDesktopAppVersion() ?? ''
  const [devMode, setDevMode] = useState(() => readDeveloperMode())
  const [fallbackVersion, setFallbackVersion] = useState(localAppVersion)
  const [overview, setOverview] = useState<DesktopAdvancedOverview | null>(null)
  const [loading, setLoading] = useState(() => Boolean(api?.advanced))
  const [error, setError] = useState('')

  useEffect(() => {
    if (!api?.advanced) {
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
    <div className="mx-auto flex w-full max-w-[760px] flex-col gap-6 px-1 pb-8">
      <div>
        <h2 className="text-[24px] font-semibold leading-tight tracking-normal text-[var(--c-text-heading)]">
          {ds.about}
        </h2>
      </div>

      <AboutSection title={ds.about}>
        <AboutCard>
          <AboutRow
            title={appName}
            description={appVersion || (loading ? '...' : '')}
            leading={(
              <div
                className="flex h-10 w-10 items-center justify-center overflow-hidden rounded-[10px] bg-[var(--c-bg-deep)]"
                style={{ border: '0.5px solid var(--c-border-subtle)' }}
              >
                {iconDataUrl ? (
                  <img src={iconDataUrl} alt={appName} className="h-full w-full object-cover" />
                ) : (
                  <HardDrive size={20} className="text-[var(--c-text-muted)]" />
                )}
              </div>
            )}
          />
          {links.length > 0 && (
            <AboutRow
              title={t.getHelp}
              control={(
                <div className="flex flex-wrap justify-end gap-2">
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
              )}
            />
          )}
        </AboutCard>
      </AboutSection>

      {error && (
        <AboutSection title={ds.about}>
          <AboutCard>
            <div className="px-5 py-4 text-sm text-[var(--c-status-error)]">
              {error}
            </div>
          </AboutCard>
        </AboutSection>
      )}

      <UpdateSettingsContent />

      <AboutSection title={ds.developerTitle}>
        <AboutCard>
          <AboutRow
            title={ds.developerTitle}
            description={ds.developerDesc}
            control={(
              <SettingsSwitch
                checked={devMode}
                onChange={(next) => {
                  setDevMode(next)
                  writeDeveloperMode(next)
                }}
              />
            )}
          />
        </AboutCard>
      </AboutSection>
    </div>
  )
}
