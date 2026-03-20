import { useState, useEffect, useCallback, useRef } from 'react'
import { Bell } from 'lucide-react'
import { listNotifications, type NotificationItem } from '../api'

const POLL_INTERVAL_MS = 30_000

type Props = {
  accessToken: string
  onClick: () => void
  refreshKey?: number
  title?: string
}

export function NotificationBell({ accessToken, onClick, refreshKey, title }: Props) {
  const [items, setItems] = useState<NotificationItem[]>([])
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  const fetchNotifications = useCallback(async () => {
    try {
      const resp = await listNotifications(accessToken, { unreadOnly: true })
      if (mountedRef.current) {
        setItems(resp.data ?? [])
      }
    } catch {
      // 静默处理
    }
  }, [accessToken])

  useEffect(() => {
    const kick = requestAnimationFrame(() => void fetchNotifications())
    const timer = setInterval(() => {
      requestAnimationFrame(() => void fetchNotifications())
    }, POLL_INTERVAL_MS)
    return () => {
      cancelAnimationFrame(kick)
      clearInterval(timer)
    }
  }, [fetchNotifications, refreshKey])

  const unreadCount = items.length

  return (
    <button
      onClick={onClick}
      title={title}
      className="relative flex h-8 w-8 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
    >
      <Bell size={18} />
      {unreadCount > 0 && (
        <span className="absolute right-0.5 top-0.5 flex h-3.5 min-w-[14px] items-center justify-center rounded-full bg-[var(--c-status-error-bg,#ef4444)] px-0.5 text-[9px] font-medium text-white">
          {unreadCount > 99 ? '99+' : unreadCount}
        </span>
      )}
    </button>
  )
}
