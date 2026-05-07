import { useState, useEffect, useCallback } from 'react'
import { HelpCircle, ArrowUpRight, Flag } from 'lucide-react'
import { isApiError, createSuggestionFeedback } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { openExternal } from '../../openExternal'
import { AutoResizeTextarea } from '@arkloop/shared'
import { SettingsButton } from './_SettingsButton'
import { SettingsModalFrame } from './_SettingsModalFrame'

export function HelpContent({ label }: { label: string }) {
  const { locale } = useLocale()
  const docsUrl = locale === 'en' ? 'https://arkloop.io/en/docs/guide' : 'https://arkloop.io/zh/docs/guide'

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
        <SettingsModalFrame
          open
          title={t.suggestionTitle}
          onClose={() => setOpen(false)}
          width={540}
          footer={(
            <>
              <SettingsButton size="modal" variant="secondary" onClick={() => setOpen(false)}>
                {t.reportCancel}
              </SettingsButton>
              <SettingsButton
                size="modal"
                variant="primary"
                onClick={() => void handleSubmit()}
                disabled={submitting || success || !feedback.trim()}
              >
                {submitting ? '...' : t.suggestionSubmit}
              </SettingsButton>
            </>
          )}
        >
          <div className="mt-7">
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
              style={{ borderColor: 'var(--c-border-subtle)', background: 'var(--c-bg-input)' }}
            />

            <div className="mt-2 flex items-center justify-between">
              <span className="text-xs text-[var(--c-text-tertiary)]">{feedback.length}/2000</span>
              {error && <span className="text-xs text-[var(--c-status-error-text)]">{error}</span>}
              {!error && success && <span className="text-xs text-[var(--c-status-success-text,#22c55e)]">{t.suggestionSuccess}</span>}
            </div>
          </div>
        </SettingsModalFrame>
      )}
    </>
  )
}
