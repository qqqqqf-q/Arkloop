import { useState, useEffect, useRef, Fragment } from 'react'
import { X } from 'lucide-react'
import { getActiveTimeZone } from '@arkloop/shared'
import { listNotifications, markAllNotificationsRead, type NotificationItem } from '../api'
import { useLocale } from '../contexts/LocaleContext'

type Props = {
  accessToken: string
  onClose: () => void
  onMarkedRead: () => void
}

function formatDate(iso: string, locale: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return new Intl.DateTimeFormat(locale === 'zh' ? 'zh-CN' : 'en-US', {
    year: 'numeric',
    month: locale === 'zh' ? 'numeric' : 'short',
    day: 'numeric',
    timeZone: getActiveTimeZone(),
  }).format(d)
}

function resolveI18nField(
  payload: Record<string, unknown> | undefined,
  field: 'title' | 'body',
  locale: string,
  fallback: string,
): string {
  const i18n = payload?.i18n as Record<string, Record<string, string>> | undefined
  return i18n?.[field]?.[locale] ?? fallback
}

// px
const ITEM_GAP_TOP = 24
const ITEM_GAP_BOTTOM = 24

export function NotificationsPanel({ accessToken, onClose, onMarkedRead }: Props) {
  const { t, locale } = useLocale()
  const [items, setItems] = useState<NotificationItem[]>([])
  const [loadError, setLoadError] = useState(false)
  const [loading, setLoading] = useState(true)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  useEffect(() => {
    void (async () => {
      try {
        const resp = await listNotifications(accessToken)
        if (mountedRef.current) setItems(resp.data ?? [])
      } catch (err) {
        console.error('notifications load failed', err)
        if (mountedRef.current) setLoadError(true)
      } finally {
        if (mountedRef.current) setLoading(false)
      }
    })()
  }, [accessToken])

  useEffect(() => {
    void (async () => {
      try {
        await markAllNotificationsRead(accessToken)
        onMarkedRead()
      } catch (err) {
        console.error('markAllNotificationsRead failed', err)
      }
    })()
  }, [accessToken, onMarkedRead])

  return (
    <div
      style={{
        position: 'absolute',
        inset: 0,
        zIndex: 30,
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        background: 'var(--c-bg-page)',
      }}
    >
      {/* Close */}
      <div style={{ display: 'flex', justifyContent: 'flex-end', padding: '12px 16px' }}>
        <button
          onClick={onClose}
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 32,
            height: 32,
            borderRadius: 8,
            border: 'none',
            background: 'transparent',
            cursor: 'pointer',
            color: 'var(--c-text-tertiary)',
          }}
        >
          <X size={20} />
        </button>
      </div>

      {/* Title */}
      <div style={{ textAlign: 'center', padding: '0 16px 24px' }}>
        <h1 style={{ fontSize: 24, fontWeight: 600, color: 'var(--c-text-primary)', margin: 0 }}>
          {t.notificationsTitle}
        </h1>
      </div>

      {/* List */}
      <div style={{ flex: 1, overflowY: 'auto' }}>
        <div style={{ maxWidth: 720, margin: '0 auto', padding: '0 24px' }}>
          {loading ? (
            <div style={{ display: 'flex', justifyContent: 'center', padding: '80px 0', color: 'var(--c-text-muted)', fontSize: 14 }}>
              {t.loading}
            </div>
          ) : loadError ? (
            <div style={{ display: 'flex', justifyContent: 'center', padding: '80px 0', color: 'var(--c-status-error-text)', fontSize: 14 }}>
              {t.requestFailed}
            </div>
          ) : items.length === 0 ? (
            <div style={{ display: 'flex', justifyContent: 'center', padding: '80px 0', color: 'var(--c-text-muted)', fontSize: 14 }}>
              {t.notificationsEmpty}
            </div>
          ) : (
            items.map((n, i) => {
              const title = resolveI18nField(n.payload, 'title', locale, n.title)
              const body = n.body ? resolveI18nField(n.payload, 'body', locale, n.body) : null
              return (
                <Fragment key={n.id}>
                  {/* Divider between items */}
                  {i > 0 && <div style={{ height: 1, background: 'var(--c-border)' }} />}

                  {/* Item */}
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'flex-start',
                      gap: 32,
                      paddingTop: ITEM_GAP_TOP,
                      paddingBottom: ITEM_GAP_BOTTOM,
                      opacity: n.read_at ? 0.6 : 1,
                    }}
                  >
                    {/* Date */}
                    <span
                      style={{
                        flexShrink: 0,
                        fontSize: 14,
                        lineHeight: '22px',
                        color: 'var(--c-text-muted)',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {formatDate(n.created_at, locale)}
                    </span>

                    {/* Content */}
                    <div style={{ minWidth: 0, flex: 1 }}>
                      <p
                        style={{
                          margin: 0,
                          fontSize: 16,
                          lineHeight: '22px',
                          color: 'var(--c-text-primary)',
                          fontWeight: n.read_at ? 400 : 600,
                        }}
                      >
                        {title}
                      </p>
                      {body && (
                        <p
                          style={{
                            margin: '6px 0 0',
                            fontSize: 14,
                            lineHeight: '20px',
                            color: 'var(--c-text-muted)',
                          }}
                        >
                          {body}
                        </p>
                      )}
                    </div>
                  </div>
                </Fragment>
              )
            })
          )}
        </div>
      </div>
    </div>
  )
}
