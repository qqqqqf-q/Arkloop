import { memo, useState, useEffect, useRef, useLayoutEffect, useCallback } from 'react'
import { motion, useReducedMotion } from 'framer-motion'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { segmentCompletedTitle, type CopSubSegment, type ResolvedPool } from '../../copSubSegment'
import type { CodeExecution } from '../CodeExecutionCard'
import type { SubAgentRef } from '../../storage'

import { CopThoughtSummaryRow, TimelineNarrativeBody } from './ThinkingBlock'
import { FileOpToolRow, FileOpToolCard } from './ToolRows'
import { normalizeToolName, presentationForTool } from '../../toolPresentation'
import { WebFetchItem } from './WebFetchItem'
import { SubAgentBlock } from '../SubAgentBlock'
import { CodeExecutionCard } from '../CodeExecutionCard'
import { ExecutionCard } from '../ExecutionCard'
import { TypewriterText, RenderTitleSpans } from './utils'
import { timelineStepText } from './types'
import { SourceListCard } from './SourceList'
import { QueryPill } from './utils'
import { useLocale } from '../../contexts/LocaleContext'
import { localizeTimelineLabel, localizeTimelineTitleSpan } from './labels'
import type { Locale } from '../../locales'
import { renderTimelineText } from '../../timelineText'

const EXPLORE_BOTTOM_PAD = 0
const SCROLL_EDGE_EPSILON = 1
const CARD_BOTTOM_FOLLOW_THRESHOLD = 24

function cardShadowState(el: HTMLElement) {
  const maxScrollTop = Math.max(0, el.scrollHeight - el.clientHeight)
  return {
    top: el.scrollTop > SCROLL_EDGE_EPSILON,
    bottom: el.scrollTop < maxScrollTop - SCROLL_EDGE_EPSILON,
  }
}

function cardIsNearBottom(el: HTMLElement) {
  const maxScrollTop = Math.max(0, el.scrollHeight - el.clientHeight)
  return maxScrollTop - el.scrollTop <= CARD_BOTTOM_FOLLOW_THRESHOLD
}

