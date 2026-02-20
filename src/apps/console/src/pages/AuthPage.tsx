import { useState, useMemo, type FormEvent } from 'react'
import { login, isApiError } from '../api'

type AppError = {
  message: string
  traceId?: string
  code?: string
}

function normalizeError(error: unknown): AppError {
  if (isApiError(error)) {
    return { message: error.message, traceId: error.traceId, code: error.code }
  }
  if (error instanceof Error) {
    return { message: error.message }
  }
  return { message: '请求失败' }
}

type Props = {
  onLoggedIn: (accessToken: string) => void
}

export function AuthPage({ onLoggedIn }: Props) {
  const [loginValue, setLoginValue] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<AppError | null>(null)

  const canSubmit = useMemo(() => {
    if (submitting) return false
    return Boolean(loginValue.trim() && password)
  }, [loginValue, password, submitting])

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (!canSubmit) return
    setSubmitting(true)
    setError(null)
    try {
      const resp = await login({ login: loginValue, password })
      onLoggedIn(resp.access_token)
    } catch (err) {
      setError(normalizeError(err))
    } finally {
      setSubmitting(false)
    }
  }

  const inputStyle = {
    border: '0.5px solid #3A3A3A',
  }

  return (
    <div
      className="flex min-h-screen flex-col items-center justify-center px-5"
      style={{ background: '#141413', padding: '72px 20px', gap: '48px' }}
    >
      <header className="flex flex-col items-center" style={{ gap: '10px' }}>
        <div style={{ fontSize: '32px', fontWeight: 500, color: '#FAF9F5' }}>Arkloop</div>
        <div style={{ fontSize: '14px', fontWeight: 500, color: 'rgba(255,255,255,0.42)', letterSpacing: '0.1em', textTransform: 'uppercase' }}>
          Console
        </div>
      </header>

      <section
        style={{
          width: 'min(400px, 100%)',
          borderRadius: '16px',
          padding: '32px 36px',
          background: '#1a1a18',
          border: '0.5px solid #3A3A3A',
        }}
      >
        <form className="flex flex-col" style={{ gap: '14px' }} onSubmit={onSubmit}>
          <input
            className="w-full rounded-[10px] bg-[#262624] text-[#FAF9F5] outline-none placeholder:text-[rgba(255,255,255,0.3)]"
            style={{ ...inputStyle, height: '44px', padding: '0 14px', fontSize: '14px', fontFamily: 'inherit' }}
            type="text"
            placeholder="Username"
            value={loginValue}
            onChange={(e) => setLoginValue(e.target.value)}
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
          />

          <input
            className="w-full rounded-[10px] bg-[#262624] text-[#FAF9F5] outline-none placeholder:text-[rgba(255,255,255,0.3)]"
            style={{ ...inputStyle, height: '44px', padding: '0 14px', fontSize: '14px', fontFamily: 'inherit' }}
            type="password"
            placeholder="Password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />

          <button
            type="submit"
            disabled={!canSubmit}
            style={{
              height: '44px',
              marginTop: '6px',
              borderRadius: '10px',
              border: 'none',
              cursor: 'pointer',
              fontSize: '14px',
              fontWeight: 500,
              background: '#FAF9F6',
              color: '#141413',
            }}
            className="disabled:cursor-not-allowed disabled:opacity-40"
          >
            {submitting ? '...' : 'Sign in'}
          </button>
        </form>

        {error && (
          <div className="mt-3 rounded-lg border border-red-900/40 bg-red-950/30 px-3 py-2.5 text-sm">
            <div className="font-medium text-red-300">{error.message}</div>
            {(error.code || error.traceId) && (
              <div className="mt-1 space-y-0.5 font-mono text-xs text-red-400/70">
                {error.code && <div>code: {error.code}</div>}
                {error.traceId && <div>trace_id: {error.traceId}</div>}
              </div>
            )}
          </div>
        )}
      </section>
    </div>
  )
}
