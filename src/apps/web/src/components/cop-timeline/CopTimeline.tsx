import { useState, useEffect, useRef } from 'react'
import { motion, useReducedMotion } from 'framer-motion'
import { ChevronRight } from 'lucide-react'
import type { CodeExecution } from '../CodeExecutionCard'
import type { SubAgentRef } from '../../storage'
import { useLocale } from '../../contexts/LocaleContext'
import type { CopSubSegment, ResolvedPool } from '../../copSubSegment'
import { aggregateMainTitle } from '../../copSubSegment'
import { recordPerfCount, recordPerfValue } from '../../perfDebug'
import {
  COP_TIMELINE_THINKING_PLAIN_LINE_HEIGHT_PX,
  COP_TIMELINE_DOT_TOP,
  COP_TIMELINE_DOT_SIZE,
  extractThinkingTitles,
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

const TOP_LEVEL_TOOL_NAMES = new Set(['python_execute', 'exec_command', 'continue_process', 'terminate_process', 'todo_write'])

function isTopLevelOnlySegment(segment: CopSubSegment): boolean {
  const calls = segment.items.filter((item): item is Extract<CopSubSegment['items'][number], { kind: 'call' }> => item.kind === 'call')
  return calls.length > 0 && calls.every((item) => TOP_LEVEL_TOOL_NAMES.has(item.call.toolName))
}

function isSingleImageToolSegment(segment: CopSubSegment): boolean {
  return segment.category === 'image' && segment.items.length === 1 && segment.items[0]?.kind === 'call'
}

export function CopTimeline({
  segments,
  pool,
  thinkingOnly,
  thinkingHint,
  headerOverride,
  isComplete,
  live,
  shimmer,
  compactNarrativeEnd = false,
  onOpenCodeExecution,
  activeCodeExecutionId,
  onOpenSubAgent,
  accessToken,
  baseUrl,
  typography = 'default',
}: {
  segments: CopSubSegment[]
  pool: ResolvedPool
  thinkingOnly?: { markdown: string; live?: boolean; durationSec: number; startedAtMs?: number } | null
  thinkingHint?: string
  headerOverride?: string
  isComplete: boolean
  live?: boolean
  shimmer?: boolean
  compactNarrativeEnd?: boolean
  onOpenCodeExecution?: (ce: CodeExecution) => void
  activeCodeExecutionId?: string
  onOpenSubAgent?: (agent: SubAgentRef) => void
  accessToken?: string
  baseUrl?: string
  typography?: 'default' | 'work'
}) {
  const { t } = useLocale()
  const reduceMotion = useReducedMotion()
  const timelineSegments = segments.filter((s) => !isTopLevelOnlySegment(s))
  const segmentDotTop = (segment: CopSubSegment) => isSingleImageToolSegment(segment) ? COP_TIMELINE_DOT_TOP : 8

  const poolHasItems = pool.fileOps.size > 0 || pool.webFetches.size > 0 || pool.subAgents.size > 0 || pool.genericTools.size > 0 || pool.steps.size > 0
  const hasSegments = timelineSegments.length > 0 || poolHasItems
  const hasThinkingOnly = thinkingOnly != null && timelineSegments.length === 0 && !poolHasItems
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

  // 提取 thinking 中最后一个 **标题** 作为 header label
  const thinkingTitles = thinkingOnly?.markdown ? extractThinkingTitles(thinkingOnly.markdown) : []
  const lastThinkingTitle = thinkingTitles.length > 0 ? thinkingTitles[thinkingTitles.length - 1] : undefined

  const pendingHasContent = hasSegments || hasThinkingOnly
  const pendingShowThinkingHeader = segments.length === 0 && !!live && !anyThinking && !pendingHasContent && !!thinkingHint
  const pendingThinkingHeaderLabel = thinkingHint ? `${thinkingHint}...` : undefined
  const thinkingTimerActive = anyThinkingLive || (anyThinking && !!live)
  const activeThinkingElapsed = useThinkingElapsedSeconds(thinkingTimerActive, segmentThinkingStartedAtMs)
  const thinkingLiveHeaderLabel = lastThinkingTitle
    ? `${lastThinkingTitle} ${activeThinkingElapsed}s`
    : formatThinkingHeaderLabel(thinkingHint, activeThinkingElapsed, t)

  const shouldRender = hasSegments || hasThinkingOnly || pendingShowThinkingHeader

  const timelineLive = !!live && timelineSegments.some((s) => s.status === 'open')
  const hasTimelineBody = timelineSegments.length > 0 || hasThinkingOnly || anyThinking || pendingShowThinkingHeader

  // 带标题的 label："{title} {sec}s" 或 "{title}"
  const titledDurationLabel = (title: string, sec: number) =>
    sec > 0 ? `${title} ${sec}s` : title

  const thoughtDurationLabel = lastThinkingTitle
    ? titledDurationLabel(lastThinkingTitle, aggregatedDurationSec)
    : aggregatedDurationSec > 0
      ? t.copTimelineThoughtForSeconds(aggregatedDurationSec)
      : t.copTimelineThinkingDoneNoDuration

  const headerPhaseKey: string = (anyThinkingLive || (anyThinking && !!live))
    ? 'thinking-live'
    : anyThinking
      ? 'thought'
      : pendingShowThinkingHeader
        ? 'thinking-pending'
        : timelineLive
          ? 'live'
          : isComplete
            ? 'complete'
            : 'idle'

  const headerLabel = headerOverride ?? (() => {
    if (anyThinkingLive || (anyThinking && live)) return thinkingLiveHeaderLabel
    if (anyThinking && isComplete && !hasSegments) return thoughtDurationLabel
    if (pendingShowThinkingHeader) return pendingThinkingHeaderLabel ?? ''
    if (hasSegments) {
      const timelineComplete = isComplete || (!!live && !timelineLive)
      const aggregated = aggregateMainTitle(timelineSegments, timelineLive, timelineComplete)
      if (aggregated) return aggregated
    }
    if (anyThinking) return thoughtDurationLabel
    if (isComplete) return 'Completed'
    return thinkingHint ? `${thinkingHint}...` : 'Working...'
  })()

  const seededStatusAnimation =
    timelineLive || !!shimmer || headerPhaseKey === 'thinking-pending' || headerPhaseKey === 'thinking-live'
  const headerUsesIncrementalTypewriter = seededStatusAnimation
  const headerAnimationSeedText =
    headerOverride == null && headerPhaseKey === 'thinking-live' && !lastThinkingTitle
      ? pendingThinkingHeaderLabel
      : undefined

  const [hovered, setHovered] = useState(false)

  useEffect(() => {
    recordPerfCount('cop_timeline_render', 1, {
      segments: timelineSegments.length,
      thinkingOnly: !!hasThinkingOnly,
      live: !!live,
      collapsed,
    })
    recordPerfValue('cop_timeline_segments', timelineSegments.length, 'segments', {
      collapsed,
      live: !!live,
    })
  }, [collapsed, hasThinkingOnly, live, timelineSegments.length])

  if (!shouldRender) return null

  const toggleBody = () => {
    setUserToggled(true)
    setCollapsed((v) => !v)
  }

  return (
    <div className={`cop-timeline-root${typography === 'work' ? ' cop-timeline-root--work' : ''}`} style={typography !== 'work' ? { maxWidth: '663px' } : undefined}>
      {hasTimelineBody && (
        <>
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
              fontSize: 'var(--c-cop-row-font-size)',
              fontWeight: 400,
              lineHeight: 'var(--c-cop-row-line-height)',
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
              animationSeedText={headerAnimationSeedText}
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
            <div style={{ position: 'relative', paddingTop: typography === 'work' ? '5px' : '3px', paddingBottom: typography === 'work' ? '6px' : '3px', paddingLeft: (timelineSegments.length > 1 || (hasThinkingOnly && typography !== 'work')) ? '24px' : undefined }}>
              {/* Thinking-only mode (no segments) */}
              {hasThinkingOnly && thinkingOnly && (() => {
                const isWork = typography === 'work'
                const showDone = !isWork && isComplete && !thinkingOnly.live
                const multiItems = showDone
                return (
                  <>
                    {isWork ? (
                      <AssistantThinkingMarkdown
                        markdown={thinkingOnly.markdown}
                        live={!!thinkingOnly.live && !isComplete}
                        variant="timeline-plain"
                      />
                    ) : (
                      <CopTimelineUnifiedRow
                        isFirst={true}
                        isLast={!showDone}
                        multiItems={multiItems}
                        dotColor={thinkingOnly.live && !isComplete ? 'var(--c-text-secondary)' : 'var(--c-border-mid)'}
                        dotTop={COP_TIMELINE_DOT_TOP}
                        paddingBottom={8}
                        horizontalMotion={false}
                      >
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
                      </CopTimelineUnifiedRow>
                    )}
                    {showDone && (
                      <CopTimelineUnifiedRow
                        isFirst={false}
                        isLast={true}
                        multiItems={multiItems}
                        dotColor="var(--c-text-muted)"
                        dotTop={COP_TIMELINE_DOT_TOP}
                        paddingBottom={0}
                        horizontalMotion={false}
                      >
                        <div
                          style={{
                            fontSize: 'var(--c-cop-row-font-size)',
                            color: 'var(--c-cop-row-fg)',
                            lineHeight: 'var(--c-cop-row-line-height)',
                            paddingTop: '3px',
                          }}
                        >
                          {t.copThinkingDone as string}
                        </div>
                      </CopTimelineUnifiedRow>
                    )}
                  </>
                )
              })()}

              {/* Segments */}
              {timelineSegments.length === 1 ? (
                <CopTimelineSegment
                  segment={timelineSegments[0]!}
                  pool={pool}
                  isLive={!!live && timelineSegments[0]!.status === 'open'}
                  defaultExpanded={true}
                  hideHeader
                  flattenSingleItem={isSingleImageToolSegment(timelineSegments[0]!)}
                  compactNarrativeEnd={compactNarrativeEnd}
                  onOpenCodeExecution={onOpenCodeExecution}
                  activeCodeExecutionId={activeCodeExecutionId}
                  onOpenSubAgent={onOpenSubAgent}
                  accessToken={accessToken}
                  baseUrl={baseUrl}
                  typography={typography}
                />
              ) : (
                timelineSegments.map((seg, index) => {
                const isLast = index === timelineSegments.length - 1
                const flattenSingleItem = isSingleImageToolSegment(seg)
                const segDotColor = seg.status === 'open'
                  ? 'var(--c-text-secondary)'
                  : 'var(--c-text-muted)'
                return (
                  <CopTimelineUnifiedRow
                    key={seg.id}
                    isFirst={index === 0}
                    isLast={isLast}
                    multiItems={timelineSegments.length >= 2}
                    dotColor={segDotColor}
                    dotTop={segmentDotTop(seg)}
                    paddingBottom={7}
                    horizontalMotion={false}
                  >
                    <CopTimelineSegment
                      segment={seg}
                      pool={pool}
                      isLive={!!live && seg.status === 'open'}
                      defaultExpanded={isLast && (!isComplete || seg.status === 'open')}
                      hideHeader={flattenSingleItem}
                      flattenSingleItem={flattenSingleItem}
                      compactNarrativeEnd={compactNarrativeEnd}
                      onOpenCodeExecution={onOpenCodeExecution}
                      activeCodeExecutionId={activeCodeExecutionId}
                      onOpenSubAgent={onOpenSubAgent}
                      accessToken={accessToken}
                      baseUrl={baseUrl}
                      typography={typography}
                    />
                  </CopTimelineUnifiedRow>
                )
              }))}
            </div>
          </motion.div>
        </>
      )}
    </div>
  )
}
