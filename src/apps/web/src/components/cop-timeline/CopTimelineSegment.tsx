import { useState, useEffect, useRef, useLayoutEffect, useCallback } from 'react'
import { motion, useReducedMotion } from 'framer-motion'
import { ChevronDown, ChevronRight } from 'lucide-react'
import type { CopSubSegment, ResolvedPool } from '../../copSubSegment'
import type { CodeExecution } from '../CodeExecutionCard'
import type { SubAgentRef } from '../../storage'
import { CopTimelineUnifiedRow } from './CopUnifiedRow'
import { CopThoughtSummaryRow, TimelineNarrativeBody } from './ThinkingBlock'
import { FileOpToolRow, FileOpToolCard, summarizeDiff } from './ToolRows'
import { normalizeToolName } from '../../toolPresentation'
import { WebFetchItem } from './WebFetchItem'
import { SubAgentBlock } from '../SubAgentBlock'
import { CodeExecutionCard } from '../CodeExecutionCard'
import { ExecutionCard } from '../ExecutionCard'
import { TypewriterText, COP_TIMELINE_CONTENT_PADDING_LEFT_PX } from './utils'
import { timelineStepDisplayLabel } from './types'
import { SourceListCard } from './SourceList'
import { QueryPill } from './utils'

const EXPLORE_PREVIEW_COUNT = 2
const EXPLORE_BOTTOM_PAD = 10

