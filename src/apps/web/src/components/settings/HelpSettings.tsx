import { useState, useEffect, useCallback } from 'react'
import { HelpCircle, ArrowUpRight, Flag, X } from 'lucide-react'
import { isApiError, createSuggestionFeedback } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { openExternal } from '../../openExternal'
import { AutoResizeTextarea } from '@arkloop/shared'

export function HelpContent({ label }: { label: string }) {
  const { locale } = useLocale()
  const docsUrl = locale === 'en' ? 'https://arkloop.cn/en/docs/guide' : 'https://arkloop.cn/zh/docs/guide'

  return (
    <div className="flex flex-col gap-2">
      <button
        type="button"
        onClick={() => openExternal(docsUrl)}
        className="flex h-9 w-[240px] items-center gap-2 rounded-lg px-3 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
      >
        <HelpCircle size={15} />
        <span>{label}</span>
        <ArrowUpRight size={12} style={{ marginLeft: 'auto' }} />
      </button>
    </div>
  )
}

export function ReportFeedbackContent({ accessToken }: { accessToken: string }) {
  const { t } = useLocale()
  const [open, setOpen] = useState(false)
  const [feedback, setFeedback] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [success, setSuccess] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) {
      setFeedback('')
      setSubmitting(false)
      setSuccess(false)
      setError('')
    }
  }, [open])

  useEffect(() => {
    if (!success) return
    const timer = window.setTimeout(() => setOpen(false), 1400)
    return () => window.clearTimeout(timer)
  }, [success])

  const handleSubmit = useCallback(async () => {
    const content = feedback.trim()
    if (!content || submitting) return
    setSubmitting(true)
    setError('')
    try {
      await createSuggestionFeedback(accessToken, content)
      setSuccess(true)
    } catch (err) {
      setError(isApiError(err) ? err.message : t.requestFailed)
    } finally {
      setSubmitting(false)
    }
  }, [accessToken, feedback, submitting, t.requestFailed])

  return (
    <>
      <div className="flex flex-col gap-2">
        <button
          type="button"
          onClick={() => setOpen(true)}
          className="flex h-9 w-[240px] items-center gap-2 rounded-lg px-3 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-heading)]"
          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
        >
          <Flag size={15} />
          <span>{t.submitSuggestion}</span>
        </button>
      </div>

      {open && (
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center"
          style={{ background: 'rgba(0,0,0,0.12)', backdropFilter: 'blur(2px)', WebkitBackdropFilter: 'blur(2px)' }}
          onMouseDown={(e) => { if (e.target === e.currentTarget) setOpen(false) }}
        >
          <div
            className="modal-enter w-full max-w-lg rounded-2xl p-6"
            style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)' }}
          >
            <div className="mb-4 flex items-center justify-between">
              <h3 className="text-base font-semibold text-[var(--c-text-heading)]">{t.suggestionTitle}</h3>
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
              >
                <X size={16} />
              </button>
            </div>

            <AutoResizeTextarea
              value={feedback}
              onChange={(e) => setFeedback(e.target.value)}
              placeholder={t.suggestionPlaceholder}
              maxLength={2000}
              rows={5}
              minRows={5}
              maxHeight={240}
              disabled={submitting || success}
              className="w-full resize-none rounded-lg border px-3 py-2 text-sm text-[var(--c-text-heading)] outline-none placeholder:text-[var(--c-text-tertiary)]"
              style={{ borderColor: 'var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            />

            <div className="mt-2 flex items-center justify-between">
              <span className="text-xs text-[var(--c-text-tertiary)]">{feedback.length}/2000</span>
              {error && <span className="text-xs text-[var(--c-status-error-text)]">{error}</span>}
              {!error && success && <span className="text-xs text-[var(--c-status-success-text,#22c55e)]">{t.suggestionSuccess}</span>}
            </div>

            <div className="mt-4 flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="rounded-lg px-4 py-2 text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
                style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
              >
                {t.reportCancel}
              </button>
              <button
                type="button"
                onClick={() => void handleSubmit()}
                disabled={submitting || success || !feedback.trim()}
                className="rounded-lg px-4 py-2 text-sm font-medium transition-colors disabled:opacity-50"
                style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
              >
                {submitting ? '...' : t.suggestionSubmit}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
