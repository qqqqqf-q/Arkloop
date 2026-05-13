import { useState, useEffect, useCallback, type ReactNode } from 'react'
import { RefreshCw } from 'lucide-react'
import { Button } from '@arkloop/shared'
import { SpinnerIcon } from '@arkloop/shared/components/auth-ui'
import { useLocale } from '../../contexts/LocaleContext'
import { getDesktopApi, getDesktopAppVersion, type UpdaterComponent, type AppUpdaterState } from '@arkloop/shared/desktop'

type ComponentStatus = {
  current: string | null
  latest: string | null
  available: boolean
}

type UpdateStatus = {
  openviking: ComponentStatus
  sandbox: {
    kernel: ComponentStatus
    rootfs: ComponentStatus
  }
  bins: {
    rtk: ComponentStatus
    opencli: ComponentStatus
  }
}

function getUpdaterApi() {
  return getDesktopApi()?.updater ?? null
}

function getAppUpdaterApi() {
  const api = getDesktopApi()?.appUpdater
  if (api) return api
  const state = localHeadlessAppUpdaterState()
  if (!state) return null
  return {
    getState: async () => state,
    check: async () => state,
    download: async () => state,
    install: async () => ({ ok: false }),
    onState: () => () => {},
  }
}

function localHeadlessAppUpdaterState(): AppUpdaterState | null {
  const currentVersion = getDesktopAppVersion()
  if (!currentVersion) return null
  return {
    supported: false,
    phase: 'unsupported',
    currentVersion,
    latestVersion: null,
    progressPercent: 0,
    error: null,
  }
}

type ComponentRow = {
  key: UpdaterComponent
  label: string
  status: ComponentStatus
}

type UpdatingState = {
  phase: 'connecting' | 'downloading' | 'verifying' | 'done' | 'error'
  percent: number
  error?: string
}

function UpdateSection({
  title,
  action,
  children,
}: {
  title: string
  action?: ReactNode
  children?: ReactNode
}) {
  return (
    <section className="flex flex-col gap-2.5">
      <div className="flex items-center justify-between gap-3 pl-2.5">
        <h3 className="text-[13px] font-normal text-[var(--c-text-secondary)]">{title}</h3>
        {action}
      </div>
      {children}
    </section>
  )
}

function UpdateCard({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-hidden rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]">
      {children}
    </div>
  )
}

function UpdateRow({
  title,
  description,
  value,
  control,
}: {
  title: string
  description?: ReactNode
  value?: ReactNode
  control?: ReactNode
}) {
  const hasRight = value !== undefined || control !== undefined

  return (
    <div
      className={[
        'relative grid items-center gap-3 px-5 py-4 sm:gap-6',
        hasRight ? 'sm:grid-cols-[minmax(0,1fr)_auto]' : '',
        "[&+&]:before:absolute [&+&]:before:left-5 [&+&]:before:right-5 [&+&]:before:top-0 [&+&]:before:h-px [&+&]:before:bg-[var(--c-border-subtle)] [&+&]:before:content-['']",
      ].join(' ')}
    >
      <div className="min-w-0">
        <div className="text-[13px] font-medium text-[var(--c-text-primary)]">{title}</div>
        {description && (
          <div className="mt-1 text-xs leading-5 text-[var(--c-text-tertiary)]">{description}</div>
        )}
      </div>
      {hasRight && (
        <div className="flex min-w-0 flex-wrap items-center gap-2 sm:justify-end">
          {value}
          {control}
        </div>
      )}
    </div>
  )
}

function VersionValue({
  current,
  latest,
  missingText,
}: {
  current?: string | null
  latest?: string | null
  missingText?: string
}) {
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2 text-sm text-[var(--c-text-secondary)]">
      {current ? (
        <span>{current}</span>
      ) : missingText ? (
        <span className="text-[var(--c-text-muted)]">{missingText}</span>
      ) : null}
      {latest && latest !== current && (
        <>
          <span className="text-[var(--c-text-muted)]">→</span>
          <span
            className="rounded-full px-1.5 py-0.5 text-xs font-medium"
            style={{
              background: 'var(--c-accent-subtle, color-mix(in srgb, var(--c-accent) 15%, transparent))',
              color: 'var(--c-accent)',
            }}
          >
            {latest}
          </span>
        </>
      )}
    </div>
  )
}

