import { useState, useEffect, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Loader2, Send } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { Badge } from '../../components/Badge'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  getEmailStatus,
  sendTestEmail,
  type EmailStatus,
} from '../../api/email'

export function EmailPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.email

  const [loading, setLoading] = useState(true)
  const [status, setStatus] = useState<EmailStatus | null>(null)

  const [testTo, setTestTo] = useState('')
  const [sending, setSending] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const s = await getEmailStatus(accessToken)
      setStatus(s)
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => { void load() }, [load])

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
