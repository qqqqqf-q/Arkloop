import { useState, useEffect, useRef } from 'react'
import { motion, useReducedMotion } from 'framer-motion'
import { ChevronRight } from 'lucide-react'
import type { CodeExecution } from '../CodeExecutionCard'
import type { SubAgentRef } from '../../storage'
import { useLocale } from '../../contexts/LocaleContext'
import type { CopSubSegment, ResolvedPool } from '../../copSubSegment'
import { recordPerfCount, recordPerfValue } from '../../perfDebug'
import {
  COP_TIMELINE_THINKING_PLAIN_LINE_HEIGHT_PX,
  COP_TIMELINE_DOT_TOP,
  COP_TIMELINE_DOT_SIZE,
} from './utils'
import {
  useThinkingElapsedSeconds,
  formatThinkingHeaderLabel,
  CopTimelineHeaderLabel,
} from './CopTimelineHeader'
import { AssistantThinkingMarkdown } from './ThinkingBlock'
import { CopTimelineSegment } from './CopTimelineSegment'
import { CopTimelineUnifiedRow } from './CopUnifiedRow'

export type { WebSearchPhaseStep } from './types'

export function CopTimeline({
  segments,
  pool,
  thinkingOnly,
  thinkingHint,
  headerOverride,
  isComplete,
  live,
  shimmer,
  onOpenCodeExecution,
  activeCodeExecutionId,
  onOpenSubAgent,
  accessToken,
  baseUrl,
}: {
  segments: CopSubSegment[]
  pool: ResolvedPool
  thinkingOnly?: { markdown: string; live?: boolean; durationSec: number; startedAtMs?: number } | null
  thinkingHint?: string
  headerOverride?: string
  isComplete: boolean
  live?: boolean
  shimmer?: boolean
  onOpenCodeExecution?: (ce: CodeExecution) => void
  activeCodeExecutionId?: string
  onOpenSubAgent?: (agent: SubAgentRef) => void
  accessToken?: string
  baseUrl?: string
}) {
  const { t } = useLocale()
  const reduceMotion = useReducedMotion()

  const poolHasItems = pool.codeExecutions.size > 0 || pool.fileOps.size > 0 || pool.webFetches.size > 0 || pool.subAgents.size > 0 || pool.genericTools.size > 0 || pool.steps.size > 0
  const hasSegments = segments.length > 0 || poolHasItems
  const hasThinkingOnly = thinkingOnly != null && segments.length === 0 && !poolHasItems
  const anyThinking = thinkingOnly != null
  const thinkingLive = thinkingOnly?.live ?? false
  const anyThinkingLive = thinkingLive
  const timelineIsLive = !!live || anyThinkingLive
  const bodyHasContent = hasSegments || hasThinkingOnly
  const [collapsed, setCollapsed] = useState(() => {
    if (timelineIsLive && !isComplete) return hasThinkingOnly
    if (hasThinkingOnly && !isComplete) return false
    if (hasThinkingOnly && isComplete) return true
    return true
  })
  const [userToggled, setUserToggled] = useState(false)
  const prevLive = useRef(timelineIsLive)

  // Auto-collapse when live ends
  useEffect(() => {
    if (userToggled) return
    if (prevLive.current && !timelineIsLive && isComplete) {
      setCollapsed(true)
    }
    prevLive.current = timelineIsLive
  }, [timelineIsLive, isComplete, userToggled])

  // Auto-expand when new segment appears (live mode)
  useEffect(() => {
    if (userToggled) return
    if (timelineIsLive && !isComplete) {
      setCollapsed(hasThinkingOnly)
    }
    if (hasThinkingOnly && isComplete) {
      setCollapsed(true)
    }
  }, [hasThinkingOnly, isComplete, timelineIsLive, userToggled])

  const aggregatedDurationSec = thinkingOnly?.durationSec ?? 0
  const segmentThinkingStartedAtMs = thinkingOnly?.startedAtMs

  const pendingHasContent = hasSegments || hasThinkingOnly
  const pendingShowThinkingHeader = !!live && !anyThinking && !pendingHasContent && !!thinkingHint
  const thinkingTimerActive = anyThinkingLive || (anyThinking && !!live)
  const activeThinkingElapsed = useThinkingElapsedSeconds(thinkingTimerActive, segmentThinkingStartedAtMs)
  const thinkingLiveHeaderLabel = formatThinkingHeaderLabel(thinkingHint, activeThinkingElapsed, t)

  const shouldRender = hasSegments || hasThinkingOnly || !!thinkingHint

  const thoughtDurationLabel =
    aggregatedDurationSec > 0
      ? t.copTimelineThoughtForSeconds(aggregatedDurationSec)
      : t.copTimelineThinkingDoneNoDuration

  const headerPhaseKey: string = (anyThinkingLive || (anyThinking && !!live))
    ? 'thinking-live'
    : anyThinking
      ? 'thought'
      : pendingShowThinkingHeader
        ? 'thinking-pending'
        : live
          ? 'live'
          : isComplete
            ? 'complete'
            : 'idle'

  const totalStepCount = segments.length

  const headerLabel = headerOverride ?? (() => {
    if (anyThinkingLive || (anyThinking && live)) return thinkingLiveHeaderLabel
    if (anyThinking && isComplete && hasSegments) {
      return totalStepCount > 0
        ? `${totalStepCount} step${totalStepCount === 1 ? '' : 's'} completed`
        : thoughtDurationLabel
    }
    if (anyThinking) return thoughtDurationLabel
    if (pendingShowThinkingHeader) return `${thinkingHint}...`
    if (isComplete) {
      if (totalStepCount > 0) return `${totalStepCount} step${totalStepCount === 1 ? '' : 's'} completed`
      return 'Completed'
    }
    const lastSeg = segments[segments.length - 1]
    if (lastSeg && lastSeg.status === 'open') return lastSeg.title
    return thinkingHint ? `${thinkingHint}...` : 'Working...'
  })()

  const seededStatusAnimation =
    !!live || !!shimmer || headerPhaseKey === 'thinking-pending' || headerPhaseKey === 'thinking-live' || headerPhaseKey === 'thought'
  const headerUsesIncrementalTypewriter = seededStatusAnimation || headerPhaseKey === 'thought'

  const [hovered, setHovered] = useState(false)

  useEffect(() => {
    previousHeaderLabelRef.current = headerLabel
  }, [headerLabel])

  useEffect(() => {
    const timelinePerfSample = {
      steps: visibleSteps.length,
      narratives: textEntries.length,
      thinkingRows: thinkingRowList.length,
      inlineRows: copInlineList.length,
      sources: sourceCount,
      codes: codeExecCount,
      subAgents: subAgentCount,
      fileOps: fileOpCount,
      webFetches: webFetchCount,
      genericTools: genericToolCount,
      live: !!live,
    }
    recordPerfCount('cop_timeline_render', 1, timelinePerfSample)
    recordPerfValue('cop_timeline_visible_nodes', unifiedEntries.length, 'nodes', {
      collapsed,
      live: !!live,
    })
  }, [
    collapsed,
    codeExecCount,
    copInlineList.length,
    fileOpCount,
    genericToolCount,
    live,
    sourceCount,
    subAgentCount,
    textEntries.length,
    thinkingRowList.length,
    unifiedEntries.length,
    useUnified,
    visibleSteps.length,
    webFetchCount,
  ])

  if (!shouldRender) return null

  const toggleBody = () => {
    setUserToggled(true)
    setCollapsed((v) => !v)
  }

  return (
    <div className="cop-timeline-root" style={{ maxWidth: '663px' }}>
      <button
        type="button"
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        onClick={toggleBody}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '6px',
          padding: '4px 0 2px',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          color: hovered ? 'var(--c-cop-row-hover-fg)' : 'var(--c-cop-row-fg)',
          fontSize: '13px',
          fontWeight: 400,
          transition: 'color 0.15s ease',
          maxWidth: '100%',
          minWidth: 0,
          alignSelf: 'stretch',
        }}
      >
        <CopTimelineHeaderLabel
          text={headerLabel}
          phaseKey={headerPhaseKey}
          shimmer={!!shimmer}
          incremental={headerUsesIncrementalTypewriter}
        />
        {(isComplete || live) && bodyHasContent && (
          <motion.div
            animate={{ rotate: collapsed ? 0 : 90 }}
            transition={{ duration: 0.2, ease: 'easeOut' }}
            style={{ display: 'flex', flexShrink: 0 }}
          >
            <ChevronRight size={13} />
          </motion.div>
        )}
      </button>

      <motion.div
        initial={false}
        animate={{ height: collapsed ? 0 : 'auto', opacity: collapsed ? 0 : 1 }}
        transition={!reduceMotion ? { duration: 0.24, ease: [0.4, 0, 0.2, 1] } : { duration: 0 }}
        style={{ overflow: collapsed ? 'hidden' : 'visible' }}
      >
        <div style={{ position: 'relative', paddingTop: '3px', paddingBottom: '3px', paddingLeft: segments.length > 0 ? '24px' : undefined }}>
          {/* Thinking-only mode (no segments) */}
          {hasThinkingOnly && thinkingOnly && (
            <>
              <div
                style={{
                  paddingTop: Math.max(0, COP_TIMELINE_DOT_TOP + COP_TIMELINE_DOT_SIZE / 2 - COP_TIMELINE_THINKING_PLAIN_LINE_HEIGHT_PX / 2),
                }}
              >
                <AssistantThinkingMarkdown
                  markdown={thinkingOnly.markdown}
                  live={!!thinkingOnly.live && !isComplete}
                  variant="timeline-plain"
                />
              </div>
              {isComplete && !thinkingOnly.live && (
                <div
                  style={{
                    fontSize: '13px',
                    color: 'var(--c-cop-row-fg)',
                    lineHeight: '18px',
                    paddingTop: '6px',
                  }}
                >
                  {t.copThinkingDone as string}
                </div>
              )}
            </>
          )}

          {/* Segments */}
          {segments.map((seg, index) => {
            const isLast = index === segments.length - 1
            const segDotColor = seg.status === 'open'
              ? 'var(--c-text-secondary)'
              : 'var(--c-text-muted)'
            return (
              <CopTimelineUnifiedRow
                key={seg.id}
                isFirst={index === 0}
                isLast={isLast}
                multiItems={segments.length >= 2}
                dotColor={segDotColor}
                dotTop={8}
                paddingBottom={7}
                horizontalMotion={false}
              >
                <CopTimelineSegment
                  segment={seg}
                  pool={pool}
                  isLive={!!live && seg.status === 'open'}
                  defaultExpanded={isLast && (!isComplete || seg.status === 'open')}
                  onOpenCodeExecution={onOpenCodeExecution}
                  activeCodeExecutionId={activeCodeExecutionId}
                  onOpenSubAgent={onOpenSubAgent}
                  accessToken={accessToken}
                  baseUrl={baseUrl}
                />
              </CopTimelineUnifiedRow>
            )
          })}

        </div>
      </motion.div>
    </div>
  )
}
