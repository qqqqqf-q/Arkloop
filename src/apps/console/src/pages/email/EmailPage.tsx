import { useState, useEffect, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Loader2, Send } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { Badge } from '../../components/Badge'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { getEmailStatus, sendTestEmail, type EmailStatus } from '../../api/email'

export function EmailPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.email

  const [status, setStatus] = useState<EmailStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [testTo, setTestTo] = useState('')
  const [sending, setSending] = useState(false)

  const loadStatus = useCallback(async () => {
    setLoading(true)
    try {
      const data = await getEmailStatus(accessToken)
      setStatus(data)
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => { loadStatus() }, [loadStatus])

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

  return (
    <div className="p-6 max-w-2xl">
      <PageHeader title={tc.title} />

      {loading ? (
        <div className="flex items-center gap-2 text-sm text-gray-500 mt-4">
          <Loader2 size={16} className="animate-spin" />
          {t.loading}
        </div>
      ) : status ? (
        <div className="space-y-6">
          <div className="border rounded-lg p-4 space-y-3">
            <h2 className="text-sm font-semibold">{tc.statusTitle}</h2>
            <div className="flex items-center gap-2 text-sm">
              <span className="text-gray-500">{tc.labelStatus}:</span>
              {status.configured
                ? <Badge variant="success">{tc.statusConfigured}</Badge>
                : <Badge variant="neutral">{tc.statusNotConfigured}</Badge>
              }
            </div>
            {status.from && (
              <div className="flex items-center gap-2 text-sm">
                <span className="text-gray-500">{tc.labelFrom}:</span>
                <span className="font-mono">{status.from}</span>
              </div>
            )}
            <div className="flex items-center gap-2 text-sm">
              <span className="text-gray-500">{tc.labelProvider}:</span>
              <span className="font-mono">{status.provider}</span>
            </div>
          </div>

          {status.configured && (
            <div className="border rounded-lg p-4 space-y-3">
              <h2 className="text-sm font-semibold">{tc.testTitle}</h2>
              <div className="flex gap-2">
                <input
                  type="email"
                  className="flex-1 border rounded px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder={tc.testPlaceholder}
                  value={testTo}
                  onChange={e => setTestTo(e.target.value)}
                  onKeyDown={e => { if (e.key === 'Enter') handleSendTest() }}
                  disabled={sending}
                />
                <button
                  className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
                  onClick={handleSendTest}
                  disabled={sending}
                >
                  {sending ? <Loader2 size={14} className="animate-spin" /> : <Send size={14} />}
                  {tc.testSend}
                </button>
              </div>
            </div>
          )}
        </div>
      ) : null}
    </div>
  )
}
