/* eslint-disable react-hooks/refs */
import { useCallback, useEffect, useRef, useState } from 'react'

export type StreamBlock = {
  content: string
  startOffset: number
}

export type StreamBlockState = 'revealed' | 'animating' | 'streaming' | 'queued'

const BASE_DELAY = 18
const ACCELERATION_FACTOR = 0.3
const MAX_BLOCK_DURATION = 3000
const FADE_DURATION = 280

const countChars = (text: string): number => [...text].length

function computeCharDelay(queueLength: number, charCount: number): number {
  const acceleration = 1 + queueLength * ACCELERATION_FACTOR
  const delay = BASE_DELAY / acceleration
  return Math.min(delay, MAX_BLOCK_DURATION / Math.max(charCount, 1))
}

type UseStreamQueueReturn = {
  charDelay: number
  getBlockState: (index: number) => StreamBlockState
  queueLength: number
}

// Adapted from @lobehub/ui Streamdown (MIT).
export function useStreamQueue(blocks: StreamBlock[]): UseStreamQueueReturn {
  const [revealedCount, setRevealedCount] = useState(0)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const previousBlocksLengthRef = useRef(0)
  const minRevealedRef = useRef(0)

  if (blocks.length === 0 && previousBlocksLengthRef.current !== 0) {
    minRevealedRef.current = 0
  }
  if (blocks.length > previousBlocksLengthRef.current && previousBlocksLengthRef.current > 0) {
    const previousTail = previousBlocksLengthRef.current - 1
    minRevealedRef.current = Math.max(minRevealedRef.current, previousTail + 1)
  }
  previousBlocksLengthRef.current = blocks.length

  useEffect(() => {
    if (blocks.length !== 0) return

    setRevealedCount(0)
    minRevealedRef.current = 0
    if (timerRef.current) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
  }, [blocks.length])

  const effectiveRevealedCount = Math.max(revealedCount, minRevealedRef.current)
  const tailIndex = blocks.length - 1

  const getBlockState = useCallback((index: number): StreamBlockState => {
    if (index < effectiveRevealedCount) return 'revealed'
    if (index === effectiveRevealedCount && index < tailIndex) return 'animating'
    if (index === effectiveRevealedCount && index === tailIndex) return 'streaming'
    return 'queued'
  }, [effectiveRevealedCount, tailIndex])

  const queueLength = Math.max(0, tailIndex - effectiveRevealedCount - 1)
  const animatingIndex = effectiveRevealedCount < tailIndex ? effectiveRevealedCount : -1
  const animatingCharCount = animatingIndex >= 0 ? countChars(blocks[animatingIndex]?.content ?? '') : 0
  const streamingIndex = animatingIndex < 0 && tailIndex >= effectiveRevealedCount ? tailIndex : -1
  const activeIndex = animatingIndex >= 0 ? animatingIndex : streamingIndex
  const activeCharCount = activeIndex >= 0 ? countChars(blocks[activeIndex]?.content ?? '') : 0

  const frozenRef = useRef({ delay: BASE_DELAY, index: -1 })
  if (activeIndex >= 0 && activeIndex !== frozenRef.current.index) {
    frozenRef.current = {
      delay: computeCharDelay(queueLength, activeCharCount),
      index: activeIndex,
    }
  }
  const charDelay = activeIndex >= 0 ? frozenRef.current.delay : BASE_DELAY

  const handleAnimationDone = useCallback(() => {
    setRevealedCount(effectiveRevealedCount + 1)
  }, [effectiveRevealedCount])

  useEffect(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }

    if (animatingIndex < 0) return

    const totalTime = Math.max(0, (animatingCharCount - 1) * charDelay) + FADE_DURATION
    timerRef.current = setTimeout(handleAnimationDone, totalTime)

    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current)
        timerRef.current = null
      }
    }
  }, [animatingCharCount, animatingIndex, charDelay, handleAnimationDone])

  return { charDelay, getBlockState, queueLength }
}
