import { useState, useEffect, useCallback } from 'react'
import { RefreshCw } from 'lucide-react'
import { SpinnerIcon } from '@arkloop/shared/components/auth-ui'
import { useLocale } from '../../contexts/LocaleContext'
import { getDesktopApi, type UpdaterComponent } from '@arkloop/shared/desktop'
import { SettingsSectionHeader } from './_SettingsSectionHeader'

type ComponentStatus = {
  current: string | null
  latest: string | null
  available: boolean
}

type UpdateStatus = {
  sidecar: ComponentStatus
  openviking: ComponentStatus
  sandbox: {
    kernel: ComponentStatus
    rootfs: ComponentStatus
  }
}

function getUpdaterApi() {
  return getDesktopApi()?.updater ?? null
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

export function UpdateSettingsContent() {
  const { t } = useLocale()

  const [updateStatus, setUpdateStatus] = useState<UpdateStatus | null>(null)
  const [checking, setChecking] = useState(false)
  const [checkError, setCheckError] = useState<string | null>(null)
  // 每个组件独立的更新状态
  const [updatingMap, setUpdatingMap] = useState<Partial<Record<UpdaterComponent, UpdatingState>>>({})

  const checkUpdates = useCallback(async () => {
    const api = getUpdaterApi()
    if (!api) return
    setChecking(true)
    setCheckError(null)
    try {
      const status = await api.check()
      setUpdateStatus(status)
    } catch (e) {
      setCheckError(e instanceof Error ? e.message : String(e))
    } finally {
      setChecking(false)
    }
  }, [])

  // 组件挂载时自动检查一次
  useEffect(() => {
    checkUpdates()
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

  const rows: ComponentRow[] = updateStatus
    ? [
        { key: 'sidecar',          label: 'Sidecar',         status: updateStatus.sidecar },
        { key: 'openviking',       label: 'OpenViking',      status: updateStatus.openviking },
        { key: 'sandbox_kernel',   label: 'Sandbox Kernel',  status: updateStatus.sandbox.kernel },
        { key: 'sandbox_rootfs',   label: 'Sandbox Rootfs',  status: updateStatus.sandbox.rootfs },
      ]
    : []

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <SettingsSectionHeader title={t.nav.updates} />
        <button
          onClick={checkUpdates}
          disabled={checking}
          className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm transition-colors"
          style={{
            border: '1px solid var(--c-border-subtle)',
            background: 'var(--c-bg-sub)',
            color: checking ? 'var(--c-text-muted)' : 'var(--c-text-primary)',
            cursor: checking ? 'not-allowed' : 'pointer',
          }}
        >
          {checking ? <SpinnerIcon /> : <RefreshCw size={14} />}
          <span>{t.desktopSettings.checkForUpdates}</span>
        </button>
      </div>

      {checkError && (
        <p className="text-sm" style={{ color: 'var(--c-status-error)' }}>{checkError}</p>
      )}

      {updateStatus && (
        <div
          className="flex flex-col overflow-hidden rounded-xl"
          style={{ border: '1px solid var(--c-border-subtle)' }}
        >
          {rows.map((row, idx) => {
            const updating = updatingMap[row.key]
            const isUpdating = !!updating
            return (
              <div
                key={row.key}
                className="flex items-center gap-3 px-4 py-3"
                style={{
                  borderTop: idx === 0 ? 'none' : '1px solid var(--c-border-subtle)',
                }}
              >
                {/* 组件名 */}
                <span className="w-32 shrink-0 text-sm font-medium text-[var(--c-text-heading)]">
                  {row.label}
                </span>

                {/* 版本信息 */}
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

                {/* 操作区 */}
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
