import { useState, useEffect, useCallback, useRef } from 'react'
import { RefreshCw, Loader2, ChevronDown, ChevronUp, QrCode } from 'lucide-react'
import {
  type NapCatStatus,
  getNapCatStatus,
  napCatDownload,
  napCatRefreshQR,
  napCatFetchQRCode,
  napCatQuickLogin,
} from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { secondaryButtonSmCls, secondaryButtonBorderStyle } from './buttonStyles'

type Props = {
  accessToken: string
  channelId: string
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function QQLoginFlow({ accessToken, channelId: _channelId }: Props) {
  const { t } = useLocale()
  const ct = t.channels
  const [status, setStatus] = useState<NapCatStatus | null>(null)
  const [error, setError] = useState('')
  const [logsOpen, setLogsOpen] = useState(false)
  const [qrBlobUrl, setQrBlobUrl] = useState<string | null>(null)
  const [setupRequested, setSetupRequested] = useState(false)
  const [showQRCode, setShowQRCode] = useState(false)
  const [quickLoginLoading, setQuickLoginLoading] = useState<string | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval>>()
  const logEndRef = useRef<HTMLDivElement>(null)
  const prevQrUrl = useRef<string | undefined>()

  const fetchStatus = useCallback(async () => {
    try {
      const s = await getNapCatStatus(accessToken)
      setStatus(s)
      return s
    } catch {
      return null
    }
  }, [accessToken])

  // poll status
  useEffect(() => {
    fetchStatus()
    pollRef.current = setInterval(fetchStatus, 2000)
    return () => clearInterval(pollRef.current)
  }, [fetchStatus])

  // keep log panel scrolled to bottom when new lines arrive
  useEffect(() => {
    const el = logEndRef.current?.parentElement
    if (el && logsOpen) {
      el.scrollTop = el.scrollHeight
    }
  }, [status?.logs?.length, logsOpen])

  // fetch QR code image as blob when qrcode_url changes
  useEffect(() => {
    if (status?.logged_in) {
      setQrBlobUrl(prev => { if (prev) URL.revokeObjectURL(prev); return null })
      prevQrUrl.current = undefined
      return
    }
    const url = status?.qrcode_url
    if (url && url !== prevQrUrl.current) {
      prevQrUrl.current = url
      let revoked = false
      napCatFetchQRCode(accessToken)
        .then(blobUrl => {
          if (!revoked) {
            setQrBlobUrl(prev => { if (prev) URL.revokeObjectURL(prev); return blobUrl })
          } else {
            URL.revokeObjectURL(blobUrl)
          }
        })
        .catch(() => { /* spinner will show */ })
      return () => { revoked = true }
    }
    if (!url) {
      setQrBlobUrl(prev => { if (prev) URL.revokeObjectURL(prev); return null })
      prevQrUrl.current = undefined
    }
  }, [status?.qrcode_url, status?.logged_in, accessToken])

  // cleanup blob URL on unmount
  useEffect(() => {
    return () => { if (qrBlobUrl) URL.revokeObjectURL(qrBlobUrl) }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleSetup = async () => {
    setError('')
    setLogsOpen(true)
    setSetupRequested(true)
    try {
      await napCatDownload(accessToken)
    } catch (e) {
      setError(String(e))
    }
  }

  const handleRefreshQR = async () => {
    try {
      await napCatRefreshQR(accessToken)
      await fetchStatus()
    } catch { /* ignore */ }
  }

  const handleQuickLogin = async (uin: string) => {
    setQuickLoginLoading(uin)
    setError('')
    try {
      await napCatQuickLogin(accessToken, uin)
      await fetchStatus()
    } catch (e) {
      setError(String(e))
    } finally {
      setQuickLoginLoading(null)
    }
  }

  const phase = status?.setup_phase ?? ''
  const isSetupInProgress = phase === 'fetch_info' || phase === 'downloading' || phase === 'extracting' || phase === 'starting'
  const logs = status?.logs ?? []
  const hasLogs = logs.length > 0
  const quickLoginList = status?.quick_login_list ?? []
  const hasQuickLogin = quickLoginList.length > 0

  const showPendingSetup = setupRequested && !status?.running && !isSetupInProgress && phase !== 'done' && phase !== 'error'

  // --- logged in ---
  if (status?.logged_in) {
    return (
      <div className="flex flex-col gap-3">
        <div className="flex items-center gap-3 py-2">
          <div
            className="flex h-8 w-8 items-center justify-center rounded-full text-xs font-medium"
            style={{ background: 'var(--c-status-success-bg, rgba(34,197,94,0.1))', color: 'var(--c-status-success-text, #22c55e)' }}
          >
            QQ
          </div>
          <div className="flex flex-col">
            <span className="text-sm text-[var(--c-text-heading)]">
              {status.nickname || status.qq}
            </span>
            <span className="text-xs text-[var(--c-text-tertiary)]">
              {status.qq ? `${status.qq} - ` : ''}{ct.qqLoggedIn}
            </span>
          </div>
        </div>
        <LogPanel logs={logs} open={logsOpen} onToggle={() => setLogsOpen(v => !v)} label={ct.qqLogs} logEndRef={logEndRef} />
      </div>
    )
  }

  // --- running but not logged in ---
  if (status?.running) {
    // 有快速登录列表且用户没有主动选择扫码
    if (hasQuickLogin && !showQRCode) {
      return (
        <div className="flex flex-col gap-3 py-2">
          <span className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.qqQuickLogin}</span>

          <div className="flex flex-col gap-1.5">
            {quickLoginList.map(account => (
              <button
                key={account.uin}
                onClick={() => handleQuickLogin(account.uin)}
                disabled={quickLoginLoading !== null}
                className="flex items-center gap-3 rounded-lg px-3 py-2 text-left transition-colors hover:bg-[var(--c-bg-deep)]"
                style={{ border: '0.5px solid var(--c-border-subtle)' }}
              >
                <div
                  className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-[10px] font-medium"
                  style={{ background: 'var(--c-bg-deep)', color: 'var(--c-text-muted)' }}
                >
                  QQ
                </div>
                <div className="flex flex-col min-w-0">
                  <span className="text-sm text-[var(--c-text-heading)] truncate">
                    {account.nickname || account.uin}
                  </span>
                  <span className="text-xs text-[var(--c-text-tertiary)]">{account.uin}</span>
                </div>
                {quickLoginLoading === account.uin && (
                  <Loader2 size={14} className="ml-auto animate-spin text-[var(--c-text-muted)]" />
                )}
              </button>
            ))}
          </div>

          <button
            onClick={() => setShowQRCode(true)}
            className="flex items-center gap-1.5 self-start rounded px-2 py-1 text-xs text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)]"
          >
            <QrCode size={12} />
            {ct.qqScanNewAccount}
          </button>

          {error && (
            <div className="rounded-lg px-3 py-2 text-xs" style={{ color: 'var(--c-status-error-text)', background: 'var(--c-status-error-bg, rgba(239,68,68,0.08))' }}>
              {error}
            </div>
          )}

          {status?.login_error && (
            <div className="rounded-lg px-3 py-2 text-xs" style={{ color: 'var(--c-status-error-text)', background: 'var(--c-status-error-bg, rgba(239,68,68,0.08))' }}>
              {status.login_error}
            </div>
          )}

          <LogPanel logs={logs} open={logsOpen} onToggle={() => setLogsOpen(v => !v)} label={ct.qqLogs} logEndRef={logEndRef} />
        </div>
      )
    }

    // 显示二维码（无快速登录列表 或 用户主动选择扫码）
    return (
      <div className="flex flex-col gap-3 py-2">
        <div className="flex items-center justify-between">
          <span className="text-xs font-medium text-[var(--c-text-secondary)]">{ct.qqScanToLogin}</span>
          <div className="flex items-center gap-2">
            {hasQuickLogin && (
              <button
                onClick={() => setShowQRCode(false)}
                className="flex items-center gap-1 rounded px-2 py-1 text-xs text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)]"
              >
                {ct.qqQuickLogin}
              </button>
            )}
            <button
              onClick={handleRefreshQR}
              className="flex items-center gap-1 rounded px-2 py-1 text-xs text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)]"
            >
              <RefreshCw size={12} />
              {ct.qqRefreshQR}
            </button>
          </div>
        </div>

        <div className="flex justify-center rounded-lg p-4" style={{ background: 'var(--c-bg-sub)', border: '0.5px solid var(--c-border-subtle)' }}>
          {qrBlobUrl ? (
            <img
              src={qrBlobUrl}
              alt="QQ Login QR Code"
              className="h-48 w-48"
              style={{ imageRendering: 'pixelated' }}
            />
          ) : (
            <div className="flex items-center justify-center h-48 w-48">
              <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
            </div>
          )}
        </div>

        {status?.login_error && (
          <div className="rounded-lg px-3 py-2 text-xs" style={{ color: 'var(--c-status-error-text)', background: 'var(--c-status-error-bg, rgba(239,68,68,0.08))' }}>
            {status.login_error}
          </div>
        )}

        <LogPanel logs={logs} open={logsOpen} onToggle={() => setLogsOpen(v => !v)} label={ct.qqLogs} logEndRef={logEndRef} />
      </div>
    )
  }

  // --- setup in progress or pending ---
  if (isSetupInProgress || showPendingSetup) {
    const progress = status?.setup_progress ?? 0
    const total = status?.setup_total ?? 0

    const progressText = (() => {
      if (showPendingSetup || phase === 'fetch_info') return ct.qqFetchingInfo
      if (phase === 'downloading') {
        if (total > 0) {
          const pct = Math.min(100, Math.round((progress / total) * 100))
          return `${ct.qqDownloading} ${formatBytes(progress)} / ${formatBytes(total)} (${pct}%)`
        }
        if (progress > 0) return `${ct.qqDownloading} ${formatBytes(progress)}`
        return `${ct.qqDownloading}...`
      }
      if (phase === 'extracting') return ct.qqExtracting
      if (phase === 'starting') return ct.qqStarting
      return ''
    })()

    return (
      <div className="flex flex-col gap-2 py-2">
        <div className="flex items-center gap-2">
          <Loader2 size={14} className="animate-spin text-[var(--c-text-muted)]" />
          <span className="text-xs text-[var(--c-text-tertiary)]">{progressText}</span>
        </div>
        {phase === 'downloading' && total > 0 && (
          <div className="h-1.5 w-full overflow-hidden rounded-full" style={{ background: 'var(--c-bg-deep)' }}>
            <div
              className="h-full rounded-full transition-all duration-300"
              style={{
                width: `${Math.min(100, Math.round((progress / total) * 100))}%`,
                background: 'var(--c-status-success, #22c55e)',
              }}
            />
          </div>
        )}
        <LogPanel logs={logs} open={logsOpen} onToggle={() => setLogsOpen(v => !v)} label={ct.qqLogs} logEndRef={logEndRef} />
      </div>
    )
  }

  // --- idle / error: show setup button ---
  const setupError = status?.setup_error || error

  return (
    <div className="flex flex-col gap-3 py-2">
      {setupError && (
        <div className="rounded-lg px-3 py-2 text-xs" style={{ color: 'var(--c-status-error-text)', background: 'var(--c-status-error-bg, rgba(239,68,68,0.08))' }}>
          {setupError}
        </div>
      )}

      <button
        onClick={handleSetup}
        disabled={isSetupInProgress}
        className={`${secondaryButtonSmCls} self-start`}
        style={secondaryButtonBorderStyle}
      >
        {ct.qqSetup}
      </button>

      {hasLogs && (
        <LogPanel logs={logs} open={logsOpen} onToggle={() => setLogsOpen(v => !v)} label={ct.qqLogs} logEndRef={logEndRef} />
      )}
    </div>
  )
}

function LogPanel({ logs, open, onToggle, label, logEndRef }: {
  logs: string[]
  open: boolean
  onToggle: () => void
  label: string
  logEndRef: React.RefObject<HTMLDivElement | null>
}) {
  if (logs.length === 0) return null

  return (
    <div
      className="rounded-lg overflow-hidden"
      style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-deep)' }}
    >
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-center justify-between px-3 py-1.5 text-[11px] font-medium text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)]"
      >
        {label}
        {open ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
      </button>
      {open && (
        <div
          className="max-h-40 overflow-y-auto px-3 pb-2 font-mono text-[11px] leading-[1.6] text-[var(--c-text-muted)]"
          style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}
        >
          {logs.map((line, i) => (
            <div key={i}>{line}</div>
          ))}
          <div ref={logEndRef} />
        </div>
      )}
    </div>
  )
}
