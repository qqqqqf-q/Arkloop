import { memo, useState, useEffect, useRef, useLayoutEffect, useCallback } from 'react'
import { motion, useReducedMotion } from 'framer-motion'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { AGENT_TOOL_NAMES, segmentCompletedTitle, type CopSubSegment, type ResolvedPool } from '../../copSubSegment'
import type { CodeExecution } from '../CodeExecutionCard'
import type { ArtifactRef, SubAgentRef } from '../../storage'

import { CopThoughtSummaryRow, TimelineNarrativeBody } from './ThinkingBlock'
import { FileOpToolRow, FileOpToolCard } from './ToolRows'
import { basename, normalizeToolName, presentationForTool, stringArg } from '../../toolPresentation'
import { WebFetchItem } from './WebFetchItem'
import { CodeExecutionCard } from '../CodeExecutionCard'
import { ExecutionCard } from '../ExecutionCard'
import { TodoListCard } from '../TodoListCard'
import { TypewriterText, RenderTitleSpans } from './utils'
import { timelineStepText } from './types'
import { SourceListCard } from './SourceList'
import { QueryPill } from './utils'
import { useLocale } from '../../contexts/LocaleContext'
import { localizeTimelineLabel, localizeTimelineTitleSpan } from './labels'
import type { Locale } from '../../locales'
import { renderTimelineText } from '../../timelineText'
import { markerForToolName } from './markers'

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
  flattenLeafItems = false,
  onOpenCodeExecution,
  activeCodeExecutionId,
  onOpenSubAgent,
  onOpenDocument,
  artifacts,
  runId,
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
  flattenLeafItems?: boolean
  onOpenCodeExecution?: (ce: CodeExecution) => void
  activeCodeExecutionId?: string
  onOpenSubAgent?: (agent: SubAgentRef) => void
  onOpenDocument?: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  artifacts?: ArtifactRef[] | null
  runId?: string
  accessToken?: string
  baseUrl?: string
  typography?: 'default' | 'work'
}) {
  const { locale } = useLocale()
  const reduceMotion = useReducedMotion()
  const [expanded, setExpanded] = useState(defaultExpanded)
  const [hovered, setHovered] = useState(false)
  const [viewportAnimating, setViewportAnimating] = useState(false)
  const [viewportHeight, setViewportHeight] = useState(0)
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

  const viewportTransition = reduceMotion
    ? { duration: 0 }
    : { duration: 0.24, ease: [0.4, 0, 0.2, 1] as const }

  const toggleExpand = () => {
    const contentHeight = contentRef.current?.scrollHeight ?? 0
    setViewportHeight(contentHeight)
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
            <div key={itemTypeId(item)} style={{ position: 'relative', padding: '4px 0' }}>
              {renderItem(item, pool, isLive, onOpenCodeExecution, activeCodeExecutionId, onOpenSubAgent, onOpenDocument, artifacts, runId, accessToken, baseUrl, typography, locale, true)}
            </div>
          ))}
        </div>
      </div>
    </div>
  )

  if (hideHeader) {
    const renderFlatItems = flattenLeafItems || (flattenSingleItem && segment.items.length === 1)
    return (
      <div style={{ position: 'relative', paddingTop: flattenSingleItem ? 0 : 1, paddingBottom: flattenSingleItem || endsWithNarrative ? 0 : EXPLORE_BOTTOM_PAD }}>
        {renderFlatItems ? (
          <div style={{ display: 'grid', gap: 2 }}>
            {segment.items.map((item) => (
              <div key={itemTypeId(item)}>
                {renderItem(item, pool, isLive, onOpenCodeExecution, activeCodeExecutionId, onOpenSubAgent, onOpenDocument, artifacts, runId, accessToken, baseUrl, typography, locale, !flattenLeafItems)}
              </div>
            ))}
          </div>
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
        animate={{
          height: displayMode === 'closed'
            ? 0
            : viewportAnimating
              ? viewportHeight
              : 'auto',
          opacity: displayMode === 'closed' ? 0 : 1,
        }}
        transition={viewportTransition}
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

function isAgentToolName(toolName: string): boolean {
  return AGENT_TOOL_NAMES.has(normalizeToolName(toolName))
}

export function isLeafProcessToolName(toolName: string): boolean {
  const normalized = normalizeToolName(toolName)
  return normalized === 'document_write' || isAgentToolName(normalized)
}

export function isLeafProcessSegment(segment: CopSubSegment): boolean {
  return segment.items.length > 0 && segment.items.every((item) => item.kind === 'call' && isLeafProcessToolName(item.call.toolName))
}

type ItemResolver = {
  check: (toolCallId: string) => boolean
  render: (toolCallId: string) => React.ReactNode
}

function relatedSearchSteps(toolCallId: string, pool: ResolvedPool) {
  return [...pool.steps.values()]
    .filter((step) => step.id === toolCallId || step.id.startsWith(`${toolCallId}::`))
    .sort((left, right) => (left.seq ?? 0) - (right.seq ?? 0))
}

function renderSearchStep(
  step: ReturnType<typeof relatedSearchSteps>[number],
  pool: ResolvedPool,
  live: boolean,
  locale: Locale,
) {
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
}

function processRowColor(status?: 'running' | 'success' | 'failed' | SubAgentRef['status']) {
  if (status === 'failed') return 'var(--c-status-error-text, #ef4444)'
  if (status === 'running' || status === 'spawning' || status === 'active') return 'var(--c-cop-row-fg)'
  return 'var(--c-cop-row-fg)'
}

function ProcessActionRow({
  toolName,
  label,
  subject,
  status,
  onClick,
  showIcon = true,
}: {
  toolName: string
  label: string
  subject?: string
  status?: 'running' | 'success' | 'failed' | SubAgentRef['status']
  onClick?: () => void
  showIcon?: boolean
}) {
  const [hovered, setHovered] = useState(false)
  const marker = markerForToolName(toolName)
  const content = (
    <>
      {showIcon && marker && <marker.icon width={12} height={12} strokeWidth={2.1} style={{ flexShrink: 0, color: 'currentColor' }} />}
      <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        {label}
        {subject && <span style={{ color: 'var(--c-text-tertiary)', fontWeight: 400 }}> {subject}</span>}
      </span>
      {onClick && <ChevronRight size={13} style={{ flexShrink: 0, color: 'currentColor' }} />}
    </>
  )
  const style = {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    maxWidth: '100%',
    minWidth: 0,
    border: 'none',
    padding: '3px 0',
    background: 'transparent',
    cursor: onClick ? 'pointer' : 'default',
    color: hovered && onClick ? 'var(--c-cop-row-hover-fg)' : processRowColor(status),
    fontSize: 'var(--c-cop-row-font-size)',
    fontWeight: 400,
    lineHeight: 'var(--c-cop-row-line-height)',
    transition: 'color 0.15s ease',
    fontFamily: 'inherit',
    textAlign: 'left' as const,
  }
  if (!onClick) {
    return <div style={style}>{content}</div>
  }
  return (
    <button
      type="button"
      onClick={onClick}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={style}
    >
      {content}
    </button>
  )
}

function resultRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null
}

function findSubAgentForCall(
  call: Extract<CopSubSegment['items'][number], { kind: 'call' }>['call'],
  pool: ResolvedPool,
): SubAgentRef | undefined {
  const direct = pool.subAgents.get(call.toolCallId)
  if (direct) return direct
  const result = resultRecord(call.result)
  const subAgentId = typeof result?.sub_agent_id === 'string' ? result.sub_agent_id : undefined
  const argSubAgentId = typeof call.arguments?.sub_agent_id === 'string' ? call.arguments.sub_agent_id : undefined
  const argAgentId = typeof call.arguments?.agent_id === 'string' ? call.arguments.agent_id : undefined
  const id = subAgentId ?? argSubAgentId ?? argAgentId
  const agents = [...pool.subAgents.values()]
  if (id) return agents.find((agent) => agent.subAgentId === id || agent.id === id)
  return agents.length === 1 ? agents[0] : undefined
}

function renderAgentActionRow(
  call: Extract<CopSubSegment['items'][number], { kind: 'call' }>['call'],
  pool: ResolvedPool,
  locale: Locale,
  onOpenSubAgent?: (agent: SubAgentRef) => void,
  showIcon = true,
) {
  const agent = findSubAgentForCall(call, pool)
  const result = resultRecord(call.result)
  const name = agent?.nickname || agent?.personaId ||
    (typeof result?.nickname === 'string' ? result.nickname : undefined) ||
    stringArg(call.arguments, 'nickname') ||
    stringArg(call.arguments, 'persona_id')
  const label = renderTimelineText(presentationForTool(call.toolName, call.arguments).text, locale)
  return (
    <ProcessActionRow
      toolName={call.toolName}
      label={label}
      subject={name}
      status={agent?.status}
      onClick={agent && onOpenSubAgent ? () => onOpenSubAgent(agent) : undefined}
      showIcon={showIcon}
    />
  )
}

function artifactRefsFromResult(result: unknown): ArtifactRef[] {
  const record = resultRecord(result)
  const artifacts = Array.isArray(record?.artifacts) ? record.artifacts : []
  return artifacts
    .filter((item): item is Record<string, unknown> => item != null && typeof item === 'object')
    .filter((item) => typeof item.key === 'string' && typeof item.filename === 'string')
    .map((item) => ({
      key: item.key as string,
      filename: item.filename as string,
      size: typeof item.size === 'number' ? item.size : 0,
      mime_type: typeof item.mime_type === 'string' ? item.mime_type : '',
      title: typeof item.title === 'string' ? item.title : undefined,
      display: item.display === 'inline' || item.display === 'panel' ? item.display as 'inline' | 'panel' : undefined,
    }))
}

function findDocumentArtifact(call: Extract<CopSubSegment['items'][number], { kind: 'call' }>['call'], artifacts?: ArtifactRef[] | null): ArtifactRef | undefined {
  const resultArtifacts = artifactRefsFromResult(call.result)
  const filename = stringArg(call.arguments, 'filename')
  const title = stringArg(call.arguments, 'title') || stringArg(call.arguments, 'name')
  const candidates = [...resultArtifacts, ...(artifacts ?? [])]
  return candidates.find((artifact) => (
    (filename && artifact.filename === filename) ||
    (title && artifact.title === title) ||
    resultArtifacts.some((item) => item.key === artifact.key)
  )) ?? resultArtifacts[0] ?? artifacts?.[0]
}

function renderDocumentWriteRow(
  call: Extract<CopSubSegment['items'][number], { kind: 'call' }>['call'],
  locale: Locale,
  onOpenDocument?: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void,
  artifacts?: ArtifactRef[] | null,
  runId?: string,
  showIcon = true,
) {
  const hasError = typeof call.errorClass === 'string' && call.errorClass.trim() !== ''
  const running = call.result === undefined && !hasError
  const label = localizeTimelineLabel(hasError ? 'Document write failed' : running ? 'Writing document' : 'Wrote document', locale)
  const subject = stringArg(call.arguments, 'title') ||
    stringArg(call.arguments, 'name') ||
    (stringArg(call.arguments, 'filename') ? basename(stringArg(call.arguments, 'filename')!) : undefined)
  const artifact = findDocumentArtifact(call, artifacts)
  return (
    <ProcessActionRow
      toolName={call.toolName}
      label={label}
      subject={subject}
      status={hasError ? 'failed' : running ? 'running' : 'success'}
      onClick={artifact && onOpenDocument ? () => onOpenDocument(artifact, { artifacts: artifacts ?? artifactRefsFromResult(call.result), runId }) : undefined}
      showIcon={showIcon}
    />
  )
}

function renderItem(
  item: CopSubSegment['items'][number],
  pool: ResolvedPool,
  live: boolean,
  onOpenCodeExecution?: (ce: CodeExecution) => void,
  activeCodeExecutionId?: string,
  onOpenSubAgent?: (agent: SubAgentRef) => void,
  onOpenDocument?: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void,
  artifacts?: ArtifactRef[] | null,
  runId?: string,
  // 预留给后续渲染定制的参数,当前未消费;保位以下划线前缀避免 TS6133
  _accessToken?: string,
  _baseUrl?: string,
  _typography: 'default' | 'work' = 'default',
  locale: Locale = 'zh',
  showLeafIcon = true,
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

  if (isAgentToolName(call.toolName)) {
    return renderAgentActionRow(call, pool, locale, onOpenSubAgent, showLeafIcon)
  }

  if (normalizeToolName(call.toolName) === 'document_write') {
    return renderDocumentWriteRow(call, locale, onOpenDocument, artifacts, runId, showLeafIcon)
  }

  const resolvers: ItemResolver[] = [
    {
      check: (id) => pool.todoWrites.has(id),
      render: (id) => <TodoListCard todo={pool.todoWrites.get(id)!} />,
    },
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
      check: (id) => relatedSearchSteps(id, pool).length > 0,
      render: (id) => {
        const steps = relatedSearchSteps(id, pool)
        return (
          <div style={{ display: 'grid', gap: '10px' }}>
            {steps.map((step) => (
              <div key={step.id}>
                {renderSearchStep(step, pool, live, locale)}
              </div>
            ))}
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
