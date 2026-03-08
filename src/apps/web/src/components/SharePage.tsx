import { useState, useEffect, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Lock } from 'lucide-react'
import { MessageBubble } from './MessageBubble'
import { getSharedThread, verifySharePassword, isApiError, type SharedThreadResponse } from '../api'
import { useLocale } from '../contexts/LocaleContext'

function SpinnerIcon() {
  return (
    <svg className="animate-spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" aria-hidden="true">
      <path d="M21 12a9 9 0 1 1-6.219-8.56" />
    </svg>
  )
}

export function SharePage() {
  const { token } = useParams<{ token: string }>()
  const navigate = useNavigate()
  const { t } = useLocale()

  const [loading, setLoading] = useState(true)
  const [data, setData] = useState<SharedThreadResponse | null>(null)
  const [notFound, setNotFound] = useState(false)

  // 密码保护状态
  const [needsPassword, setNeedsPassword] = useState(false)
  const [password, setPassword] = useState('')
  const [passwordError, setPasswordError] = useState(false)
  const [verifying, setVerifying] = useState(false)
  const [sessionToken, setSessionToken] = useState<string | null>(null)

  const loadData = useCallback(async (st?: string) => {
    if (!token) return
    setLoading(true)
    try {
      const resp = await getSharedThread(token, st ?? sessionToken ?? undefined)
      if (resp.requires_password && !st) {
        setNeedsPassword(true)
      } else {
        setData(resp)
        setNeedsPassword(false)
      }
    } catch (err) {
      if (isApiError(err) && (err.status === 404 || err.status === 403)) {
        if (err.code === 'shares.invalid_session') {
          setSessionToken(null)
          setNeedsPassword(true)
        } else {
          setNotFound(true)
        }
      }
    } finally {
      setLoading(false)
    }
  }, [token, sessionToken])

  useEffect(() => {
    void loadData()
  }, [loadData])

  const handleVerify = useCallback(async () => {
    if (!token || !password.trim()) return
    setVerifying(true)
    setPasswordError(false)
    try {
      const resp = await verifySharePassword(token, password)
      setSessionToken(resp.session_token)
      void loadData(resp.session_token)
    } catch (err) {
      if (isApiError(err) && err.status === 403) {
        setPasswordError(true)
      }
    } finally {
      setVerifying(false)
    }
  }, [token, password, loadData])

  // Brand bar
  const brandBar = (
    <div
      className="flex items-center justify-between px-5 py-3"
      style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
    >
      <button
        onClick={() => navigate('/')}
        className="text-base font-semibold tracking-tight"
        style={{ color: 'var(--c-text-primary)' }}
      >
        Arkloop
      </button>
      <div className="flex items-center gap-2">
        <button
          onClick={() => navigate('/login')}
          className="rounded-lg px-3 py-1.5 text-sm font-medium transition-colors hover:bg-[var(--c-bg-sub)]"
          style={{ color: 'var(--c-text-secondary)' }}
        >
          {t.sharePageLogin}
        </button>
        <button
          onClick={() => navigate('/login')}
          className="rounded-lg px-3 py-1.5 text-sm font-medium"
          style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
        >
          {t.sharePageRegister}
        </button>
      </div>
    </div>
  )

  if (loading) {
    return (
      <div className="flex h-screen flex-col bg-[var(--c-bg-page)]">
        {brandBar}
        <div className="flex flex-1 items-center justify-center">
          <div className="text-sm" style={{ color: 'var(--c-text-muted)' }}>{t.loading}</div>
        </div>
      </div>
    )
  }

  if (notFound) {
    return (
      <div className="flex h-screen flex-col bg-[var(--c-bg-page)]">
        {brandBar}
        <div className="flex flex-1 items-center justify-center">
          <div className="text-center">
            <p className="text-sm" style={{ color: 'var(--c-text-muted)' }}>{t.sharePageNotFound}</p>
          </div>
        </div>
      </div>
    )
  }

  if (needsPassword) {
    return (
      <div className="flex h-screen flex-col bg-[var(--c-bg-page)]">
        {brandBar}
        <div className="flex flex-1 items-center justify-center">
          <div
            className="w-full max-w-sm rounded-2xl p-6"
            style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)' }}
          >
            <div className="mb-4 flex flex-col items-center gap-3">
              <div
                className="flex h-10 w-10 items-center justify-center rounded-xl"
                style={{ background: 'var(--c-bg-sub)' }}
              >
                <Lock size={20} style={{ color: 'var(--c-text-icon)' }} />
              </div>
              <p className="text-sm font-medium" style={{ color: 'var(--c-text-primary)' }}>
                {t.sharePagePasswordTitle}
              </p>
            </div>
            <input
              type="password"
              value={password}
              onChange={(e) => { setPassword(e.target.value); setPasswordError(false) }}
              placeholder={t.sharePagePasswordPlaceholder}
              autoFocus
              className="mb-3 w-full rounded-lg px-3 py-2 text-sm outline-none"
              style={{
                background: 'var(--c-bg-sub)',
                border: `0.5px solid ${passwordError ? 'var(--c-destructive, #ef4444)' : 'var(--c-border-subtle)'}`,
                color: 'var(--c-text-primary)',
              }}
              onKeyDown={(e) => { if (e.key === 'Enter') void handleVerify() }}
            />
            {passwordError && (
              <p className="mb-3 text-xs" style={{ color: 'var(--c-destructive, #ef4444)' }}>
                {t.sharePagePasswordWrong}
              </p>
            )}
            <button
              onClick={() => void handleVerify()}
              disabled={verifying || !password.trim()}
              className="flex w-full items-center justify-center gap-1.5 rounded-lg px-3 py-2.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
              style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
            >
              {verifying && <SpinnerIcon />}
              {t.sharePagePasswordSubmit}
            </button>
          </div>
        </div>
      </div>
    )
  }

  // 消息列表
  const messages = data?.messages ?? []

  return (
    <div className="flex h-screen flex-col bg-[var(--c-bg-page)]">
      {brandBar}
      <div className="flex-1 overflow-y-auto">
        <div
          style={{ maxWidth: 800, margin: '0 auto', padding: '50px 60px' }}
          className="flex w-full flex-col gap-6"
        >
          {data?.thread?.title && (
            <h1
              className="text-lg font-semibold"
              style={{ color: 'var(--c-text-primary)' }}
            >
              {data.thread.title}
            </h1>
          )}

          {messages.map((msg) => (
            <MessageBubble
              key={msg.id}
              message={{
                id: msg.id,
                org_id: '',
                thread_id: '',
                created_by_user_id: '',
                role: msg.role,
                content: msg.content,
                content_json: msg.content_json,
                created_at: msg.created_at,
              }}
            />
          ))}

          {messages.length === 0 && (
            <div className="py-20 text-center text-sm" style={{ color: 'var(--c-text-muted)' }}>
              {t.sharePageNotFound}
            </div>
          )}
        </div>
      </div>

      {/* Footer */}
      <div
        className="flex items-center justify-center py-3"
        style={{ borderTop: '0.5px solid var(--c-border-subtle)' }}
      >
        <span className="text-xs" style={{ color: 'var(--c-text-muted)' }}>
          {t.sharePagePoweredBy}
        </span>
      </div>
    </div>
  )
}
