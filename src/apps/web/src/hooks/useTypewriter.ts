import { useState, useRef, useEffect } from 'react'

/**
 * 逐字释放 targetText，用于流式消息的打字机效果。
 * 流式阶段按比例步进保持平滑，流结束后加速排空剩余积压。
 */
export function useTypewriter(targetText: string, isComplete = false): string {
  const [displayedLen, setDisplayedLen] = useState(0)
  const lenRef = useRef(0)
  const targetLenRef = useRef(0)
  const rafRef = useRef(0)
  const prevTimeRef = useRef(0)
  const lastGrowthRef = useRef(0)

  targetLenRef.current = targetText.length

  const isEmpty = targetText.length === 0
  useEffect(() => {
    if (isEmpty) {
      lenRef.current = 0
      setDisplayedLen(0)
      prevTimeRef.current = 0
      lastGrowthRef.current = 0
    }
  }, [isEmpty])

  useEffect(() => {
    let prevTarget = 0

    const tick = (now: number) => {
      const current = lenRef.current
      const target = targetLenRef.current

      if (target > prevTarget) {
        lastGrowthRef.current = now
        prevTarget = target
      }

      if (current < target) {
        if (!prevTimeRef.current) prevTimeRef.current = now

        const elapsed = now - prevTimeRef.current

        if (elapsed >= 16) {
          const pending = target - current
          const settled = lastGrowthRef.current > 0 && (now - lastGrowthRef.current) > 120

          // isComplete: 流已结束，立即排空剩余积压，避免被 refreshMessages 截断
          // streaming: 上限 12 chars/frame ≈ 720 chars/s
          // settled:  上限 30 chars/frame ≈ 1800 chars/s
          const step = isComplete
            ? pending
            : settled
              ? Math.min(30, Math.max(3, Math.ceil(pending / 6)))
              : Math.min(12, Math.max(1, Math.ceil(pending / 12)))

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
