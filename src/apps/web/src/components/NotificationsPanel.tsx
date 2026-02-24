import { useState, useEffect, useCallback, useRef } from 'react'
import { X } from 'lucide-react'
import { listNotifications, markAllNotificationsRead, type NotificationItem } from '../api'
import { useLocale } from '../contexts/LocaleContext'

type Props = {
  accessToken: string
  onClose: () => void
  onMarkedRead: () => void
}

function formatDate(iso: string, locale: string): string {
  const d = new Date(iso)
  if (locale === 'zh') {
    const y = d.getFullYear()
    const m = d.getMonth() + 1
    const day = d.getDate()
    return `${y}年${m}月${day}日`
  }
  return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' })
}

// 从 payload.i18n 中取当前语言版本，找不到则 fallback 到原始字段。
function resolveI18nField(
  payload: Record<string, unknown> | undefined,
  field: 'title' | 'body',
  locale: string,
  fallback: string,
): string {
  const i18n = payload?.i18n as Record<string, Record<string, string>> | undefined
  return i18n?.[field]?.[locale] ?? fallback
}

export function NotificationsPanel({ accessToken, onClose, onMarkedRead }: Props) {
  const { t, locale } = useLocale()
  const [items, setItems] = useState<NotificationItem[]>([])
  const [loading, setLoading] = useState(true)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  // 拉取全部通知用于展示
  useEffect(() => {
    void (async () => {
      try {
        const resp = await listNotifications(accessToken)
        if (mountedRef.current) setItems(resp.data ?? [])
      } catch {
        // 静默处理
      } finally {
        if (mountedRef.current) setLoading(false)
      }
    })()
  }, [accessToken])

  // 打开面板即标记全部已读，独立于拉取流程
  useEffect(() => {
    void (async () => {
      try {
        await markAllNotificationsRead(accessToken)
        onMarkedRead()
      } catch {
        // 静默处理
      }
    })()
  }, [accessToken, onMarkedRead])

  return (
    <div className="absolute inset-0 z-30 flex flex-col overflow-hidden bg-[var(--c-bg-page)]">
      <div className="flex items-center justify-end px-4 py-3">
        <button
          onClick={onClose}
          className="flex h-8 w-8 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]"
        >
          <X size={20} />
        </button>
      </div>

      <div className="flex flex-col items-center gap-6 px-4 pb-6">
        <h1 className="text-2xl font-semibold text-[var(--c-text-primary)]">
          {t.notificationsTitle}
        </h1>
      </div>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[720px] px-6">
          {loading ? (
            <div className="flex items-center justify-center py-20">
              <span className="text-sm text-[var(--c-text-muted)]">{t.loading}</span>
            </div>
          ) : items.length === 0 ? (
            <div className="flex items-center justify-center py-20">
              <span className="text-sm text-[var(--c-text-muted)]">{t.notificationsEmpty}</span>
            </div>
          ) : (
            items.map((n) => (
              <div
                key={n.id}
                className={`flex items-start gap-8 border-b border-[var(--c-border)] py-6${n.read_at ? ' opacity-60' : ''}`}
              >
                <span className="mt-0.5 shrink-0 text-sm text-[var(--c-text-muted)]">
                  {formatDate(n.created_at, locale)}
                </span>
                <div className="min-w-0 flex-1">
                  <p className={`text-base text-[var(--c-text-primary)]${n.read_at ? '' : ' font-semibold'}`}>
                    {resolveI18nField(n.payload, 'title', locale, n.title)}
                  </p>
                  {n.body && (
                    <p className="mt-1.5 text-sm text-[var(--c-text-muted)]">
                      {resolveI18nField(n.payload, 'body', locale, n.body)}
                    </p>
                  )}
                </div>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  )
}
