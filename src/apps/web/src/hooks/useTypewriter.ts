import { useState, useRef, useEffect } from 'react'

const EWMA_ALPHA = 0.38

/** 尚无「块间隔」样本时的占位间隔（ms），只影响首次 delta 前的 CPS 估计 */
const DEFAULT_GAP_MS = 480

/**
 * 更新 gap EWMA 时对 dt 取底限：同批次多次变长不致把间隔估成 0；
 * 底限取较小值以免在真实高频到达时故意拖慢太多。
 */
const MIN_GAP_SAMPLE_MS = 72

const MIN_CHUNK_EWMA = 6

/** 略慢于可持续率：不求与 delta 同步，优先字里行间的连贯感 */
const JITTER_BRIDGE = 0.86

const MIN_REVEAL_CPS = 18
const MAX_REVEAL_CPS = 420

/** 积压超过约这么多「典型包」时再明显提速，避免模型都结束了正文还落半篇 */
const LAG_HARD_MULT = 2.35
/** 超出硬上限后每多一字约补多少字/秒（尽快吃掉超限 backlog） */
const LAG_CATCHUP_PER_CHAR = 1.75
/** 轻微积压（略超一包）时的温和加码，相对 sustainable 的比例 */
const LAG_SOFT_BOOST = 0.2

/**
 * 流式正文揭示：速率由观测到的 **单包字数 / 包间墙钟时间** 推导（与 message.delta 节律一致），
 * 不是手工拍脑袋的 CPS 或每帧上限。
 */
export function useTypewriter(targetText: string, isComplete = false): string {
  const [displayedLen, setDisplayedLen] = useState(() =>
    isComplete ? targetText.length : 0,
  )
  const lenRef = useRef(0)
  const targetLenRef = useRef(0)
  const prevTickRef = useRef(0)
  const rafRef = useRef(0)
  const isCompleteRef = useRef(isComplete)

  const prevLenRef = useRef(0)
  const lastGrowthAtRef = useRef(0)
  const chunkEwmaRef = useRef(MIN_CHUNK_EWMA)
  const gapEwmaRef = useRef(DEFAULT_GAP_MS)
  /** 是否已过首包（首包只用字数、间隔仍用占位，第二条起 EWMA 间隔） */
  const pastFirstChunkRef = useRef(false)

  useEffect(() => {
    isCompleteRef.current = isComplete
  }, [isComplete])

  useEffect(() => {
    targetLenRef.current = targetText.length
  }, [targetText])

  const isEmpty = targetText.length === 0
  useEffect(() => {
    if (!isEmpty) return
    const id = requestAnimationFrame(() => {
      lenRef.current = 0
      setDisplayedLen(0)
      prevTickRef.current = 0
      prevLenRef.current = 0
      lastGrowthAtRef.current = performance.now()
      chunkEwmaRef.current = MIN_CHUNK_EWMA
      gapEwmaRef.current = DEFAULT_GAP_MS
      pastFirstChunkRef.current = false
    })
    return () => cancelAnimationFrame(id)
  }, [isEmpty])

  useEffect(() => {
    if (isComplete) return
    const len = targetText.length
    const now = performance.now()
    if (len === 0) return

    const prev = prevLenRef.current
    if (len > prev) {
      const delta = len - prev
      const dt = now - lastGrowthAtRef.current
      const a = EWMA_ALPHA

      if (!pastFirstChunkRef.current) {
        chunkEwmaRef.current = Math.max(MIN_CHUNK_EWMA, delta)
        pastFirstChunkRef.current = true
      } else {
        chunkEwmaRef.current = a * delta + (1 - a) * chunkEwmaRef.current
        const gapSample = Math.max(dt, MIN_GAP_SAMPLE_MS)
        gapEwmaRef.current = a * gapSample + (1 - a) * gapEwmaRef.current
      }

      prevLenRef.current = len
      lastGrowthAtRef.current = now
    } else if (len < prev) {
      prevLenRef.current = len
      lastGrowthAtRef.current = now
      chunkEwmaRef.current = MIN_CHUNK_EWMA
      gapEwmaRef.current = DEFAULT_GAP_MS
      pastFirstChunkRef.current = false
    }
  }, [targetText, isComplete])

  useEffect(() => {
    if (!isComplete) return
    const L = targetText.length
    const id = requestAnimationFrame(() => {
      lenRef.current = L
      setDisplayedLen(L)
      prevTickRef.current = 0
    })
    return () => cancelAnimationFrame(id)
  }, [isComplete, targetText])

  useEffect(() => {
    cancelAnimationFrame(rafRef.current)

    const tick = (now: number) => {
      const target = targetLenRef.current
      let current = lenRef.current

      if (current > target) {
        lenRef.current = target
        setDisplayedLen(target)
        current = target
      }

      if (isCompleteRef.current) {
        if (current < target) {
          lenRef.current = target
          setDisplayedLen(target)
        }
        rafRef.current = 0
        return
      }

      if (current < target) {
        if (!prevTickRef.current) prevTickRef.current = now
        const elapsed = now - prevTickRef.current
        if (elapsed >= 16) {
          const pending = target - current
          const chunk = Math.max(chunkEwmaRef.current, MIN_CHUNK_EWMA)
          const gap = Math.max(gapEwmaRef.current, MIN_GAP_SAMPLE_MS)

          let sustainableCps = (1000 * chunk) / gap
          if (sustainableCps < MIN_REVEAL_CPS) sustainableCps = MIN_REVEAL_CPS
          if (sustainableCps > MAX_REVEAL_CPS) sustainableCps = MAX_REVEAL_CPS

          let revealCps = sustainableCps * JITTER_BRIDGE

          const softStart = chunk * 1.08
          if (pending > softStart) {
            revealCps +=
              ((pending - softStart) / Math.max(chunk, 1)) * sustainableCps * LAG_SOFT_BOOST
          }
          const hardLine = chunk * LAG_HARD_MULT
          if (pending > hardLine) {
            revealCps += (pending - hardLine) * LAG_CATCHUP_PER_CHAR
          }

          if (revealCps < MIN_REVEAL_CPS) revealCps = MIN_REVEAL_CPS
          if (revealCps > MAX_REVEAL_CPS) revealCps = MAX_REVEAL_CPS

          const naturalStep = Math.ceil((revealCps * elapsed) / 1000)
          const frameCap = Math.max(
            6,
            Math.ceil(chunk * 1.15),
            Math.min(140, Math.ceil(pending * 0.14)),
          )
          const step = Math.max(1, Math.min(frameCap, naturalStep))

          const next = Math.min(current + step, target)
          lenRef.current = next
          setDisplayedLen(next)
          prevTickRef.current = now
        }
        rafRef.current = requestAnimationFrame(tick)
      } else {
        prevTickRef.current = 0
        rafRef.current = 0
      }
    }

    if (!isComplete && displayedLen >= targetText.length) {
      rafRef.current = 0
      return () => cancelAnimationFrame(rafRef.current)
    }

    rafRef.current = requestAnimationFrame(tick)
    return () => {
      cancelAnimationFrame(rafRef.current)
      rafRef.current = 0
    }
  }, [displayedLen, isComplete, targetText])

  return targetText.slice(0, displayedLen)
}