export const CopTimelineSegment = memo(function CopTimelineSegment({
  segment,
  pool,
  isLive,
  defaultExpanded,
  hideHeader,
  compactNarrativeEnd = false,
  flattenSingleItem = false,
  onOpenCodeExecution,
  activeCodeExecutionId,
  onOpenSubAgent,
  accessToken,
  baseUrl,
  typography = 'default',
}: {
  segment: CopSubSegment
  pool: ResolvedPool
  isLive: boolean
  defaultExpanded: boolean
  hideHeader?: boolean
  compactNarrativeEnd?: boolean
  flattenSingleItem?: boolean
  onOpenCodeExecution?: (ce: CodeExecution) => void
  activeCodeExecutionId?: string
  onOpenSubAgent?: (agent: SubAgentRef) => void
  accessToken?: string
  baseUrl?: string
  typography?: 'default' | 'work'
}) {
  const { locale } = useLocale()
  const reduceMotion = useReducedMotion()
  const [expanded, setExpanded] = useState(defaultExpanded)
  const [hovered, setHovered] = useState(false)
  const [viewportAnimating, setViewportAnimating] = useState(false)
  const contentRef = useRef<HTMLDivElement | null>(null)
  const cardScrollRef = useRef<HTMLDivElement | null>(null)
  const cardContentRef = useRef<HTMLDivElement | null>(null)
  const didPinInitialCardRef = useRef(false)
  const shouldFollowCardBottomRef = useRef(true)
  const [cardShadows, setCardShadows] = useState({ top: false, bottom: false })

  // Sync expanded state when defaultExpanded prop changes (e.g. new segment appears)
  useEffect(() => {
    setExpanded(defaultExpanded)
  }, [defaultExpanded])

  const isOpen = segment.status === 'open'
  const [contentHeight, setContentHeight] = useState(0)

  const measure = useCallback(() => {
    const el = contentRef.current
    if (!el) return
    const nextHeight = el.scrollHeight
    setContentHeight((prev) => prev === nextHeight ? prev : nextHeight)
  }, [])

  useLayoutEffect(() => { measure() }, [measure])

  useLayoutEffect(() => {
    const el = contentRef.current
    if (!el) return
    if (typeof ResizeObserver !== 'function') return
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [measure])

  const updateCardShadows = useCallback(() => {
    const el = cardScrollRef.current
    if (!el) return
    const next = cardShadowState(el)
    setCardShadows((prev) => prev.top === next.top && prev.bottom === next.bottom ? prev : next)
  }, [])

  const handleCardScroll = useCallback(() => {
    const el = cardScrollRef.current
    if (!el) return
    shouldFollowCardBottomRef.current = cardIsNearBottom(el)
    updateCardShadows()
  }, [updateCardShadows])

  useLayoutEffect(() => {
    const el = cardScrollRef.current
    if (!el) return
    if (!didPinInitialCardRef.current || (isLive && shouldFollowCardBottomRef.current)) {
      el.scrollTop = el.scrollHeight
      didPinInitialCardRef.current = true
      shouldFollowCardBottomRef.current = true
    }
    updateCardShadows()
  })

  useLayoutEffect(() => {
    if (typeof ResizeObserver !== 'function') return
    const ro = new ResizeObserver(() => {
      const el = cardScrollRef.current
      if (!el) return
      if (isLive && shouldFollowCardBottomRef.current) el.scrollTop = el.scrollHeight
      updateCardShadows()
    })
    if (cardScrollRef.current) ro.observe(cardScrollRef.current)
    if (cardContentRef.current) ro.observe(cardContentRef.current)
    return () => ro.disconnect()
  }, [isLive, updateCardShadows])

  const displayMode: 'full' | 'closed' = expanded ? 'full' : 'closed'

  const viewportHeight = displayMode === 'full' ? contentHeight : 0

  const viewportTargetHeight = displayMode === 'full' && !viewportAnimating ? 'auto' : viewportHeight
  const viewportTransition = reduceMotion
    ? { duration: 0 }
    : { duration: 0.24, ease: [0.4, 0, 0.2, 1] as const }

  const toggleExpand = () => {
    setViewportAnimating(true)
    setExpanded((v) => !v)
  }

  const endsWithNarrative = compactNarrativeEnd && segment.items.at(-1)?.kind === 'assistant_text'

  const headerLive = isOpen && isLive
  const canDeriveLegacyTitle = segment.status === 'closed'
    && segment.items.some((item) => item.kind === 'call')
    && (segment.category === 'exec' || segment.category === 'plan' || segment.category === 'generic')
  const segmentTitleSpans = segment.titleSpans && segment.titleSpans.length > 0
    ? segment.titleSpans
    : canDeriveLegacyTitle
      ? segmentCompletedTitle(segment)
      : null
  const headerLabel = localizeTimelineLabel(segment.title, locale)
  const hasTitleSpans = segmentTitleSpans && segmentTitleSpans.length > 0

  const renderItemsCard = () => (
    <div
      className="cop-timeline-items-card"
      data-top-shadow={cardShadows.top ? 'true' : 'false'}
      data-bottom-shadow={cardShadows.bottom ? 'true' : 'false'}
    >
      <div
        ref={cardScrollRef}
        className="cop-timeline-items-card__scroll"
        onScroll={handleCardScroll}
      >
        <div ref={cardContentRef} className="cop-timeline-items-card__content">
          {segment.items.map((item) => (
            <div key={itemTypeId(item)} style={{ position: 'relative', padding: '3px 0' }}>
              {renderItem(item, pool, isLive, onOpenCodeExecution, activeCodeExecutionId, onOpenSubAgent, accessToken, baseUrl, typography, locale)}
            </div>
          ))}
        </div>
      </div>
    </div>
  )

  if (hideHeader) {
    return (
      <div style={{ position: 'relative', paddingTop: flattenSingleItem ? 0 : 1, paddingBottom: flattenSingleItem || endsWithNarrative ? 0 : EXPLORE_BOTTOM_PAD }}>
        {flattenSingleItem && segment.items.length === 1 ? (
          renderItem(segment.items[0]!, pool, isLive, onOpenCodeExecution, activeCodeExecutionId, onOpenSubAgent, accessToken, baseUrl, typography, locale)
        ) : (
          renderItemsCard()
        )}
      </div>
    )
  }

  return (
    <div style={{ maxWidth: 'min(100%, 760px)', minWidth: 0 }}>
      <button
        type="button"
        onClick={toggleExpand}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          maxWidth: '100%',
          minWidth: 0,
          border: 'none',
          padding: '3px 0 3px',
          background: 'transparent',
          cursor: 'pointer',
          color: hovered ? 'var(--c-cop-row-hover-fg)' : 'var(--c-cop-row-fg)',
          fontSize: 'var(--c-cop-row-font-size)',
          fontWeight: 400,
          lineHeight: 'var(--c-cop-row-line-height)',
          transition: 'color 0.15s ease',
        }}
      >
        <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          {hasTitleSpans ? (
            <RenderTitleSpans spans={segmentTitleSpans!.map(s => localizeTimelineTitleSpan(s, locale))} />
          ) : (
            <TypewriterText text={headerLabel} live={headerLive} className={headerLive ? 'thinking-shimmer-dim' : undefined} />
          )}
        </span>
        {expanded
          ? <ChevronDown size={13} style={{ flexShrink: 0, color: 'currentColor' }} />
          : <ChevronRight size={13} style={{ flexShrink: 0, color: 'currentColor' }} />
        }
      </button>

      <motion.div
        initial={false}
        animate={{ height: viewportTargetHeight, opacity: displayMode === 'closed' ? 0 : 1 }}
        transition={viewportTransition}
        onAnimationStart={() => setViewportAnimating(true)}
        onAnimationComplete={() => setViewportAnimating(false)}
        style={{
          overflow: displayMode === 'full' && !viewportAnimating ? 'visible' : 'hidden',
        }}
      >
        <motion.div
          ref={contentRef}
          initial={false}
          style={{
            position: 'relative',
            paddingTop: 6,
            paddingLeft: 0,
            paddingBottom: endsWithNarrative ? 0 : EXPLORE_BOTTOM_PAD,
          }}
        >
          {renderItemsCard()}
        </motion.div>
      </motion.div>
    </div>
  )
})

