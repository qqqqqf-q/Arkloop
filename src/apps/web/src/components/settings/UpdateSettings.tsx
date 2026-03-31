import { useState, useEffect, useCallback } from 'react'
import { RefreshCw } from 'lucide-react'
import { SpinnerIcon } from '@arkloop/shared/components/auth-ui'
import { useLocale } from '../../contexts/LocaleContext'
import { getDesktopApi, type UpdaterComponent, type AppUpdaterState } from '@arkloop/shared/desktop'
import { SettingsSectionHeader } from './_SettingsSectionHeader'

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
}

function getUpdaterApi() {
  return getDesktopApi()?.updater ?? null
}

function getAppUpdaterApi() {
  return getDesktopApi()?.appUpdater ?? null
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

export function UpdateSettingsContent() {
  const { t } = useLocale()

  const [updateStatus, setUpdateStatus] = useState<UpdateStatus | null>(null)
  const [appUpdateState, setAppUpdateState] = useState<AppUpdaterState | null>(null)
  const [checking, setChecking] = useState(false)
  const [checkError, setCheckError] = useState<string | null>(null)
  // 每个组件独立的更新状态
  const [updatingMap, setUpdatingMap] = useState<Partial<Record<UpdaterComponent, UpdatingState>>>({})

  const checkUpdates = useCallback(async () => {
    const updaterApi = getUpdaterApi()
    const appUpdaterApi = getAppUpdaterApi()
    if (!updaterApi && !appUpdaterApi) return
    setChecking(true)
    setCheckError(null)
    try {
      const tasks: Promise<unknown>[] = []
      if (updaterApi) {
        tasks.push(updaterApi.check().then((status) => {
          setUpdateStatus(status)
        }))
      }
      if (appUpdaterApi) {
        tasks.push(appUpdaterApi.check().then((state) => {
          setAppUpdateState(state)
        }))
      }
      await Promise.all(tasks)
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e)
      setCheckError(isSilentUpdateError(message) ? null : message)
    } finally {
      setChecking(false)
    }
  }, [])

  useEffect(() => {
    checkUpdates()
  }, [checkUpdates])

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

    try {
      await api.apply({ component })
    } catch (e) {
      setUpdatingMap((prev) => ({
        ...prev,
        [component]: { phase: 'error', percent: 0, error: e instanceof Error ? e.message : String(e) },
      }))
    } finally {
      unsub()
      // 更新完成后刷新状态
      await checkUpdates()
      setUpdatingMap((prev) => {
        const next = { ...prev }
        delete next[component]
        return next
      })
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
      const message = e instanceof Error ? e.message : String(e)
      setCheckError(isSilentUpdateError(message) ? null : message)
    }
  }, [])

  const handleInstallApp = useCallback(async () => {
    const api = getAppUpdaterApi()
    if (!api) return
    try {
      setCheckError(null)
      await api.install()
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e)
      setCheckError(isSilentUpdateError(message) ? null : message)
    }
  }, [])

  const rows: ComponentRow[] = updateStatus
    ? [
        { key: 'openviking',       label: 'OpenViking',      status: updateStatus.openviking },
        { key: 'sandbox_kernel',   label: 'Sandbox Kernel',  status: updateStatus.sandbox.kernel },
        { key: 'sandbox_rootfs',   label: 'Sandbox Rootfs',  status: updateStatus.sandbox.rootfs },
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
      <div className="flex items-center justify-between">
        <SettingsSectionHeader title={t.nav.updates} />
        <button
          onClick={checkUpdates}
          disabled={checking || isAppUpdaterBusy(appUpdateState)}
          className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm transition-colors"
          style={{
            border: '1px solid var(--c-border-subtle)',
            background: 'var(--c-bg-sub)',
            color: checking || isAppUpdaterBusy(appUpdateState) ? 'var(--c-text-muted)' : 'var(--c-text-primary)',
            cursor: checking || isAppUpdaterBusy(appUpdateState) ? 'not-allowed' : 'pointer',
          }}
        >
          {checking ? <SpinnerIcon /> : <RefreshCw size={14} />}
          <span>{t.desktopSettings.checkForUpdates}</span>
        </button>
      </div>

      {checkError && (
        <p className="text-sm" style={{ color: 'var(--c-status-error)' }}>{checkError}</p>
      )}

      <div
        className="flex flex-col gap-3 rounded-xl px-4 py-4"
        style={{ border: '1px solid var(--c-border-subtle)' }}
      >
        <SettingsSectionHeader title={t.desktopSettings.appUpdateTitle} />
        <div className="flex items-center gap-3">
          <span className="w-32 shrink-0 text-sm font-medium text-[var(--c-text-heading)]">
            {t.desktopSettings.appUpdateVersion}
          </span>
          <div className="flex flex-1 items-center gap-2 text-sm text-[var(--c-text-secondary)]">
            <span>{appUpdateState?.currentVersion ?? '-'}</span>
            {appUpdateState?.latestVersion && appUpdateState.latestVersion !== appUpdateState.currentVersion && (
              <>
                <span style={{ color: 'var(--c-text-muted)' }}>→</span>
                <span
                  className="rounded-full px-1.5 py-0.5 text-xs font-medium"
                  style={{
                    background: 'var(--c-accent-subtle, color-mix(in srgb, var(--c-accent) 15%, transparent))',
                    color: 'var(--c-accent)',
                  }}
                >
                  {appUpdateState.latestVersion}
                </span>
              </>
            )}
          </div>
          <div className="flex items-center gap-2">
            {appBusy ? (
              <div className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
                <SpinnerIcon />
                <span>{appStateText}</span>
              </div>
            ) : appUpdateState?.phase === 'available' ? (
              <button
                onClick={handleDownloadApp}
                className="rounded-lg px-3 py-1 text-sm transition-colors"
                style={{
                  background: 'var(--c-accent)',
                  color: 'var(--c-accent-fg)',
                }}
              >
                {t.desktopSettings.appUpdateDownload}
              </button>
            ) : appUpdateState?.phase === 'downloaded' ? (
              <button
                onClick={handleInstallApp}
                className="rounded-lg px-3 py-1 text-sm transition-colors"
                style={{
                  background: 'var(--c-accent)',
                  color: 'var(--c-accent-fg)',
                }}
              >
                {t.desktopSettings.appUpdateInstall}
              </button>
            ) : (
              <span className="text-sm" style={{ color: appUpdateState?.phase === 'error' ? 'var(--c-status-error)' : 'var(--c-text-secondary)' }}>
                {appStateText ?? '—'}
              </span>
            )}
          </div>
        </div>
      </div>

      {updateStatus && (
        <div
          className="flex flex-col overflow-hidden rounded-xl"
          style={{ border: '1px solid var(--c-border-subtle)' }}
        >
          <div className="px-4 py-3">
            <SettingsSectionHeader title={t.desktopSettings.componentUpdateTitle} />
          </div>
          {rows.map((row) => {
            const updating = updatingMap[row.key]
            const isUpdating = !!updating
            return (
              <div
                key={row.key}
                className="flex items-center gap-3 px-4 py-3"
                style={{
                  borderTop: '1px solid var(--c-border-subtle)',
                }}
              >
                <span className="w-32 shrink-0 text-sm font-medium text-[var(--c-text-heading)]">
                  {row.label}
                </span>

                <div className="flex flex-1 items-center gap-2 text-sm text-[var(--c-text-secondary)]">
                  <span>{row.status.current ?? '-'}</span>
                  {row.status.available && row.status.latest && (
                    <>
                      <span style={{ color: 'var(--c-text-muted)' }}>→</span>
                      <span
                        className="rounded-full px-1.5 py-0.5 text-xs font-medium"
                        style={{
                          background: 'var(--c-accent-subtle, color-mix(in srgb, var(--c-accent) 15%, transparent))',
                          color: 'var(--c-accent)',
                        }}
                      >
                        {row.status.latest}
                      </span>
                    </>
                  )}
                </div>

                <div className="flex items-center gap-2">
                  {isUpdating ? (
                    <div className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
                      {updating.phase === 'error' ? (
                        <span style={{ color: 'var(--c-status-error)' }}>
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
                    <button
                      onClick={() => handleApply(row.key)}
                      className="rounded-lg px-3 py-1 text-sm transition-colors"
                      style={{
                        background: 'var(--c-accent)',
                        color: 'var(--c-accent-fg)',
                      }}
                    >
                      {t.skills.update}
                    </button>
                  ) : (
                    <span className="text-xs" style={{ color: 'var(--c-text-muted)' }}>
                      {/* 已是最新，不显示文字 */}
                    </span>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      )}

      {!updateStatus && !checking && !checkError && (
        <p className="text-sm" style={{ color: 'var(--c-text-muted)' }}>—</p>
      )}
    </div>
  )
}