export function CopTimelineSegment({
  segment,
  pool,
  isLive,
  defaultExpanded,
  onOpenCodeExecution,
  activeCodeExecutionId,
  onOpenSubAgent,
  accessToken,
  baseUrl,
}: {
  segment: CopSubSegment
  pool: ResolvedPool
  isLive: boolean
  defaultExpanded: boolean
  onOpenCodeExecution?: (ce: CodeExecution) => void
  activeCodeExecutionId?: string
  onOpenSubAgent?: (agent: SubAgentRef) => void
  accessToken?: string
  baseUrl?: string
}) {
  const reduceMotion = useReducedMotion()
  const [expanded, setExpanded] = useState(defaultExpanded)
  const [hovered, setHovered] = useState(false)
  const [viewportAnimating, setViewportAnimating] = useState(false)
  const contentRef = useRef<HTMLDivElement | null>(null)
  const itemRefs = useRef(new Map<string, HTMLDivElement>())

  // Sync expanded state when defaultExpanded prop changes (e.g. new segment appears)
  useEffect(() => {
    setExpanded(defaultExpanded)
  }, [defaultExpanded])

  const isExplore = segment.category === 'explore'
  const isOpen = segment.status === 'open'
  const showPreview = isExplore && isOpen && expanded
  const itemCount = segment.items.length

  const [metrics, setMetrics] = useState({ fullHeight: 0, previewHeight: 0, previewOffset: 0 })

  const measure = useCallback(() => {
    const el = contentRef.current
    if (!el) return
    const fullHeight = el.scrollHeight
    let previewHeight = fullHeight
    let previewOffset = 0
    if (showPreview) {
      const previewCount = Math.min(EXPLORE_PREVIEW_COUNT, itemCount)
      if (previewCount > 0) {
        const firstPreview = segment.items.at(-previewCount)
        const firstNode = firstPreview ? itemRefs.current.get(firstPreviewTypeId(firstPreview)) : undefined
        previewOffset = firstNode ? firstNode.offsetTop : 0
        previewHeight = Math.max(0, fullHeight - previewOffset)
      }
    }
    setMetrics((prev) =>
      prev.fullHeight === fullHeight && prev.previewHeight === previewHeight && prev.previewOffset === previewOffset
        ? prev
        : { fullHeight, previewHeight, previewOffset },
    )
  }, [showPreview, itemCount, segment.items])

  useLayoutEffect(() => { measure() }, [measure])

  useLayoutEffect(() => {
    const el = contentRef.current
    if (!el) return
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [measure])

  const displayMode: 'full' | 'preview' | 'closed' =
    !expanded ? 'closed' : showPreview ? 'preview' : 'full'

  const viewportHeight = displayMode === 'full'
    ? metrics.fullHeight
    : displayMode === 'preview'
      ? metrics.previewHeight
      : 0

  const viewportTargetHeight = displayMode === 'full' && !viewportAnimating ? 'auto' : viewportHeight
  const contentY = displayMode === 'preview' ? -metrics.previewOffset : 0
  const viewportTransition = reduceMotion
    ? { duration: 0 }
    : { duration: 0.24, ease: [0.4, 0, 0.2, 1] as const }

  const toggleExpand = () => {
    setViewportAnimating(true)
    setExpanded((v) => !v)
  }

  const headerLabel = segment.title
  const headerLive = isOpen && isLive

  // Compute diff suffix for edit segments (colored +/-)
  const diffSuffix: React.ReactNode = (() => {
    if (segment.category !== 'edit') return null
    const editCall = segment.items.find((i) => i.kind === 'call')
    if (!editCall || editCall.kind !== 'call') return null
    const result = editCall.call.result
    if (!result || typeof result !== 'object') return null
    const r = result as Record<string, unknown>
    const diff = typeof r.diff === 'string' ? r.diff : typeof r.patch === 'string' ? r.patch : typeof r.unified_diff === 'string' ? r.unified_diff : ''
    if (typeof diff !== 'string' || !diff) return null
    const counts = summarizeDiff(diff)
    if (!counts) return null
    return (
      <span style={{ display: 'inline-flex', gap: 2, flexShrink: 0, fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace', fontSize: 11 }}>
        {counts.added > 0 && <span className="cop-diff-added">+{counts.added}</span>}
        {counts.removed > 0 && <span className="cop-diff-removed">-{counts.removed}</span>}
      </span>
    )
  })()

  return (
    <div style={{ maxWidth: 'min(100%, 760px)', minWidth: 0 }}>
      <button
        type="button"
        onClick={toggleExpand}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 6,
          maxWidth: '100%',
          minWidth: 0,
          border: 'none',
          padding: '3px 0 3px',
          background: 'transparent',
          cursor: 'pointer',
          color: hovered ? 'var(--c-cop-row-hover-fg)' : 'var(--c-cop-row-fg)',
          fontSize: 13,
          fontWeight: 400,
          lineHeight: '18px',
          transition: 'color 0.15s ease',
        }}
      >
        <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <TypewriterText text={headerLabel} live={headerLive} className={headerLive ? 'thinking-shimmer-dim' : undefined} />
          {diffSuffix}
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
          animate={{ y: contentY }}
          transition={viewportTransition}
          style={{
            position: 'relative',
            paddingTop: 6,
            paddingLeft: COP_TIMELINE_CONTENT_PADDING_LEFT_PX,
            paddingBottom: EXPLORE_BOTTOM_PAD,
          }}
        >
          {segment.items.map((item, index) => (
            <div
              key={itemTypeId(item)}
              ref={(node) => {
                const id = itemTypeId(item)
                if (node) itemRefs.current.set(id, node)
                else itemRefs.current.delete(id)
              }}
              style={{ position: 'relative' }}
            >
              <CopTimelineUnifiedRow
                isFirst={index === 0}
                isLast={index === segment.items.length - 1}
                multiItems={segment.items.length >= 2}
                dotColor={itemDotColor(item)}
                dotTop={itemDotTop(item)}
                paddingBottom={8}
                horizontalMotion={false}
              >
                {renderItem(item, pool, isLive, onOpenCodeExecution, activeCodeExecutionId, onOpenSubAgent, accessToken, baseUrl)}
              </CopTimelineUnifiedRow>
            </div>
          ))}
        </motion.div>
      </motion.div>
    </div>
  )
}

function itemTypeId(item: CopSubSegment['items'][number]): string {
  if (item.kind === 'call') return item.call.toolCallId
  return `${item.kind}-${item.seq}`
}

function firstPreviewTypeId(item: CopSubSegment['items'][number]): string {
  return itemTypeId(item)
}

function itemDotColor(item: CopSubSegment['items'][number]): string {
  if (item.kind === 'thinking') return 'var(--c-text-muted)'
  if (item.kind === 'assistant_text') return 'var(--c-border-mid)'
  // call item - defer to resolved status
  const hasError = typeof item.call.errorClass === 'string' && item.call.errorClass !== ''
  if (hasError) return 'var(--c-status-error-text, #ef4444)'
  const hasResult = item.call.result !== undefined
  return hasResult ? 'var(--c-text-muted)' : 'var(--c-text-secondary)'
}

function itemDotTop(item: CopSubSegment['items'][number]): number | undefined {
  if (item.kind === 'call') {
    const n = normalizeToolName(item.call.toolName)
    // Cards with title bars need higher dot alignment
    if (n === 'edit' || n === 'edit_file' || n === 'write_file') return 18
    if (n === 'python_execute') return 18
  }
  return 6
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

  // Check each pool
  const codeExec = pool.codeExecutions.get(toolCallId)
  if (codeExec) {
    return codeExec.language === 'shell'
      ? <ExecutionCard variant="shell" code={codeExec.code} output={codeExec.output} status={codeExec.status} errorMessage={codeExec.errorMessage} smooth={live && codeExec.status === 'running'} />
      : <CodeExecutionCard language={codeExec.language} code={codeExec.code} output={codeExec.output} errorMessage={codeExec.errorMessage} status={codeExec.status} onOpen={onOpenCodeExecution ? () => onOpenCodeExecution(codeExec) : undefined} isActive={activeCodeExecutionId === codeExec.id} />
  }

  const fileOp = pool.fileOps.get(toolCallId)
  if (fileOp) {
    const isEdit = normalizeToolName(fileOp.toolName) === 'edit' ||
      normalizeToolName(fileOp.toolName) === 'edit_file' ||
      normalizeToolName(fileOp.toolName) === 'write_file'
    if (isEdit) {
      return <FileOpToolCard op={fileOp} />
    }
    return <FileOpToolRow op={fileOp} live={live} />
  }

  const subAgent = pool.subAgents.get(toolCallId)
  if (subAgent) {
    return <SubAgentBlock sourceTool={subAgent.sourceTool} nickname={subAgent.nickname} personaId={subAgent.personaId} input={subAgent.input} output={subAgent.output} status={subAgent.status} error={subAgent.error} live={live} currentRunId={subAgent.currentRunId} accessToken={accessToken} baseUrl={baseUrl} onOpenPanel={onOpenSubAgent ? () => onOpenSubAgent(subAgent) : undefined} />
  }

  const fetch = pool.webFetches.get(toolCallId)
  if (fetch) {
    return <WebFetchItem fetch={fetch} live={live} />
  }

  const gen = pool.genericTools.get(toolCallId)
  if (gen) {
    return <ExecutionCard variant="fileop" toolName={gen.toolName} label={gen.label} output={gen.output} status={gen.status} errorMessage={gen.errorMessage} smooth={live && gen.status === 'running'} />
  }

  const step = pool.steps.get(toolCallId)
  if (step) {
    return (
      <div>
        <div style={{ fontSize: '13px', color: 'var(--c-cop-row-fg)', lineHeight: '18px', display: 'flex', alignItems: 'center', gap: '6px' }}>
          <TypewriterText text={timelineStepDisplayLabel(step)} className={step.status === 'active' ? 'thinking-shimmer-dim' : undefined} live={live} />
        </div>
        {step.kind === 'searching' && step.queries && step.queries.length > 0 && (
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', marginTop: '6px' }}>
            {step.queries.map((q) => <QueryPill key={q} text={q} live={live} />)}
          </div>
        )}
        {step.kind === 'reviewing' && <SourceListCard sources={step.sources ?? pool.sources} />}
      </div>
    )
  }

  // Fallback: render tool name + status
  const hasError = typeof call.errorClass === 'string' && call.errorClass !== ''
  return (
    <div style={{ fontSize: '13px', color: 'var(--c-cop-row-fg)', lineHeight: '18px' }}>
      <TypewriterText text={call.toolName} live={live && !hasError && call.result === undefined} />
    </div>
  )
}
