import { useMemo, useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useLocale } from '../contexts/LocaleContext'

export type AppError = {
  message: string
  traceId?: string
  code?: string
}

type FriendlyText = { zh: string; en: string }

const FRIENDLY_ERROR_MESSAGES: Record<string, FriendlyText> = {
  'internal.error': { zh: '服务出错，请稍后再试', en: 'Something went wrong. Please try again.' },
  'database.not_configured': { zh: '服务暂不可用', en: 'Service unavailable.' },
  'service_unavailable': { zh: '服务暂不可用', en: 'Service unavailable.' },
  'validation.error': { zh: '请求参数有误', en: 'Invalid request.' },
  'bad_request': { zh: '请求参数有误', en: 'Invalid request.' },
  'policy.denied': { zh: '无权限', en: 'Access denied.' },
  'auth.forbidden': { zh: '无权限', en: 'Access denied.' },
  'auth.invalid_credentials': { zh: '账号或密码错误', en: 'Invalid credentials.' },
  'auth.user_suspended': { zh: '账号已停用', en: 'Account suspended.' },
  'auth.email_not_verified': { zh: '邮箱未验证', en: 'Email not verified.' },
  'auth.captcha_invalid': { zh: '人机验证失败', en: 'Captcha verification failed.' },
  'auth.otp_invalid': { zh: '验证码无效或已过期', en: 'Code invalid or expired.' },
  'auth.token_expired': { zh: '登录已过期，请重新登录', en: 'Session expired. Please log in again.' },
  'auth.invalid_token': { zh: '登录已过期，请重新登录', en: 'Session expired. Please log in again.' },
  'auth.missing_token': { zh: '未登录', en: 'Not authenticated.' },
  'auth.invalid_authorization': { zh: '未登录', en: 'Not authenticated.' },
}

function hasCjk(text: string): boolean {
  return /[\u4e00-\u9fff]/.test(text)
}

function isNetworkErrorMessage(text: string): boolean {
  const m = text.trim().toLowerCase()
  if (!m) return false
  return m.includes('failed to fetch') || m.includes('networkerror') || m.includes('network error') || m.includes('load failed')
}

export function ErrorCallout({ error }: { error: AppError }) {
  const { locale, t } = useLocale()
  const [detailsOpen, setDetailsOpen] = useState(false)

  const rawMessage = useMemo(() => (error.message ?? '').trim(), [error.message])
  const code = useMemo(() => (typeof error.code === 'string' ? error.code.trim() : ''), [error.code])
  const traceId = useMemo(() => (typeof error.traceId === 'string' ? error.traceId.trim() : ''), [error.traceId])

  const title = useMemo(() => {
    if (rawMessage && hasCjk(rawMessage)) return rawMessage
    if (code && FRIENDLY_ERROR_MESSAGES[code]) {
      return FRIENDLY_ERROR_MESSAGES[code][locale]
    }
    if (rawMessage && isNetworkErrorMessage(rawMessage)) {
      return locale === 'zh' ? '网络异常，请重试' : 'Network error. Please try again.'
    }
    return t.requestFailed
  }, [code, locale, rawMessage, t.requestFailed])

  const showDetails = useMemo(() => {
    if (code || traceId) return true
    if (rawMessage && rawMessage !== title) return true
    return false
  }, [code, rawMessage, title, traceId])

  const labels = useMemo(() => {
    if (locale === 'zh') {
      return { details: '详情', raw: '原始信息', code: '错误码', trace: 'Trace ID' }
    }
    return { details: 'Details', raw: 'Raw message', code: 'Code', trace: 'Trace ID' }
  }, [locale])

  return (
    <div
      className="mt-3 rounded-xl border px-4 py-3 text-sm"
      style={{
        background: 'var(--c-error-bg)',
        borderColor: 'var(--c-error-border)',
      }}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="font-medium" style={{ color: 'var(--c-error-text)' }}>
          {title}
        </div>
        {showDetails && (
          <button
            type="button"
            onClick={() => setDetailsOpen((v) => !v)}
            className="flex items-center gap-1 whitespace-nowrap text-xs"
            style={{
              background: 'transparent',
              border: 'none',
              padding: 0,
              cursor: 'pointer',
              color: 'var(--c-error-subtext)',
              opacity: 0.9,
            }}
          >
            {detailsOpen ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            {labels.details}
          </button>
        )}
      </div>

      {showDetails && detailsOpen && (
        <div
          className="mt-1.5 space-y-0.5 text-xs"
          style={{ color: 'var(--c-error-subtext)' }}
        >
          {rawMessage && rawMessage !== title && (
            <div className="break-words">
              <span className="font-mono">{labels.raw}: </span>
              <span>{rawMessage}</span>
            </div>
          )}
          {code && <div className="font-mono break-words">{labels.code}: {code}</div>}
          {traceId && <div className="font-mono break-words">{labels.trace}: {traceId}</div>}
        </div>
      )}
    </div>
  )
}