function isAppUpdaterBusy(state: AppUpdaterState | null) {
  return state?.phase === 'checking' || state?.phase === 'downloading'
}

function isSilentUpdateError(message: string | null): boolean {
  if (!message) return false
  const normalized = message.toLowerCase()
  return normalized.includes('failed to fetch release info: 404')
    || normalized.includes('not published')
    || normalized.includes('no release published')
}

function getVisibleErrorMessage(error: unknown): string | null {
  const message = error instanceof Error ? error.message : String(error)
  return isSilentUpdateError(message) ? null : message
}

export function UpdateSettingsContent() {
  const { t } = useLocale()

  const [updateStatus, setUpdateStatus] = useState<UpdateStatus | null>(null)
  const [appUpdateState, setAppUpdateState] = useState<AppUpdaterState | null>(null)
  const [checking, setChecking] = useState(false)
  const [checkError, setCheckError] = useState<string | null>(null)
  // 每个组件独立的更新状态
  const [updatingMap, setUpdatingMap] = useState<Partial<Record<UpdaterComponent, UpdatingState>>>({})

  useEffect(() => {
    const updaterApi = getUpdaterApi()
    if (!updaterApi) return
    let active = true
    void updaterApi.getCached()
      .then((status) => {
        if (active) setUpdateStatus(status)
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [])

  const checkUpdates = useCallback(async () => {
    const updaterApi = getUpdaterApi()
    const appUpdaterApi = getAppUpdaterApi()
    if (!updaterApi && !appUpdaterApi) return
    setChecking(true)
    setCheckError(null)
    let visibleError: string | null = null
    const tasks: Promise<void>[] = []

    if (updaterApi) {
      tasks.push(
        updaterApi.check().then((status) => {
          setUpdateStatus(status)
        }).catch((error) => {
          visibleError ??= getVisibleErrorMessage(error)
        }),
      )
    }

    if (appUpdaterApi) {
      tasks.push(
        appUpdaterApi.check().then((state) => {
          setAppUpdateState(state)
        }).catch((error) => {
          visibleError ??= getVisibleErrorMessage(error)
        }),
      )
    }

    try {
      await Promise.all(tasks)
      setCheckError(visibleError)
    } finally {
      setChecking(false)
    }
  }, [])

  useEffect(() => {
    const api = getAppUpdaterApi()
    if (!api) return

    let active = true
    void api.getState().then((state) => {
      if (active) setAppUpdateState(state)
    }).catch(() => {})

    const unsub = api.onState((state) => {
      setAppUpdateState(state)
    })

    return () => {
      active = false
      unsub()
    }
  }, [])

  useEffect(() => {
    void checkUpdates()
  }, [checkUpdates])

  const handleApply = useCallback(async (component: UpdaterComponent) => {
    const api = getUpdaterApi()
    if (!api) return

    setUpdatingMap((prev) => ({
      ...prev,
      [component]: { phase: 'connecting', percent: 0 },
    }))

    const unsub = api.onProgress((progress) => {
      setUpdatingMap((prev) => ({
        ...prev,
        [component]: {
          phase: progress.phase,
          percent: progress.percent,
          error: progress.error,
        },
      }))
    })

    let succeeded = false
    try {
      await api.apply({ component })
      succeeded = true
    } catch (e) {
      setUpdatingMap((prev) => ({
        ...prev,
        [component]: { phase: 'error', percent: 0, error: e instanceof Error ? e.message : String(e) },
      }))
    } finally {
      unsub()
      // 更新完成后刷新状态
      await checkUpdates()
      if (succeeded) {
        setUpdatingMap((prev) => {
          const next = { ...prev }
          delete next[component]
          return next
        })
      }
    }
  }, [checkUpdates])

  const handleDownloadApp = useCallback(async () => {
    const api = getAppUpdaterApi()
    if (!api) return
    try {
      setCheckError(null)
      const state = await api.download()
      setAppUpdateState(state)
    } catch (e) {
      setCheckError(getVisibleErrorMessage(e))
    }
  }, [])

  const handleInstallApp = useCallback(async () => {
    const api = getAppUpdaterApi()
    if (!api) return
    try {
      setCheckError(null)
      await api.install()
    } catch (e) {
      setCheckError(getVisibleErrorMessage(e))
    }
  }, [])

  const rows: ComponentRow[] = updateStatus
    ? [
        { key: 'openviking',       label: 'OpenViking',      status: updateStatus.openviking },
        { key: 'sandbox_kernel',   label: 'Sandbox Kernel',  status: updateStatus.sandbox.kernel },
        { key: 'sandbox_rootfs',   label: 'Sandbox Rootfs',  status: updateStatus.sandbox.rootfs },
        ...(updateStatus.bins ? [
          { key: 'rtk' as UpdaterComponent,     label: 'RTK',      status: updateStatus.bins.rtk },
          { key: 'opencli' as UpdaterComponent,  label: 'OpenCLI',  status: updateStatus.bins.opencli },
        ] : []),
      ]
    : []

  const appBusy = checking || isAppUpdaterBusy(appUpdateState)
  const appStateText = (() => {
    if (!appUpdateState) return null
    switch (appUpdateState.phase) {
      case 'unsupported':
        return t.desktopSettings.appUpdateUnsupported
      case 'checking':
        return t.desktopSettings.appUpdateChecking
      case 'available':
        return t.desktopSettings.appUpdateAvailable
      case 'not-available':
        return t.desktopSettings.appUpdateLatest
      case 'downloading':
        return `${t.desktopSettings.appUpdateDownloading} ${appUpdateState.progressPercent}%`
      case 'downloaded':
        return t.desktopSettings.appUpdateReady
      case 'error':
        return appUpdateState.error ?? t.desktopSettings.appUpdateError
      default:
        return null
    }
  })()

  return (
    <div className="flex flex-col gap-6">
      <UpdateSection title={t.desktopSettings.appUpdateTitle}>
        <UpdateCard>
          <UpdateRow
            title={t.desktopSettings.appUpdateVersion}
            description={(checkError || (appStateText && !appBusy)) ? (
              <span className={(checkError || appUpdateState?.phase === 'error') ? 'text-[var(--c-status-error)]' : undefined}>
                {checkError ?? appStateText}
              </span>
            ) : undefined}
            value={(
              <VersionValue
                current={appUpdateState?.currentVersion}
                latest={appUpdateState?.latestVersion}
              />
            )}
            control={(
              <>
                {appBusy && !checking ? (
                  <div className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
                    <SpinnerIcon />
                    <span>{appStateText}</span>
                  </div>
                ) : appUpdateState?.phase === 'available' ? (
                  <Button
                    onClick={handleDownloadApp}
                    variant="primary"
                    size="sm"
                  >
                    {t.desktopSettings.appUpdateDownload}
                  </Button>
                ) : appUpdateState?.phase === 'downloaded' ? (
                  <Button
                    onClick={handleInstallApp}
                    variant="primary"
                    size="sm"
                  >
                    {t.desktopSettings.appUpdateInstall}
                  </Button>
                ) : null}
                <Button
                  onClick={checkUpdates}
                  disabled={appBusy}
                  variant="outline"
                  size="sm"
                  loading={checking}
                >
                  {!checking && <RefreshCw size={14} />}
                  <span>{t.desktopSettings.checkForUpdates}</span>
                </Button>
              </>
            )}
          />
        </UpdateCard>
      </UpdateSection>

      {updateStatus && (
        <UpdateSection title={t.desktopSettings.componentUpdateTitle}>
          <UpdateCard>
            {rows.map((row) => {
              const updating = updatingMap[row.key]
              const isUpdating = !!updating
              return (
                <UpdateRow
                  key={row.key}
                  title={row.label}
                  value={(
                    <VersionValue
                      current={row.status.current}
                      latest={row.status.available ? row.status.latest : null}
                      missingText={t.desktopSettings.componentNotInstalled}
                    />
                  )}
                  control={isUpdating ? (
                    <div className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
                      {updating.phase === 'error' ? (
                        <span className="text-[var(--c-status-error)]">
                          {updating.error ?? 'error'}
                        </span>
                      ) : (
                        <>
                          <SpinnerIcon />
                          <span>{updating.percent}%</span>
                        </>
                      )}
                    </div>
                  ) : row.status.available ? (
                    <Button
                      onClick={() => handleApply(row.key)}
                      variant="primary"
                      size="sm"
                    >
                      {t.skills.update}
                    </Button>
                  ) : undefined}
                />
              )
            })}
          </UpdateCard>
        </UpdateSection>
      )}
    </div>
  )
}
