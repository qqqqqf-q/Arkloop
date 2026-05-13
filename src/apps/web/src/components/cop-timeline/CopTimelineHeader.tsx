import { useState, useEffect } from 'react'
import { useLocale } from '../../contexts/LocaleContext'
import { useIncrementalTypewriter } from '../../hooks/useIncrementalTypewriter'

export function useThinkingElapsedSeconds(active: boolean, startedAtMs?: number): number {
  const [elapsed, setElapsed] = useState(0)

  useEffect(() => {
    if (!active || !startedAtMs) {
      if (!active) setElapsed(0)
      return
    }
    const tick = () => setElapsed(Math.max(0, Math.round((Date.now() - startedAtMs) / 1000)))
    tick()
    const id = setInterval(tick, 1000)
    return () => clearInterval(id)
  }, [active, startedAtMs])

  return elapsed
}

export function formatThinkingHeaderLabel(thinkingHint: string | undefined, elapsedSeconds: number, t: ReturnType<typeof useLocale>['t']): string {
  if (thinkingHint && thinkingHint.trim() !== '') {
    return `${thinkingHint} ${elapsedSeconds}s`
  }
  return t.copTimelineThinkingForSeconds(elapsedSeconds)
}

export function CopTimelineHeaderLabel({
  text,
  phaseKey,
  shimmer,
  incremental,
  animationSeedText,
}: {
  text: string
  phaseKey: string
  shimmer?: boolean
  incremental?: boolean
  animationSeedText?: string
}) {
  const displayed = useIncrementalTypewriter(text, incremental, animationSeedText)
  return (
    <span
      data-phase={phaseKey}
      className={shimmer ? 'thinking-shimmer' : undefined}
    >
      {incremental ? displayed : text}
    </span>
  )
}