function itemTypeId(item: CopSubSegment['items'][number]): string {
  if (item.kind === 'call') return item.call.toolCallId
  return `${item.kind}-${item.seq}`
}

type ItemResolver = {
  check: (toolCallId: string) => boolean
  render: (toolCallId: string) => React.ReactNode
}

function renderItem(
  item: CopSubSegment['items'][number],
  pool: ResolvedPool,
  live: boolean,
  onOpenCodeExecution?: (ce: CodeExecution) => void,
  activeCodeExecutionId?: string,
  onOpenSubAgent?: (agent: SubAgentRef) => void,
  accessToken?: string,
  baseUrl?: string,
  typography: 'default' | 'work' = 'default',
  locale: Locale = 'zh',
): React.ReactNode {
  if (item.kind === 'thinking') {
    return (
      <CopThoughtSummaryRow
        markdown={item.content}
        live={live && item.startedAtMs != null && item.endedAtMs == null}
        thoughtDurationSeconds={item.startedAtMs != null && item.endedAtMs != null
          ? Math.max(0, Math.round((item.endedAtMs - item.startedAtMs) / 1000))
          : 0}
        startedAtMs={item.startedAtMs}
      />
    )
  }

  if (item.kind === 'assistant_text') {
    return <TimelineNarrativeBody text={item.content} tone="primary" live={live} />
  }

  // call item - look up resolved data
  const call = item.call
  const toolCallId = call.toolCallId

  const resolvers: ItemResolver[] = [
    {
      check: (id) => pool.codeExecutions.has(id),
      render: (id) => {
        const codeExec = pool.codeExecutions.get(id)!
        return codeExec.language === 'shell'
          ? <ExecutionCard variant="shell" displayDescription={codeExec.displayDescription} displayText={codeExec.displayText} code={codeExec.code} output={codeExec.output} status={codeExec.status} errorMessage={codeExec.errorMessage} smooth={live && codeExec.status === 'running'} />
          : <CodeExecutionCard language={codeExec.language} code={codeExec.code} output={codeExec.output} errorMessage={codeExec.errorMessage} status={codeExec.status} onOpen={onOpenCodeExecution ? () => onOpenCodeExecution(codeExec) : undefined} isActive={activeCodeExecutionId === codeExec.id} />
      },
    },
    {
      check: (id) => pool.fileOps.has(id),
      render: (id) => {
        const fileOp = pool.fileOps.get(id)!
        const isEdit = normalizeToolName(fileOp.toolName) === 'edit' ||
          normalizeToolName(fileOp.toolName) === 'edit_file' ||
          normalizeToolName(fileOp.toolName) === 'write_file'
        if (isEdit) {
          return <FileOpToolCard op={fileOp} />
        }
        return <FileOpToolRow op={fileOp} live={live} />
      },
    },
    {
      check: (id) => pool.subAgents.has(id),
      render: (id) => {
        const subAgent = pool.subAgents.get(id)!
        return <SubAgentBlock nickname={subAgent.nickname} personaId={subAgent.personaId} input={subAgent.input} output={subAgent.output} status={subAgent.status} error={subAgent.error} live={live} currentRunId={subAgent.currentRunId} accessToken={accessToken} baseUrl={baseUrl} onOpenPanel={onOpenSubAgent ? () => onOpenSubAgent(subAgent) : undefined} typography={typography} />
      },
    },
    {
      check: (id) => pool.webFetches.has(id),
      render: (id) => {
        const fetch = pool.webFetches.get(id)!
        return <WebFetchItem fetch={fetch} live={live} />
      },
    },
    {
      check: (id) => pool.genericTools.has(id),
      render: (id) => {
        const gen = pool.genericTools.get(id)!
        return <ExecutionCard variant="fileop" toolName={gen.toolName} label={gen.label} displayDescription={gen.displayDescription} displayText={gen.displayText} output={gen.output} status={gen.status} errorMessage={gen.errorMessage} smooth={live && gen.status === 'running'} />
      },
    },
    {
      check: (id) => pool.steps.has(id),
      render: (id) => {
        const step = pool.steps.get(id)!
        return (
          <div>
            <div style={{ fontSize: 'var(--c-cop-row-font-size)', color: 'var(--c-cop-row-fg)', lineHeight: 'var(--c-cop-row-line-height)', display: 'flex', alignItems: 'center', gap: '6px' }}>
              <TypewriterText text={renderTimelineText(timelineStepText(step), locale)} className={step.status === 'active' ? 'thinking-shimmer-dim' : undefined} live={live} />
            </div>
            {step.kind === 'searching' && step.queries && step.queries.length > 0 && (
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', marginTop: '6px' }}>
                {step.queries.map((q, index) => <QueryPill key={`${step.id}:query:${index}`} text={q} live={live} />)}
              </div>
            )}
            {step.kind === 'reviewing' && <SourceListCard sources={step.sources ?? pool.sources} />}
          </div>
        )
      },
    },
  ]

  for (const resolver of resolvers) {
    if (resolver.check(toolCallId)) {
      return resolver.render(toolCallId)
    }
  }

  // Fallback: keep unknown tool rows readable instead of exposing raw ids.
  const hasError = typeof call.errorClass === 'string' && call.errorClass !== ''
  const fallbackTitle = renderTimelineText(presentationForTool(call.toolName, call.arguments).text, locale)
  return (
    <div style={{ fontSize: 'var(--c-cop-row-font-size)', color: 'var(--c-cop-row-fg)', lineHeight: 'var(--c-cop-row-line-height)' }}>
      <TypewriterText text={fallbackTitle} live={live && !hasError && call.result === undefined} />
    </div>
  )
}
