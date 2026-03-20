import { useEffect, useState } from 'react'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { confirmEmailVerification } from '../api'
import { useLocale } from '../contexts/LocaleContext'

export function VerifyEmailPage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const { t } = useLocale()
  const token = params.get('token') ?? ''
  const [status, setStatus] = useState<'loading' | 'success' | 'error'>('loading')

  useEffect(() => {
    if (!token) {
      const id = requestAnimationFrame(() => setStatus('error'))
      return () => cancelAnimationFrame(id)
    }
    confirmEmailVerification(token)
      .then(() => setStatus('success'))
      .catch(() => setStatus('error'))
  }, [token])

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'var(--c-bg-page)',
      }}
    >
      <div
        className="flex flex-col gap-4 rounded-2xl p-10"
        style={{
          boxShadow: 'inset 0 0 0 0.5px var(--c-border-subtle)',
          background: 'var(--c-bg-menu)',
          minWidth: '320px',
        }}
      >
        {status === 'loading' && (
          <p className="text-sm text-[var(--c-text-secondary)]">...</p>
        )}

        {status === 'success' && (
          <>
            <p className="text-base font-semibold text-[var(--c-text-heading)]">{t.emailVerifySuccess}</p>
            <button
              onClick={() => navigate('/')}
              className="flex h-9 w-fit items-center rounded-lg px-4 text-sm font-medium transition-colors hover:opacity-80"
              style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
            >
              {t.emailVerifyGoToApp}
            </button>
          </>
        )}

        {status === 'error' && (
          <>
            <p className="text-base font-semibold text-[var(--c-text-heading)]">{t.emailVerifyFailed}</p>
            <button
              onClick={() => navigate('/')}
              className="flex h-9 w-fit items-center rounded-lg px-4 text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              {t.emailVerifyGoToApp}
            </button>
          </>
        )}
      </div>
    </div>
  )
}
