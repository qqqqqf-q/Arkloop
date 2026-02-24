import { useState, useEffect, useCallback, useRef } from 'react'
import { Bell, Check } from 'lucide-react'
import { listNotifications, markNotificationRead, type NotificationItem } from '../api'
import { useLocale } from '../contexts/LocaleContext'

const POLL_INTERVAL_MS = 30_000

type Props = {
  accessToken: string
}

export function NotificationBell({ accessToken }: Props) {
  const { t } = useLocale()
  const [items, setItems] = useState<NotificationItem[]>([])
  const [open, setOpen] = useState(false)
  const mountedRef = useRef(true)
  const panelRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  const fetchNotifications = useCallback(async () => {
    try {
      const resp = await listNotifications(accessToken, 'broadcast')
      if (mountedRef.current) {
        setItems(resp.data ?? [])
      }
    } catch {
      // 静默处理
    }
  }, [accessToken])

  useEffect(() => {
    void fetchNotifications()
    const timer = setInterval(() => void fetchNotifications(), POLL_INTERVAL_MS)
    return () => clearInterval(timer)
  }, [fetchNotifications])

  // 点击外部关闭面板
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  const handleMarkRead = useCallback(async (id: string) => {
    try {
      await markNotificationRead(accessToken, id)
      setItems((prev) => prev.filter((n) => n.id !== id))
    } catch {
      // 静默处理
    }
  }, [accessToken])

  const unreadCount = items.length

  return (
    <div className="relative" ref={panelRef}>
      <button
        onClick={() => setOpen((v) => !v)}
        className="relative flex h-5 w-5 items-center justify-center text-[var(--c-text-secondary)] opacity-80 transition-opacity hover:opacity-100"
      >
        <Bell size={20} />
        {unreadCount > 0 && (
          <span className="absolute -right-0.5 -top-0.5 flex h-4 min-w-[16px] items-center justify-center rounded-full bg-[var(--c-status-error-bg,#ef4444)] px-1 text-[10px] font-medium text-white">
            {unreadCount > 99 ? '99+' : unreadCount}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute right-0 top-10 z-50 w-[320px] rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-page)] shadow-lg">
          <div className="border-b border-[var(--c-border)] px-4 py-2.5">
            <span className="text-sm font-medium text-[var(--c-text-primary)]">
              {t.notificationsTitle}
            </span>
          </div>
          <div className="max-h-[360px] overflow-y-auto">
            {items.length === 0 ? (
              <div className="flex items-center justify-center py-8">
                <span className="text-xs text-[var(--c-text-muted)]">{t.notificationsEmpty}</span>
              </div>
            ) : (
              items.map((n) => (
                <div
                  key={n.id}
                  className="flex items-start gap-2 border-b border-[var(--c-border)] px-4 py-3 transition-colors hover:bg-[var(--c-bg-deep)]"
                >
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium text-[var(--c-text-primary)]">{n.title}</p>
                    {n.body && (
                      <p className="mt-0.5 text-xs text-[var(--c-text-muted)] line-clamp-2">
                        {n.body}
                      </p>
                    )}
                    <p className="mt-1 text-[10px] text-[var(--c-text-muted)]">
                      {new Date(n.created_at).toLocaleString()}
                    </p>
                  </div>
                  <button
                    onClick={() => void handleMarkRead(n.id)}
                    className="mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
                    title={t.notificationsMarkRead}
                  >
                    <Check size={14} />
                  </button>
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  )
}
