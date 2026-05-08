/* eslint-disable react-hooks/refs */
import { memo, useEffect, useId, useMemo, useRef } from 'react'
import ReactMarkdown from 'react-markdown'
import type { Components, Options, UrlTransform } from 'react-markdown'
import { rehypeStreamAnimated } from './rehypeStreamAnimated'
import { resolveBlockAnimationMeta } from './streamAnimationMeta'
import { useSmoothStreamContent } from './useSmoothStreamContent'
import { useStreamQueue, type StreamBlock } from './useStreamQueue'

const STREAM_FADE_DURATION = 280
const REVEALED_STREAM_PLUGIN = [rehypeStreamAnimated, { revealed: true }] as const

type RehypePlugins = NonNullable<Options['rehypePlugins']>
type RemarkPlugins = NonNullable<Options['remarkPlugins']>
type PluginList = ReadonlyArray<unknown>

const countChars = (text: string): number => [...text].length

const getNow = (): number => (typeof performance === 'undefined' ? Date.now() : performance.now())

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function isDeepEqualValue(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (Array.isArray(a) || Array.isArray(b)) {
    if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) return false
    return a.every((item, index) => isDeepEqualValue(item, b[index]))
  }
  if (!isRecord(a) || !isRecord(b)) return false

  const keysA = Object.keys(a)
  const keysB = Object.keys(b)
  if (keysA.length !== keysB.length) return false
  return keysA.every((key) => isDeepEqualValue(a[key], b[key]))
}

function isSamePlugin(previousPlugin: unknown, nextPlugin: unknown): boolean {
  const previousTuple = Array.isArray(previousPlugin) ? previousPlugin : [previousPlugin]
  const nextTuple = Array.isArray(nextPlugin) ? nextPlugin : [nextPlugin]
  if (previousTuple.length !== nextTuple.length || previousTuple[0] !== nextTuple[0]) return false
  return isDeepEqualValue(previousTuple.slice(1), nextTuple.slice(1))
}

function isSamePlugins(previousPlugins?: PluginList | null, nextPlugins?: PluginList | null): boolean {
  if (previousPlugins === nextPlugins) return true
  if (!previousPlugins || !nextPlugins) return !previousPlugins && !nextPlugins
  if (previousPlugins.length !== nextPlugins.length) return false
  return previousPlugins.every((plugin, index) => isSamePlugin(plugin, nextPlugins[index]))
}

function useStablePlugins<T extends PluginList>(plugins: T): T {
  const stableRef = useRef<T>(plugins)
  if (!isSamePlugins(stableRef.current, plugins)) stableRef.current = plugins
  return stableRef.current
}

const FENCE_RE = /^ {0,3}(`{3,}|~{3,})/

function splitMarkdownBlocks(content: string): StreamBlock[] {
  const blocks: StreamBlock[] = []
  const lines = content.match(/[^\n]*(?:\n|$)/g) ?? []
  let block = ''
  let blockStartOffset = 0
  let offset = 0
  let fence: { marker: '`' | '~'; length: number } | null = null

  for (const line of lines) {
    if (line === '') break

    if (block === '') blockStartOffset = offset
    block += line

    const lineWithoutBreak = line.replace(/\n$/, '')
    const fenceMatch = FENCE_RE.exec(lineWithoutBreak)
    if (fenceMatch?.[1]) {
      const marker = fenceMatch[1][0] as '`' | '~'
      const length = fenceMatch[1].length
      if (!fence) {
        fence = { marker, length }
      } else if (fence.marker === marker && length >= fence.length) {
        fence = null
      }
    }

    offset += line.length

    if (!fence && lineWithoutBreak.trim() === '') {
      if (block.trim() !== '') {
        blocks.push({ content: block.trimEnd(), startOffset: blockStartOffset })
      }
      block = ''
    }
  }

  if (block.trim() !== '') {
    blocks.push({ content: block.trimEnd(), startOffset: blockStartOffset })
  }
  return blocks
}

type MarkdownBlockProps = {
  children: string
  components: Components
  rehypePlugins: RehypePlugins
  remarkPlugins: RemarkPlugins
  urlTransform: UrlTransform
}

const MarkdownBlock = memo(function MarkdownBlock({
  children,
  components,
  rehypePlugins,
  remarkPlugins,
  urlTransform,
}: MarkdownBlockProps) {
  return (
    <ReactMarkdown
      components={components}
      rehypePlugins={rehypePlugins}
      remarkPlugins={remarkPlugins}
      urlTransform={urlTransform}
    >
      {children}
    </ReactMarkdown>
  )
}, (previousProps, nextProps) => (
  previousProps.children === nextProps.children &&
  previousProps.components === nextProps.components &&
  previousProps.urlTransform === nextProps.urlTransform &&
  isSamePlugins(previousProps.rehypePlugins, nextProps.rehypePlugins) &&
  isSamePlugins(previousProps.remarkPlugins, nextProps.remarkPlugins)
))

