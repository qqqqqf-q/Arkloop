import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { setupBootstrapAdmin, verifyBootstrapToken, isApiError } from '../api'
import { ErrorCallout, type AppError } from '../components/ErrorCallout'
import { useLocale } from '../contexts/LocaleContext'

type Props = {
  onLoggedIn: (accessToken: string) => void
}

function normalizeError(error: unknown): AppError {
  if (isApiError(error)) return { message: error.message, traceId: error.traceId, code: error.code }
  if (error instanceof Error) return { message: error.message }
  return { message: 'Request failed' }
}

export function BootstrapPage({ onLoggedIn }: Props) {
  const { token = '' } = useParams()
  const navigate = useNavigate()
  const { t, locale } = useLocale()

  const [valid, setValid] = useState<boolean | null>(null)
  const [expiresAt, setExpiresAt] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [success, setSuccess] = useState(false)
  const [pendingAccessToken, setPendingAccessToken] = useState<string | null>(null)
  const [error, setError] = useState<AppError | null>(null)

  useEffect(() => {
    let cancelled = false
    setValid(null)
    setError(null)

    verifyBootstrapToken(token)
      .then((resp) => {
        if (cancelled) return
        setValid(resp.valid)
        setExpiresAt(resp.expires_at)
      })
      .catch((err) => {
        if (cancelled) return
        setValid(false)
        setError(normalizeError(err))
      })

    return () => {
      cancelled = true
    }
  }, [token])

  useEffect(() => {
    if (!success || !pendingAccessToken) return
    const timer = window.setTimeout(() => {
      onLoggedIn(pendingAccessToken)
      navigate('/dashboard', { replace: true })
    }, 900)
    return () => window.clearTimeout(timer)
  }, [navigate, onLoggedIn, pendingAccessToken, success])

  const expiresLabel = useMemo(() => {
    if (!expiresAt) return ''
    const date = new Date(expiresAt)
    const formatted = Number.isNaN(date.valueOf())
      ? expiresAt
      : new Intl.DateTimeFormat(locale === 'zh' ? 'zh-CN' : 'en-US', {
          dateStyle: 'medium',
          timeStyle: 'short',
        }).format(date)
    return t.bootstrap.expiresAt(formatted)
  }, [expiresAt, locale, t.bootstrap])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!token || !username.trim() || !password) return
    setSubmitting(true)
    setError(null)
    try {
      const resp = await setupBootstrapAdmin({
        token,
        username: username.trim(),
        password,
        locale,
      })
      setPendingAccessToken(resp.access_token)
      setSuccess(true)
    } catch (err) {
      const normalized = normalizeError(err)
      setError(normalized)
      if (normalized.code === 'bootstrap.invalid_token' || normalized.code === 'bootstrap.already_initialized') {
        setValid(false)
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '24px',
        background: 'var(--c-bg-page)',
      }}
    >
      <div
        style={{
          width: '100%',
          maxWidth: '440px',
          borderRadius: '24px',
          border: '1px solid var(--c-border-auth)',
          background: 'var(--c-bg-card)',
          boxShadow: '0 20px 60px rgba(0,0,0,0.18)',
          padding: '28px',
        }}
      >
        <div style={{ fontSize: '24px', fontWeight: 700, color: 'var(--c-text-primary)' }}>{t.bootstrap.title}</div>
        <div style={{ marginTop: '8px', fontSize: '14px', color: 'var(--c-text-secondary)' }}>{t.bootstrap.subtitle}</div>

        {valid === null && (
          <div style={{ marginTop: '20px', fontSize: '14px', color: 'var(--c-text-secondary)' }}>{t.bootstrap.verifying}</div>
        )}

        {valid === false && (
          <div style={{ marginTop: '20px' }}>
            <div style={{ fontSize: '16px', fontWeight: 600, color: 'var(--c-text-primary)' }}>{t.bootstrap.invalidTitle}</div>
            <div style={{ marginTop: '8px', fontSize: '14px', color: 'var(--c-text-secondary)' }}>{t.bootstrap.invalidBody}</div>
            {error && <ErrorCallout error={error} />}
          </div>
        )}

        {valid && !success && (
          <form onSubmit={handleSubmit} style={{ marginTop: '20px' }}>
            <label style={{ display: 'block', fontSize: '12px', fontWeight: 600, color: 'var(--c-text-secondary)' }}>
              {t.bootstrap.username}
            </label>
            <input
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              placeholder={t.bootstrap.usernamePlaceholder}
              autoComplete="username"
              style={{
                marginTop: '8px',
                width: '100%',
                height: '40px',
                borderRadius: '12px',
                border: '1px solid var(--c-border-auth)',
                background: 'var(--c-bg-input)',
                color: 'var(--c-text-primary)',
                padding: '0 14px',
                outline: 'none',
              }}
            />

            <label style={{ display: 'block', marginTop: '16px', fontSize: '12px', fontWeight: 600, color: 'var(--c-text-secondary)' }}>
              {t.bootstrap.password}
            </label>
            <input
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              placeholder={t.bootstrap.passwordPlaceholder}
              autoComplete="new-password"
              style={{
                marginTop: '8px',
                width: '100%',
                height: '40px',
                borderRadius: '12px',
                border: '1px solid var(--c-border-auth)',
                background: 'var(--c-bg-input)',
                color: 'var(--c-text-primary)',
                padding: '0 14px',
                outline: 'none',
              }}
            />

            {expiresLabel && (
              <div style={{ marginTop: '12px', fontSize: '12px', color: 'var(--c-text-secondary)' }}>{expiresLabel}</div>
            )}

            {error && <ErrorCallout error={error} />}

            <button
              type="submit"
              disabled={submitting || !username.trim() || !password}
              style={{
                marginTop: '20px',
                width: '100%',
                height: '42px',
                border: 'none',
                borderRadius: '12px',
                background: 'var(--c-accent, #6366f1)',
                color: '#fff',
                fontWeight: 700,
                cursor: submitting ? 'progress' : 'pointer',
                opacity: submitting ? 0.7 : 1,
              }}
            >
              {submitting ? t.loading : t.bootstrap.submit}
            </button>
          </form>
        )}

        {success && (
          <div style={{ marginTop: '20px' }}>
            <div style={{ fontSize: '16px', fontWeight: 600, color: 'var(--c-text-primary)' }}>{t.bootstrap.successTitle}</div>
            <div style={{ marginTop: '8px', fontSize: '14px', color: 'var(--c-text-secondary)' }}>{t.bootstrap.successBody}</div>
          </div>
        )}
      </div>
    </div>
  )
}
