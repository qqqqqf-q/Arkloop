import { useEffect, useRef } from 'react'

declare global {
  interface Window {
    turnstile?: {
      render: (container: string | HTMLElement, options: TurnstileOptions) => string
      remove: (widgetId: string) => void
      reset: (widgetId: string) => void
    }
  }
}

interface TurnstileOptions {
  sitekey: string
  callback: (token: string) => void
  'expired-callback'?: () => void
  'error-callback'?: () => void
  theme?: 'light' | 'dark' | 'auto'
  size?: 'normal' | 'compact' | 'flexible'
}

interface TurnstileProps {
  siteKey: string
  onSuccess: (token: string) => void
  onExpire?: () => void
}

export function Turnstile({ siteKey, onSuccess, onExpire }: TurnstileProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const widgetIdRef = useRef<string | null>(null)
  const onSuccessRef = useRef(onSuccess)
  const onExpireRef = useRef(onExpire)
  useEffect(() => {
    onSuccessRef.current = onSuccess
    onExpireRef.current = onExpire
  })

  useEffect(() => {
    const container = containerRef.current
    if (!container || !siteKey) return

    let intervalId: ReturnType<typeof setInterval> | null = null
    let active = true

    const render = () => {
      if (!active || !container || !window.turnstile) return
      if (widgetIdRef.current) {
        try { window.turnstile.remove(widgetIdRef.current) } catch { /* noop */ }
        widgetIdRef.current = null
      }
      try {
        widgetIdRef.current = window.turnstile.render(container, {
          sitekey: siteKey,
          size: 'flexible',
          callback: (token) => onSuccessRef.current(token),
          'expired-callback': () => onExpireRef.current?.(),
          'error-callback': () => onExpireRef.current?.(),
        })
      } catch { /* noop */ }
    }

    if (window.turnstile) {
      render()
    } else {
      intervalId = setInterval(() => {
        if (window.turnstile) {
          if (intervalId) clearInterval(intervalId)
          intervalId = null
          render()
        }
      }, 100)
    }

    return () => {
      active = false
      if (intervalId) clearInterval(intervalId)
      if (widgetIdRef.current && window.turnstile) {
        try { window.turnstile.remove(widgetIdRef.current) } catch { /* noop */ }
        widgetIdRef.current = null
      }
    }
  }, [siteKey])

  return <div ref={containerRef} style={{ width: '100%' }} />
}