export type StreamingMarkdownProps = {
  children: string
  components: Components
  rehypePlugins: RehypePlugins
  remarkPlugins: RemarkPlugins
  urlTransform: UrlTransform
}

// Adapted from @lobehub/ui Streamdown (MIT).
export function StreamingMarkdown({
  children,
  components,
  rehypePlugins,
  remarkPlugins,
  urlTransform,
}: StreamingMarkdownProps) {
  const generatedId = useId()
  const smoothedContent = useSmoothStreamContent(children, { preset: 'balanced' })
  const blocks = useMemo(() => splitMarkdownBlocks(smoothedContent), [smoothedContent])
  const stableRehypePlugins = useStablePlugins(rehypePlugins)
  const stableRemarkPlugins = useStablePlugins(remarkPlugins)
  const { charDelay, getBlockState } = useStreamQueue(blocks)
  const blockCharDelayRef = useRef<Map<number, number>>(new Map())
  const blockBirthsRef = useRef<Map<number, number[]>>(new Map())
  const renderNow = getNow()

  const birthsForRender = useMemo(() => {
    const nextBirths = new Map<number, number[]>()
    const previousBirths = blockBirthsRef.current

    for (const [index, block] of blocks.entries()) {
      const state = getBlockState(index)
      if (state === 'queued') continue

      const blockCharCount = countChars(block.content)
      const previous = previousBirths.get(block.startOffset)
      let births: number[]

      if (previous && previous.length === blockCharCount) {
        births = previous
      } else if (previous && previous.length > blockCharCount) {
        births = previous.slice(0, blockCharCount)
      } else {
        births = previous ? previous.slice() : []
        const cap = renderNow + STREAM_FADE_DURATION
        for (let i = births.length; i < blockCharCount; i += 1) {
          const previousBirth = i > 0 ? (births[i - 1] ?? renderNow) : renderNow - charDelay
          const chained = previousBirth + charDelay
          births.push(Math.min(cap, Math.max(chained, renderNow)))
        }
      }

      nextBirths.set(block.startOffset, births)
    }

    return nextBirths
  }, [blocks, charDelay, getBlockState, renderNow])

  const blockAnimationMeta = useMemo(() => {
    const nextBlockCharDelay = new Map<number, number>()
    const nextBlockAnimationMeta = new Map<number, ReturnType<typeof resolveBlockAnimationMeta>>()

    for (const [index, block] of blocks.entries()) {
      const state = getBlockState(index)
      const births = birthsForRender.get(block.startOffset)
      const lastBirthTs = births && births.length > 0 ? (births.at(-1) ?? renderNow) : renderNow
      const animationMeta = resolveBlockAnimationMeta({
        currentCharDelay: charDelay,
        fadeDuration: STREAM_FADE_DURATION,
        lastElapsedMs: renderNow - lastBirthTs,
        previousCharDelay: blockCharDelayRef.current.get(block.startOffset),
        state,
      })

      nextBlockCharDelay.set(block.startOffset, animationMeta.charDelay)
      nextBlockAnimationMeta.set(block.startOffset, animationMeta)
    }

    return {
      blockAnimationMeta: nextBlockAnimationMeta,
      blockCharDelay: nextBlockCharDelay,
    }
  }, [birthsForRender, blocks, charDelay, getBlockState, renderNow])

  useEffect(() => {
    blockCharDelayRef.current = blockAnimationMeta.blockCharDelay
    blockBirthsRef.current = birthsForRender
  }, [birthsForRender, blockAnimationMeta.blockCharDelay])

  return (
    <div className="md-stream-content">
      {blocks.map((block, index) => {
        const state = getBlockState(index)
        if (state === 'queued') return null

        const animationMeta = blockAnimationMeta.blockAnimationMeta.get(block.startOffset)
        if (!animationMeta) return null

        const births = birthsForRender.get(block.startOffset)
        const plugins = (animationMeta.settled
          ? [...stableRehypePlugins, REVEALED_STREAM_PLUGIN]
          : [
              ...stableRehypePlugins,
              [rehypeStreamAnimated, { births, fadeDuration: STREAM_FADE_DURATION, nowMs: renderNow }],
            ]) as RehypePlugins

        return (
          <MarkdownBlock
            components={components}
            key={`${generatedId}-${block.startOffset}`}
            rehypePlugins={plugins}
            remarkPlugins={stableRemarkPlugins}
            urlTransform={urlTransform}
          >
            {block.content}
          </MarkdownBlock>
        )
      })}
    </div>
  )
}
