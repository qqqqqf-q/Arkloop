import { useState, useRef, useEffect } from 'react'

/**
 * 逐字释放 targetText，用于流式消息的打字机效果。
 * 速度根据积压长度自适应：积压多时加速追赶，积压少时放慢以获得自然手感。
 */
export function useTypewriter(targetText: string): string {
  const [displayedLen, setDisplayedLen] = useState(0)
  const lenRef = useRef(0)
  const targetLenRef = useRef(0)
  const rafRef = useRef(0)
  const prevTimeRef = useRef(0)

  targetLenRef.current = targetText.length

  const isEmpty = targetText.length === 0
  useEffect(() => {
    if (isEmpty) {
      lenRef.current = 0
      setDisplayedLen(0)
      prevTimeRef.current = 0
    }
  }, [isEmpty])

  useEffect(() => {
    const tick = (now: number) => {
      const current = lenRef.current
      const target = targetLenRef.current

      if (current < target) {
        if (!prevTimeRef.current) prevTimeRef.current = now

        const elapsed = now - prevTimeRef.current
        const pending = target - current

        // 积压 >100 字符: ~500 chars/s (快速追赶)
        // 积压 40-100:    ~166 chars/s
        // 积压 15-40:     ~83 chars/s
        // 积压 <15:       ~50 chars/s (自然手感)
        const interval = pending > 100 ? 2 : pending > 40 ? 6 : pending > 15 ? 12 : 20

        if (elapsed >= interval) {
          const step = Math.max(1, Math.floor(elapsed / interval))
          const next = Math.min(current + step, target)
          lenRef.current = next
          setDisplayedLen(next)
          prevTimeRef.current = now
        }
      } else {
        prevTimeRef.current = 0
      }

      rafRef.current = requestAnimationFrame(tick)
    }

    rafRef.current = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(rafRef.current)
  }, [])

  return targetText.slice(0, displayedLen)
}
