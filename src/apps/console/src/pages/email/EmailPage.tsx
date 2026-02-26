import { useState, useEffect, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Loader2, Save, Send } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { Badge } from '../../components/Badge'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  getEmailStatus,
  getEmailConfig,
  updateEmailConfig,
  sendTestEmail,
  type EmailStatus,
  type EmailConfig,
} from '../../api/email'
import { getPlatformSetting, setPlatformSetting } from '../../api/platform-settings'

const TLS_OPTIONS = ['starttls', 'tls', 'none'] as const
const KEY_APP_BASE_URL = 'app.base_url'

export function EmailPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.email

  const [statusLoading, setStatusLoading] = useState(true)
  const [configLoading, setConfigLoading] = useState(true)
  const [status, setStatus] = useState<EmailStatus | null>(null)

  // config form state
  const [from, setFrom] = useState('')
  const [host, setHost] = useState('')
  const [port, setPort] = useState('587')
  const [user, setUser] = useState('')
  const [pass, setPass] = useState('')
  const [passSet, setPassSet] = useState(false)
  const [tlsMode, setTlsMode] = useState('starttls')

  // saved state for dirty check
  const [savedFrom, setSavedFrom] = useState('')
  const [savedHost, setSavedHost] = useState('')
  const [savedPort, setSavedPort] = useState('587')
  const [savedUser, setSavedUser] = useState('')
  const [savedTlsMode, setSavedTlsMode] = useState('starttls')

  const [saving, setSaving] = useState(false)

  // app base url
  const [appUrl, setAppUrl] = useState('')
  const [savedAppUrl, setSavedAppUrl] = useState('')
  const [savingAppUrl, setSavingAppUrl] = useState(false)

  // test send state
  const [testTo, setTestTo] = useState('')
  const [sending, setSending] = useState(false)

  const isDirty =
    from !== savedFrom ||
    host !== savedHost ||
    port !== savedPort ||
    user !== savedUser ||
    tlsMode !== savedTlsMode ||
    pass !== ''

  const applyConfig = useCallback((cfg: EmailConfig) => {
    setFrom(cfg.from)
    setHost(cfg.smtp_host)
    setPort(cfg.smtp_port || '587')
    setUser(cfg.smtp_user)
    setPassSet(cfg.smtp_pass_set)
    setTlsMode(cfg.smtp_tls_mode || 'starttls')
    setSavedFrom(cfg.from)
    setSavedHost(cfg.smtp_host)
    setSavedPort(cfg.smtp_port || '587')
    setSavedUser(cfg.smtp_user)
    setSavedTlsMode(cfg.smtp_tls_mode || 'starttls')
    setPass('')
  }, [])

  const loadAll = useCallback(async () => {
    setStatusLoading(true)
    setConfigLoading(true)
    try {
      const [s, cfg] = await Promise.all([
        getEmailStatus(accessToken),
        getEmailConfig(accessToken),
      ])
      setStatus(s)
      applyConfig(cfg)
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastLoadFailed, 'error')
    } finally {
      setStatusLoading(false)
      setConfigLoading(false)
    }
    // app base url 加载失败不阻断页面（未设置时 404）
    try {
      const setting = await getPlatformSetting(KEY_APP_BASE_URL, accessToken)
      setAppUrl(setting.value)
      setSavedAppUrl(setting.value)
    } catch {
      setAppUrl('')
      setSavedAppUrl('')
    }
  }, [accessToken, addToast, applyConfig, tc.toastLoadFailed])

  useEffect(() => { void loadAll() }, [loadAll])

  const handleSave = async () => {
    setSaving(true)
    try {
      const req: Parameters<typeof updateEmailConfig>[0] = {
        from,
        smtp_host: host,
        smtp_port: port,
        smtp_user: user,
        smtp_tls_mode: tlsMode,
      }
      if (pass !== '') req.smtp_pass = pass
      await updateEmailConfig(req, accessToken)
      setSavedFrom(from)
      setSavedHost(host)
      setSavedPort(port)
      setSavedUser(user)
      setSavedTlsMode(tlsMode)
      if (pass !== '') setPassSet(true)
      setPass('')
      addToast(tc.toastSaved, 'success')
      // reload status badge
      const s = await getEmailStatus(accessToken)
      setStatus(s)
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastSaveFailed, 'error')
    } finally {
      setSaving(false)
    }
  }

  const handleSaveAppUrl = async () => {
    setSavingAppUrl(true)
    try {
      await setPlatformSetting(KEY_APP_BASE_URL, appUrl.trim(), accessToken)
      setSavedAppUrl(appUrl.trim())
      addToast(tc.appUrlSaved, 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.appUrlSaveFailed, 'error')
    } finally {
      setSavingAppUrl(false)
    }
  }

  const handleSendTest = async () => {
    if (!testTo.trim() || !testTo.includes('@')) {
      addToast(tc.errInvalidTo, 'error')
      return
    }
    setSending(true)
    try {
      await sendTestEmail(testTo.trim(), accessToken)
      addToast(tc.toastSent, 'success')
      setTestTo('')
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastSendFailed, 'error')
    } finally {
      setSending(false)
    }
  }

  const inputClass =
    'w-full rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-border-focus)]'
  const labelClass = 'mb-1 block text-xs font-medium text-[var(--c-text-secondary)]'

  const loading = statusLoading || configLoading

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} />
      <div className="flex-1 overflow-y-auto p-6">
        {loading ? (
          <div className="flex items-center justify-center py-16">
            <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
          </div>
        ) : (
          <div className="mx-auto max-w-xl space-y-6">

            {/* Status */}
            <div className="rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5">
              <div className="flex items-center justify-between">
                <div className="space-y-1">
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">
                    {tc.statusTitle}
                  </h3>
                  {status?.from && (
                    <p className="text-xs text-[var(--c-text-muted)]">{status.from}</p>
                  )}
                </div>
                <div className="flex items-center gap-2">
                  {status?.source && status.source !== 'none' && (
                    <Badge variant="neutral">
                      {status.source === 'db' ? tc.sourceDb : tc.sourceEnv}
                    </Badge>
                  )}
                  <Badge variant={status?.configured ? 'success' : 'warning'}>
                    {status?.configured ? tc.statusConfigured : tc.statusNotConfigured}
                  </Badge>
                </div>
              </div>
            </div>

            {/* App Base URL */}
            <div className="rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5">
              <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.appUrlTitle}</h3>
              <div className="mt-4 space-y-3">
                <div>
                  <label className={labelClass}>{tc.appUrlField}</label>
                  <input
                    type="url"
                    className={inputClass}
                    value={appUrl}
                    onChange={e => setAppUrl(e.target.value)}
                    placeholder={tc.appUrlPlaceholder}
                    autoComplete="off"
                  />
                  <p className="mt-1 text-xs text-[var(--c-text-muted)]">{tc.appUrlHint}</p>
                </div>
                <button
                  onClick={handleSaveAppUrl}
                  disabled={savingAppUrl || appUrl.trim() === savedAppUrl}
                  className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
                >
                  {savingAppUrl ? <Loader2 size={12} className="animate-spin" /> : <Save size={12} />}
                  {tc.save}
                </button>
              </div>
            </div>

            {/* SMTP Config */}
            <div className="rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5">
              <h3 className="text-sm font-medium text-[var(--c-text-primary)]">
                {tc.configTitle}
              </h3>
              <div className="mt-4 space-y-4">
                <div>
                  <label className={labelClass}>{tc.fieldFrom}</label>
                  <input
                    type="email"
                    className={inputClass}
                    value={from}
                    onChange={e => setFrom(e.target.value)}
                    placeholder="no-reply@example.com"
                    autoComplete="off"
                  />
                </div>
                <div className="grid grid-cols-3 gap-3">
                  <div className="col-span-2">
                    <label className={labelClass}>{tc.fieldHost}</label>
                    <input
                      type="text"
                      className={inputClass}
                      value={host}
                      onChange={e => setHost(e.target.value)}
                      placeholder="smtp.example.com"
                      autoComplete="off"
                    />
                  </div>
                  <div>
                    <label className={labelClass}>{tc.fieldPort}</label>
                    <input
                      type="number"
                      className={inputClass}
                      value={port}
                      onChange={e => setPort(e.target.value)}
                      placeholder="587"
                      min={1}
                      max={65535}
                    />
                  </div>
                </div>
                <div>
                  <label className={labelClass}>{tc.fieldUser}</label>
                  <input
                    type="text"
                    className={inputClass}
                    value={user}
                    onChange={e => setUser(e.target.value)}
                    placeholder="user@example.com"
                    autoComplete="off"
                  />
                </div>
                <div>
                  <label className={labelClass}>{tc.fieldPass}</label>
                  <input
                    type="password"
                    className={inputClass}
                    value={pass}
                    onChange={e => setPass(e.target.value)}
                    placeholder={passSet ? tc.fieldPassSet : tc.fieldPassPlaceholder}
                    autoComplete="new-password"
                  />
                </div>
                <div>
                  <label className={labelClass}>{tc.fieldTLSMode}</label>
                  <select
                    className={inputClass}
                    value={tlsMode}
                    onChange={e => setTlsMode(e.target.value)}
                  >
                    {TLS_OPTIONS.map(opt => (
                      <option key={opt} value={opt}>{tc[`tls_${opt}` as keyof typeof tc] as string ?? opt}</option>
                    ))}
                  </select>
                </div>
                <div className="border-t border-[var(--c-border-console)] pt-4">
                  <button
                    onClick={handleSave}
                    disabled={saving || !isDirty}
                    className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
                  >
                    {saving ? <Loader2 size={12} className="animate-spin" /> : <Save size={12} />}
                    {tc.save}
                  </button>
                </div>
              </div>
            </div>

            {/* Test */}
            {status?.configured && (
              <div className="rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5">
                <h3 className="text-sm font-medium text-[var(--c-text-primary)]">
                  {tc.testTitle}
                </h3>
                <div className="mt-4 flex gap-2">
                  <input
                    type="email"
                    className={inputClass}
                    placeholder={tc.testPlaceholder}
                    value={testTo}
                    onChange={e => setTestTo(e.target.value)}
                    onKeyDown={e => { if (e.key === 'Enter') void handleSendTest() }}
                    disabled={sending}
                  />
                  <button
                    onClick={handleSendTest}
                    disabled={sending}
                    className="inline-flex shrink-0 items-center gap-1.5 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
                  >
                    {sending ? <Loader2 size={12} className="animate-spin" /> : <Send size={12} />}
                    {tc.testSend}
                  </button>
                </div>
              </div>
            )}

          </div>
        )}
      </div>
    </div>
  )
}

