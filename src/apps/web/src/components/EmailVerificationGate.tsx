import { useState, useRef, useEffect } from 'react'
import { sendEmailVerification, sendEmailOTP, verifyEmailOTP, isApiError, getMe } from '../api'
import { useLocale } from '../contexts/LocaleContext'

const POLL_INTERVAL_MS = 4000

interface Props {
  accessToken: string
  email: string
  onVerified: () => void
  onPollVerified: () => void
  onLogout: () => void
}

export function EmailVerificationGate({ accessToken, email, onVerified, onPollVerified, onLogout }: Props) {
  const { t } = useLocale()

  // 轮询检测邮箱是否已在其他标签页/设备完成验证
  useEffect(() => {
    const id = setInterval(async () => {
      try {
        const me = await getMe(accessToken)
        if (me.email_verified) {
          clearInterval(id)
          onPollVerified()
        }
      } catch { /* 静默 */ }
    }, POLL_INTERVAL_MS)
    return () => clearInterval(id)
  }, [accessToken, onPollVerified])

  const [resendCountdown, setResendCountdown] = useState(0)
  const [resending, setResending] = useState(false)
  const [resentOnce, setResentOnce] = useState(false)

  // OTP flow
  const [showOtp, setShowOtp] = useState(false)
  const [otpCode, setOtpCode] = useState('')
  const [otpCountdown, setOtpCountdown] = useState(0)
  const [otpSending, setOtpSending] = useState(false)
  const [otpSubmitting, setOtpSubmitting] = useState(false)
  const [otpError, setOtpError] = useState('')

  const resendTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const otpTimer = useRef<ReturnType<typeof setInterval> | null>(null)

  const startCountdown = (set: React.Dispatch<React.SetStateAction<number>>, ref: React.MutableRefObject<ReturnType<typeof setInterval> | null>) => {
    set(60)
    if (ref.current) clearInterval(ref.current)
    ref.current = setInterval(() => {
      set((c) => {
        if (c <= 1) { clearInterval(ref.current!); return 0 }
        return c - 1
      })
    }, 1000)
  }

  const [checking, setChecking] = useState(false)

  const handleCheckNow = async () => {
    setChecking(true)
    try {
      const me = await getMe(accessToken)
      if (me.email_verified) onPollVerified()
    } catch { /* 静默 */ } finally {
      setChecking(false)
    }
  }

  const handleResend = async () => {
    setResending(true)
    try {
      await sendEmailVerification(accessToken)
      setResentOnce(true)
    } catch { /* 静默 */ } finally {
      setResending(false)
      startCountdown(setResendCountdown, resendTimer)
    }
  }

  const handleSwitchToOtp = async () => {
    setOtpCode('')
    setOtpError('')
    setShowOtp(true)
    setOtpSending(true)
    try { await sendEmailOTP(email) } catch { /* 静默 */ } finally {
      setOtpSending(false)
      startCountdown(setOtpCountdown, otpTimer)
    }
  }

  const handleVerifyOtp = async () => {
    if (otpCode.length !== 6) return
    setOtpSubmitting(true)
    setOtpError('')
    try {
      await verifyEmailOTP(email, otpCode)
      onVerified()
    } catch (err) {
      setOtpError(isApiError(err) ? err.message : 'invalid code')
    } finally {
      setOtpSubmitting(false)
    }
  }

  return (
    <div style={{
      position: 'fixed', inset: 0, zIndex: 100,
      background: 'var(--c-bg-page)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
    }}>
      <div style={{
        width: 'min(400px, calc(100vw - 40px))',
        background: 'var(--c-bg-menu)',
        borderRadius: '16px',
        boxShadow: 'inset 0 0 0 0.5px var(--c-border-subtle)',
        padding: '32px',
        display: 'flex', flexDirection: 'column', gap: '16px',
      }}>
        <h2 style={{ margin: 0, fontSize: '18px', fontWeight: 600, color: 'var(--c-text-heading)' }}>
          {t.emailGateTitle}
        </h2>

        <p style={{ margin: 0, fontSize: '13px', lineHeight: 1.6, color: 'var(--c-text-secondary)' }}>
          {t.emailGateDesc(email)}
        </p>

        {!showOtp ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
            <button
              onClick={handleResend}
              disabled={resending || resendCountdown > 0}
              style={{
                height: '36px', borderRadius: '8px', border: '0.5px solid var(--c-border-subtle)',
                background: 'var(--c-bg-sub)', color: 'var(--c-text-primary)',
                fontSize: '13px', fontWeight: 500, cursor: 'pointer',
                opacity: resending || resendCountdown > 0 ? 0.5 : 1,
              }}
            >
              {resentOnce && resendCountdown > 0
                ? `${t.emailGateResent} · ${resendCountdown}s`
                : resendCountdown > 0
                  ? `${resendCountdown}s`
                  : t.emailGateResend}
            </button>

            <button
              onClick={handleCheckNow}
              disabled={checking}
              style={{
                height: '36px', borderRadius: '8px', border: 'none',
                background: 'none', color: 'var(--c-text-tertiary)',
                fontSize: '12px', cursor: 'pointer',
                opacity: checking ? 0.5 : 1,
              }}
            >
              {t.emailGateAlreadyVerified}
            </button>

            <button
              onClick={handleSwitchToOtp}
              style={{
                height: '28px', borderRadius: '8px', border: 'none',
                background: 'none', color: 'var(--c-text-muted)',
                fontSize: '12px', cursor: 'pointer',
              }}
            >
              {t.emailGateUseOtp}
            </button>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
            <input
              type="text"
              inputMode="numeric"
              maxLength={6}
              placeholder={t.otpCodePlaceholder}
              value={otpCode}
              onChange={(e) => setOtpCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
              autoFocus
              autoComplete="one-time-code"
              style={{
                height: '36px', borderRadius: '8px',
                border: '0.5px solid var(--c-border-auth)',
                background: 'var(--c-bg-input)', color: 'var(--c-text-primary)',
                padding: '0 14px', fontSize: '13px', fontWeight: 500, fontFamily: 'inherit',
                outline: 'none',
              }}
            />
            {otpError && (
              <p style={{ margin: 0, fontSize: '12px', color: 'var(--c-danger, #ef4444)' }}>{otpError}</p>
            )}
            <button
              onClick={handleVerifyOtp}
              disabled={otpCode.length !== 6 || otpSubmitting}
              style={{
                height: '36px', borderRadius: '8px', border: 'none',
                background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)',
                fontSize: '13px', fontWeight: 500, cursor: 'pointer',
                opacity: otpCode.length !== 6 || otpSubmitting ? 0.5 : 1,
              }}
            >
              {t.otpVerifyBtn}
            </button>
            <button
              onClick={() => {
                if (otpCountdown > 0 || otpSending) return
                setOtpSending(true)
                sendEmailOTP(email).catch(() => {}).finally(() => {
                  setOtpSending(false)
                  startCountdown(setOtpCountdown, otpTimer)
                })
              }}
              disabled={otpCountdown > 0 || otpSending}
              style={{
                height: '28px', background: 'none', border: 'none',
                fontSize: '12px', color: 'var(--c-text-tertiary)', cursor: 'pointer',
                opacity: otpCountdown > 0 || otpSending ? 0.5 : 1,
              }}
            >
              {otpCountdown > 0 ? t.otpSendingCountdown(otpCountdown) : t.otpSendBtn}
            </button>
          </div>
        )}

        <div style={{ borderTop: '0.5px solid var(--c-border-subtle)', paddingTop: '12px' }}>
          <button
            onClick={onLogout}
            style={{
              width: '100%', height: '32px', borderRadius: '8px', border: 'none',
              background: 'none', color: 'var(--c-text-muted)',
              fontSize: '12px', cursor: 'pointer',
            }}
          >
            {t.logout}
          </button>
        </div>
      </div>
    </div>
  )
}
