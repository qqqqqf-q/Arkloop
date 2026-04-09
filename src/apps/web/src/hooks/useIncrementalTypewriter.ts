import { useEffect, useMemo, useRef, useState } from 'react'

type IncrementalSpec = {
  from: string
  to: string
  prefix: string
  suffix: string
  nextSegment: string
  animate: boolean
  revealIntervalMs: number
}

const CHAR_REVEAL_MS = 28
const MIN_REVEAL_WINDOW_MS = 140

function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined'
    && typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

function commonPrefixLength(a: string, b: string): number {
  const limit = Math.min(a.length, b.length)
  let idx = 0
  while (idx < limit && a[idx] === b[idx]) idx += 1
  return idx
}

function commonSuffixLength(a: string, b: string, prefixLen: number): number {
  const aLimit = a.length - prefixLen
  const bLimit = b.length - prefixLen
  const limit = Math.min(aLimit, bLimit)
  let idx = 0
  while (
    idx < limit
    && a[a.length - 1 - idx] === b[b.length - 1 - idx]
  ) {
    idx += 1
  }
  return idx
}

function buildSpec(from: string, to: string, animate: boolean): IncrementalSpec {
  if (!animate || from === to || to.length === 0) {
    return {
      from: to,
      to,
      prefix: to,
      suffix: '',
      nextSegment: to,
      animate: false,
      revealIntervalMs: CHAR_REVEAL_MS,
    }
  }

  const prefixLen = commonPrefixLength(from, to)
  const suffixLen = commonSuffixLength(from, to, prefixLen)
  const nextSegment = to.slice(prefixLen, to.length - suffixLen)

  if (nextSegment.length === 0) {
    return {
      from: to,
      to,
      prefix: to,
      suffix: '',
      nextSegment: to,
      animate: false,
      revealIntervalMs: CHAR_REVEAL_MS,
    }
  }

  return {
    from,
    to,
    prefix: to.slice(0, prefixLen),
    suffix: suffixLen > 0 ? to.slice(to.length - suffixLen) : '',
    nextSegment,
    animate: true,
    revealIntervalMs: Math.max(
      CHAR_REVEAL_MS,
      Math.ceil(MIN_REVEAL_WINDOW_MS / nextSegment.length),
    ),
  }
}

export function useIncrementalTypewriter(text: string, enabled = true): string {
  const animate = enabled && !prefersReducedMotion()
  const previousTargetRef = useRef(text)
  const [spec, setSpec] = useState<IncrementalSpec>(() => buildSpec('', text, animate))
  const [revealedLen, setRevealedLen] = useState(() => (spec.animate ? 0 : spec.to.length))

  useEffect(() => {
    if (!animate) {
      previousTargetRef.current = text
      setSpec(buildSpec(text, text, false))
      setRevealedLen(text.length)
      return
    }
    const previous = previousTargetRef.current
    if (previous === text) return
    previousTargetRef.current = text
    setSpec(buildSpec(previous, text, true))
    setRevealedLen(0)
  }, [animate, text])

  useEffect(() => {
    if (!spec.animate) {
      setRevealedLen(spec.to.length)
      return
    }
    let frame = 0
    let startAt = -1

    const tick = (now: number) => {
      if (startAt < 0) startAt = now
      const elapsed = now - startAt
      const nextLen = Math.min(
        spec.nextSegment.length,
        spec.nextSegment.length === 1
          ? Math.max(0, Math.floor(elapsed / spec.revealIntervalMs))
          : Math.max(1, Math.floor(elapsed / spec.revealIntervalMs) + 1),
      )
      setRevealedLen((current) => (current === nextLen ? current : nextLen))
      if (nextLen < spec.nextSegment.length) {
        frame = requestAnimationFrame(tick)
      }
    }

    frame = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame)
  }, [spec])

  return useMemo(() => {
    if (!spec.animate) return spec.to
    if (revealedLen === 0) return spec.from
    if (revealedLen >= spec.nextSegment.length) return spec.to
    return `${spec.prefix}${spec.nextSegment.slice(0, revealedLen)}${spec.suffix}`
  }, [revealedLen, spec])
}
