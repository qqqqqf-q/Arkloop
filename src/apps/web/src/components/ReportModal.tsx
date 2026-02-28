import { useState, useEffect, useRef } from 'react'
import { X, ImageOff, CalendarClock, FileText, FileWarning, ShieldAlert, Globe } from 'lucide-react'
import { createThreadReport, isApiError } from '../api'
import { useLocale } from '../contexts/LocaleContext'

function SpinnerIcon() {
  return (
    <svg className="animate-spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" aria-hidden="true">
      <path d="M21 12a9 9 0 1 1-6.219-8.56" />
    </svg>
  )
}

type Props = {
  accessToken: string
  threadId: string
  open: boolean
  onClose: () => void
}

type CategoryDef = {
  key: string
  labelKey: keyof ReturnType<typeof useLocale>['t']
  icon: React.ReactNode
}

const categories: CategoryDef[] = [
  { key: 'inaccurate',           labelKey: 'reportInaccurate',    icon: <ImageOff size={16} /> },
  { key: 'out_of_date',          labelKey: 'reportOutOfDate',     icon: <CalendarClock size={16} /> },
  { key: 'too_short',            labelKey: 'reportTooShort',      icon: <FileText size={16} /> },
  { key: 'too_long',             labelKey: 'reportTooLong',       icon: <FileWarning size={16} /> },
  { key: 'harmful_or_offensive', labelKey: 'reportHarmful',       icon: <ShieldAlert size={16} /> },
  { key: 'wrong_sources',        labelKey: 'reportWrongSources',  icon: <Globe size={16} /> },
]

export function ReportModal({ accessToken, threadId, open, onClose }: Props) {
  const { t } = useLocale()
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [feedback, setFeedback] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState(false)
  const formRef = useRef<HTMLDivElement>(null)
  const successRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (open) {
      setSelected(new Set())
      setFeedback('')
      setError(null)
      setSuccess(false)
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [onClose, open])

  const toggle = (key: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const handleSubmit = async () => {
    if (selected.size === 0) return
    setSubmitting(true)
    setError(null)
    try {
      await createThreadReport(accessToken, threadId, Array.from(selected), feedback.trim() || undefined)
      setSuccess(true)
      setTimeout(onClose, 2000)
    } catch (err) {
      if (isApiError(err)) setError(err.message)
      else setError('unexpected error')
    } finally {
      setSubmitting(false)
    }
  }

  if (!open) return null

  return (
    <div
      className="overlay-fade-in fixed inset-0 z-50 flex items-center justify-center"
      style={{ background: 'rgba(0,0,0,0.12)', backdropFilter: 'blur(2px)', WebkitBackdropFilter: 'blur(2px)' }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="modal-enter relative w-full max-w-lg rounded-2xl p-6"
        style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)' }}
      >
        {/* header */}
        <div className="mb-1 flex items-center justify-between">
          <h2 className="text-base font-semibold" style={{ color: 'var(--c-text-primary)' }}>
            {t.reportTitle}
          </h2>
          <button
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-lg transition-colors hover:bg-[var(--c-bg-deep)]"
            style={{ color: 'var(--c-text-muted)' }}
          >
            <X size={16} />
          </button>
        </div>

        <p className="mb-4 text-sm" style={{ color: 'var(--c-text-muted)' }}>
          {t.reportSubtitle}
        </p>

        {/* content wrapper with height transition */}
        <div style={{ position: 'relative', overflow: 'hidden' }}>
          {/* success state */}
          <div
            ref={successRef}
            style={{
              position: success ? 'relative' : 'absolute',
              inset: 0,
              opacity: success ? 1 : 0,
              transform: success ? 'translateY(0)' : 'translateY(8px)',
              transition: 'opacity 0.25s ease, transform 0.25s ease',
              pointerEvents: success ? 'auto' : 'none',
            }}
          >
            <div className="py-8 text-center text-sm" style={{ color: 'var(--c-text-secondary)' }}>
              {t.reportSuccess}
            </div>
          </div>

          {/* form state */}
          <div
            ref={formRef}
            style={{
              opacity: success ? 0 : 1,
              transform: success ? 'translateY(-8px)' : 'translateY(0)',
              transition: 'opacity 0.25s ease, transform 0.25s ease',
              pointerEvents: success ? 'none' : 'auto',
              ...(success ? { position: 'absolute', inset: 0 } : {}),
            }}
          >
            {/* categories grid */}
            <div className="mb-4 grid grid-cols-2 gap-2">
              {categories.map(cat => {
                const isSelected = selected.has(cat.key)
                return (
                  <button
                    key={cat.key}
                    onClick={() => toggle(cat.key)}
                    className="flex items-center gap-2 rounded-lg border px-3 py-2.5 text-left text-sm transition-colors"
                    style={{
                      borderColor: isSelected ? 'var(--c-text-muted)' : 'var(--c-border-subtle)',
                      background: isSelected ? 'var(--c-bg-deep)' : 'transparent',
                      color: 'var(--c-text-primary)',
                      cursor: 'pointer',
                    }}
                  >
                    <span style={{ color: 'var(--c-text-muted)', flexShrink: 0 }}>{cat.icon}</span>
                    {t[cat.labelKey] as string}
                  </button>
                )
              })}
            </div>

            {/* feedback */}
            <div className="mb-4">
              <p className="mb-2 text-sm" style={{ color: 'var(--c-text-muted)' }}>
                {t.reportFeedbackLabel}
              </p>
              <textarea
                value={feedback}
                onChange={e => setFeedback(e.target.value)}
                placeholder={t.reportFeedbackPlaceholder}
                maxLength={2000}
                rows={3}
                className="w-full resize-none rounded-lg border px-3 py-2 text-sm outline-none transition-colors focus:border-[var(--c-text-muted)]"
                style={{
                  borderColor: 'var(--c-border-subtle)',
                  background: 'var(--c-bg-page)',
                  color: 'var(--c-text-primary)',
                  fontFamily: 'inherit',
                }}
              />
            </div>

            {error && (
              <p className="mb-3 text-xs" style={{ color: 'var(--c-danger, #ef4444)' }}>{error}</p>
            )}

            {/* actions */}
            <div className="flex items-center justify-end gap-2">
              <button
                onClick={onClose}
                className="rounded-lg px-4 py-2 text-sm font-medium transition-colors hover:bg-[var(--c-bg-deep)]"
                style={{ color: 'var(--c-text-primary)', background: 'var(--c-bg-sub)', cursor: 'pointer', border: 'none' }}
              >
                {t.reportCancel}
              </button>
              <button
                onClick={handleSubmit}
                disabled={selected.size === 0 || submitting}
                className="flex items-center justify-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium transition-colors"
                style={{
                  background: selected.size === 0 || submitting ? 'var(--c-bg-sub)' : 'var(--c-text-primary)',
                  color: selected.size === 0 || submitting ? 'var(--c-text-muted)' : 'var(--c-bg-page)',
                  cursor: selected.size === 0 || submitting ? 'default' : 'pointer',
                  border: 'none',
                }}
              >
                {submitting && <SpinnerIcon />}
                {t.reportSubmit}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
