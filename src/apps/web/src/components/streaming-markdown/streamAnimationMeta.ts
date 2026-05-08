import type { StreamBlockState } from './useStreamQueue'

type ResolveBlockAnimationMetaOptions = {
  currentCharDelay: number
  fadeDuration: number
  lastElapsedMs: number
  previousCharDelay?: number
  state: StreamBlockState
}

export type BlockAnimationMeta = {
  charDelay: number
  settled: boolean
}

const isActiveBlock = (state: StreamBlockState): boolean => state === 'animating' || state === 'streaming'

// Adapted from @lobehub/ui Streamdown (MIT).
export function resolveBlockAnimationMeta({
  currentCharDelay,
  fadeDuration,
  lastElapsedMs,
  previousCharDelay,
  state,
}: ResolveBlockAnimationMetaOptions): BlockAnimationMeta {
  const charDelay = isActiveBlock(state) ? currentCharDelay : (previousCharDelay ?? currentCharDelay)
  return {
    charDelay,
    settled: state === 'revealed' && lastElapsedMs >= fadeDuration,
  }
}
