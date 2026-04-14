import { useState, useRef, useEffect, useLayoutEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { ChevronRight } from 'lucide-react'
import { CodeExecutionCard, type CodeExecution } from '../CodeExecutionCard'
import { useLocale } from '../../contexts/LocaleContext'
import { ExecutionCard } from '../ExecutionCard'
import { SubAgentBlock } from '../SubAgentBlock'
import { recordPerfCount, recordPerfValue } from '../../perfDebug'
import type { Props, UEntry } from './types'
import { dotColor, autoLabel, ENTRY_SORT_PRIORITY, timelineStepDisplayLabel } from './types'
import {
  COP_TIMELINE_DOT_TOP,
  COP_TIMELINE_DOT_SIZE,
  COP_TIMELINE_PYTHON_DOT_TOP,
  COP_TIMELINE_CONTENT_PADDING_LEFT_PX,
  COP_TIMELINE_THINKING_PLAIN_LINE_HEIGHT_PX,
  TypewriterText,
  QueryPill,
  initialThinkingElapsedSec,
  firstThinkingStartMs,
} from './utils'
import {
  useThinkingElapsedSeconds,
  formatThinkingHeaderLabel,
  CopTimelineHeaderLabel,
} from './CopTimelineHeader'
import { AssistantThinkingMarkdown, CopThoughtSummaryRow, TimelineNarrativeBody } from './ThinkingBlock'
import { SourceListCard } from './SourceList'
import { WebFetchItem } from './WebFetchItem'
import { CopTimelineUnifiedRow } from './CopUnifiedRow'

export function CopTimeline({ steps, sources, narratives, isComplete, codeExecutions, onOpenCodeExecution, activeCodeExecutionId, subAgents, fileOps, webFetches, genericTools, headerOverride, shimmer, live, preserveExpanded, accessToken, baseUrl, thinkingRows, copInlineTextRows, assistantThinking, thinkingStartedAt, trailingAssistantTextPresent, thinkingHint, forceCollapsed }: Props) {
  const { t } = useLocale()
  const thinkingRowList = thinkingRows ?? []
  const copInlineList = copInlineTextRows ?? []
  const interleavedThinkingLive = thinkingRowList.some((r) => r.live)
  const legacyThinkingLive = !!assistantThinking?.live
  const legacyThinkingVisible = !!(assistantThinking && (assistantThinking.markdown.trim() !== '' || assistantThinking.live))
  const hasInterleavedThinking = thinkingRowList.length > 0
  const hasAnyThinking = hasInterleavedThinking || legacyThinkingVisible
  const anyThinkingLive = interleavedThinkingLive || legacyThinkingLive

  const visibleSteps = steps.filter((step) => step.kind !== 'finished')
  const textEntries = narratives ?? []
  const codeExecCount = codeExecutions?.length ?? 0
  const subAgentCount = subAgents?.length ?? 0
  const fileOpCount = fileOps?.length ?? 0
  const webFetchCount = webFetches?.length ?? 0
  const genericToolCount = genericTools?.length ?? 0
  const sourceCount = sources.length
  const effectiveStepCount = visibleSteps.length || (codeExecCount + subAgentCount + fileOpCount + webFetchCount + genericToolCount)
  const hasThinkingOnly = hasAnyThinking && effectiveStepCount === 0 && sourceCount === 0
  const mixedSegmentWithThinking = hasAnyThinking && !hasThinkingOnly
  const timelineIsLive = !!live || anyThinkingLive
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

  const [collapsed, setCollapsed] = useState(() => {
    if (preserveExpanded) return false
    if (!isComplete) return timelineIsLive ? hasThinkingOnly : false
    if (hasThinkingOnly) return true
    return true
  })

  const userToggledCollapsedRef = useRef(false)
  const prevTimelineIsLiveRef = useRef<boolean | undefined>(undefined)

  const prevCompleteRef = useRef<boolean | undefined>(undefined)
  useEffect(() => {
    if (preserveExpanded) {
      prevCompleteRef.current = isComplete
      return
    }
    if (userToggledCollapsedRef.current) {
      prevCompleteRef.current = isComplete
      return
    }
    if (prevCompleteRef.current === undefined) {
      prevCompleteRef.current = isComplete
      return
    }
    if (!prevCompleteRef.current && isComplete) {
      setCollapsed(true)
    }
    prevCompleteRef.current = isComplete
  }, [isComplete, preserveExpanded])

  useEffect(() => {
    if (preserveExpanded) return
    if (userToggledCollapsedRef.current) return
    if (!isComplete && timelineIsLive) {
      setCollapsed(hasThinkingOnly)
      return
    }
    if (hasThinkingOnly && trailingAssistantTextPresent) {
      setCollapsed(true)
    }
  }, [hasThinkingOnly, isComplete, preserveExpanded, timelineIsLive, trailingAssistantTextPresent])

  useLayoutEffect(() => {
    if (preserveExpanded) {
      prevTimelineIsLiveRef.current = timelineIsLive
      return
    }
    if (userToggledCollapsedRef.current) {
      prevTimelineIsLiveRef.current = timelineIsLive
      return
    }
    if (prevTimelineIsLiveRef.current === undefined) {
      prevTimelineIsLiveRef.current = timelineIsLive
      return
    }
    if (prevTimelineIsLiveRef.current && !timelineIsLive) {
      setCollapsed(true)
    }
    prevTimelineIsLiveRef.current = timelineIsLive
  }, [preserveExpanded, timelineIsLive])

  useEffect(() => {
    if (forceCollapsed == null) return
    if (userToggledCollapsedRef.current) return
    setCollapsed(forceCollapsed)
  }, [forceCollapsed])

  const aggregatedDurationSec = (() => {
    let sum = 0
    for (const r of thinkingRowList) {
      const d = r.durationSec
      if (typeof d === 'number' && d > 0) sum += d
    }
    if (legacyThinkingVisible && thinkingRowList.length === 0) {
      sum += initialThinkingElapsedSec(thinkingStartedAt, [], assistantThinking ?? null)
    }
    return sum
  })()

  const segmentThinkingStartedAtMs = firstThinkingStartMs(thinkingRowList, thinkingStartedAt)
  const pendingHasContent = (
    visibleSteps.length +
    textEntries.length +
    codeExecCount +
    subAgentCount +
    fileOpCount +
    webFetchCount +
    genericToolCount +
    thinkingRowList.length +
    copInlineList.length +
    (legacyThinkingVisible ? 1 : 0)
  ) > 0 || sourceCount > 0
  const pendingShowThinkingHeader = !!live && !hasAnyThinking && !pendingHasContent && !!thinkingHint
  const thinkingTimerActive = anyThinkingLive || (hasAnyThinking && !!live)
  const activeThinkingElapsed = useThinkingElapsedSeconds(thinkingTimerActive, segmentThinkingStartedAtMs)
  const thinkingLiveHeaderLabel = formatThinkingHeaderLabel(thinkingHint, activeThinkingElapsed, t)

  const [hovered, setHovered] = useState(false)
  const shouldRender = !(
    visibleSteps.length === 0 &&
    textEntries.length === 0 &&
    codeExecCount === 0 &&
    subAgentCount === 0 &&
    fileOpCount === 0 &&
    webFetchCount === 0 &&
    genericToolCount === 0 &&
    !headerOverride &&
    !hasAnyThinking &&
    !thinkingHint &&
    copInlineList.length === 0
  )

  let fallbackSeq = 0
  const nextSeq = (seq?: number) => {
    if (typeof seq === 'number') {
      fallbackSeq = Math.max(fallbackSeq + 1, seq + 1)
      return seq
    }
    const next = fallbackSeq
    fallbackSeq += 1
    return next
  }
  const allUnified: UEntry[] = []
  for (const step of visibleSteps) {
    allUnified.push({ kind: 'step', id: step.id, seq: nextSeq(step.seq), item: step })
  }
  for (const narrative of textEntries) {
    allUnified.push({ kind: 'text', id: narrative.id, seq: nextSeq(narrative.seq), item: narrative })
  }
  for (const ce of (codeExecutions ?? [])) {
    allUnified.push({ kind: 'code', id: ce.id, seq: nextSeq(ce.seq), item: ce })
  }
  for (const a of (subAgents ?? [])) {
    allUnified.push({ kind: 'agent', id: a.id, seq: nextSeq(a.seq), item: a })
  }
  for (const op of (fileOps ?? [])) {
    allUnified.push({ kind: 'fileop', id: op.id, seq: nextSeq(op.seq), item: op })
  }
  for (const wf of (webFetches ?? [])) {
    allUnified.push({ kind: 'fetch', id: wf.id, seq: nextSeq(wf.seq), item: wf })
  }
  for (const tool of (genericTools ?? [])) {
    allUnified.push({ kind: 'generic', id: tool.id, seq: nextSeq(tool.seq), item: tool })
  }
  for (const row of thinkingRowList) {
    allUnified.push({
      kind: 'thinking',
      id: row.id,
      seq: nextSeq(row.seq),
      item: { markdown: row.markdown, live: !!row.live, durationSec: row.durationSec, startedAtMs: row.startedAtMs },
    })
  }
  for (const row of copInlineList) {
    allUnified.push({
      kind: 'copinline',
      id: row.id,
      seq: nextSeq(row.seq),
      item: { text: row.text, live: !!row.live },
    })
  }
  if (legacyThinkingVisible) {
    const legacyDurationSec = initialThinkingElapsedSec(
      thinkingStartedAt,
      thinkingRowList,
      assistantThinking ?? null,
    )
    allUnified.push({
      kind: 'thinking',
      id: '_assistant_thinking',
      seq: Number.MIN_SAFE_INTEGER,
      item: {
        markdown: assistantThinking!.markdown,
        live: !!assistantThinking!.live,
        durationSec: legacyDurationSec,
      },
    })
  }
  const totalUnifiableItems =
    visibleSteps.length +
    textEntries.length +
    codeExecCount +
    subAgentCount +
    fileOpCount +
    webFetchCount +
    genericToolCount +
    thinkingRowList.length +
    copInlineList.length +
    (legacyThinkingVisible ? 1 : 0)
  const hasContent = totalUnifiableItems > 0 || sourceCount > 0
  const useUnified = totalUnifiableItems > 0
  allUnified.sort((a, b) => a.seq - b.seq || ENTRY_SORT_PRIORITY[a.kind] - ENTRY_SORT_PRIORITY[b.kind] || a.id.localeCompare(b.id))

  const thinkingOnlyUnified =
    useUnified &&
    allUnified.length > 0 &&
    copInlineList.length === 0 &&
    allUnified.every((e) => e.kind === 'thinking')

  const thoughtDurationLabel =
    aggregatedDurationSec > 0
      ? t.copTimelineThoughtForSeconds(aggregatedDurationSec)
      : t.copTimelineThinkingDoneNoDuration
  const showPendingThinkingHeader = pendingShowThinkingHeader

  const thinkingOnlyCompletedPlain =
    thinkingOnlyUnified && isComplete && !anyThinkingLive && hasThinkingOnly
  const unifiedEntries: UEntry[] = thinkingOnlyCompletedPlain
    ? [
        ...allUnified,
        {
          kind: 'done',
          id: '_thinking_done',
          seq: (allUnified[allUnified.length - 1]?.seq ?? 0) + 0.5,
          item: { label: t.copThinkingDone },
        },
      ]
    : allUnified

  const headerPhaseKey = (anyThinkingLive || (hasAnyThinking && !!live))
    ? 'thinking-live'
    : hasAnyThinking
      ? 'thought'
      : showPendingThinkingHeader
        ? 'thinking-pending'
        : live
          ? 'live'
          : isComplete
            ? 'complete'
            : 'idle'

  const resolvedAutoLabel = autoLabel({
    anyThinkingLive,
    hasAnyThinking,
    live: !!live,
    thinkingLiveHeaderLabel,
    isComplete,
    hasThinkingOnly,
    sourceCount,
    effectiveStepCount,
    thoughtDurationLabel,
    showPendingThinkingHeader,
    thinkingHint,
    visibleSteps,
    t,
  })

  const headerLabel = headerOverride ?? resolvedAutoLabel
  const previousHeaderLabelRef = useRef<string | null>(null)
  const seededStatusAnimation =
    !!live || !!shimmer || (
      !headerOverride && (
        headerPhaseKey === 'thinking-pending'
        || headerPhaseKey === 'thinking-live'
        || headerPhaseKey === 'thought'
      )
    )
  const carriesForwardHeaderAnimation =
    !headerOverride && headerPhaseKey === 'thought'
  const headerUsesIncrementalTypewriter =
    seededStatusAnimation || carriesForwardHeaderAnimation

  useEffect(() => {
    previousHeaderLabelRef.current = headerLabel
  }, [headerLabel])

  useEffect(() => {
    recordPerfCount('cop_timeline_render', 1, timelinePerfSample)
    recordPerfValue('cop_timeline_visible_nodes', unifiedEntries.length, 'nodes', {
      collapsed,
      live: !!live,
      unified: useUnified,
    })
  }, [collapsed, live, timelinePerfSample, unifiedEntries.length, useUnified])

  if (!shouldRender) return null

  return (
    <div
      className="cop-timeline-root"
      style={{
        maxWidth: '663px',
      }}
    >
      <button
        type="button"
        onClick={() => {
          if (!hasContent && isComplete) return
          userToggledCollapsedRef.current = true
          setCollapsed((p) => !p)
        }}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '6px',
          padding: '4px 0 2px',
          background: 'none',
          border: 'none',
          cursor: (!hasContent && isComplete) ? 'default' : 'pointer',
          color: hovered
            ? 'var(--c-text-primary)'
            : isComplete && collapsed
              ? 'var(--c-text-tertiary)'
              : 'var(--c-text-secondary)',
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
        {(isComplete || live) && hasContent && (
          <motion.div
            animate={{ rotate: collapsed ? 0 : 90 }}
            transition={{ duration: 0.2, ease: 'easeOut' }}
            style={{ display: 'flex', flexShrink: 0 }}
          >
            <ChevronRight size={13} />
          </motion.div>
        )}
      </button>

      <AnimatePresence initial={!isComplete || !!live || !!shimmer}>
        {!collapsed && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.25, ease: [0.4, 0, 0.2, 1] }}
            style={{ overflow: 'hidden' }}
          >
            <div style={{ position: 'relative', paddingLeft: visibleSteps.length > 0 || textEntries.length > 0 || codeExecCount > 0 || subAgentCount > 0 || webFetchCount > 0 || fileOpCount > 0 || hasAnyThinking || copInlineList.length > 0 || genericToolCount > 0 ? `${COP_TIMELINE_CONTENT_PADDING_LEFT_PX}px` : undefined, paddingTop: '3px', paddingBottom: '3px' }}>

              <div style={{ display: 'flex', flexDirection: 'column', paddingTop: unifiedEntries.length > 0 ? '0' : undefined }}>
                <AnimatePresence initial={!isComplete || !!live}>
                {unifiedEntries.map((entry, idx) => {
                  const isFirst = idx === 0
                  const isLast = idx === unifiedEntries.length - 1
                  const multiItems = unifiedEntries.length >= 2
                  const entryDotTop = entry.kind === 'code'
                    ? (entry.item.language !== 'shell' ? COP_TIMELINE_PYTHON_DOT_TOP : COP_TIMELINE_DOT_TOP)
                    : COP_TIMELINE_DOT_TOP
                  const entryDotColor = dotColor(entry)
                  return (
                    <CopTimelineUnifiedRow
                      key={entry.id}
                      isFirst={isFirst}
                      isLast={isLast}
                      multiItems={multiItems}
                      dotTop={entryDotTop}
                      dotColor={entryDotColor}
                    >
                      {entry.kind === 'step' && (
                        <div>
                          <div
                            style={{
                              fontSize: '13px',
                              color: 'var(--c-text-tertiary)',
                              lineHeight: '18px',
                              display: 'flex',
                              alignItems: 'center',
                              gap: '6px',
                            }}
                          >
                            <TypewriterText
                              text={timelineStepDisplayLabel(entry.item)}
                              className={entry.item.status === 'active' ? 'thinking-shimmer-dim' : undefined}
                              live={!!live}
                            />
                          </div>

                          {entry.item.kind === 'searching' && entry.item.queries && entry.item.queries.length > 0 && (
                            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', marginTop: '6px' }}>
                              {entry.item.queries.map((q) => (
                                <QueryPill key={q} text={q} live={!!live} />
                              ))}
                            </div>
                          )}

                          {entry.item.kind === 'reviewing' && (
                            <SourceListCard sources={entry.item.sources ?? sources} />
                          )}
                        </div>
                      )}
                      {entry.kind === 'thinking' && (
                        mixedSegmentWithThinking ? (
                          <CopThoughtSummaryRow
                            markdown={entry.item.markdown}
                            live={entry.item.live}
                            thoughtDurationSeconds={entry.item.durationSec ?? 0}
                            startedAtMs={entry.item.startedAtMs}
                          />
                        ) : hasThinkingOnly ? (
                          <div
                            style={{
                              paddingTop: Math.max(
                                0,
                                COP_TIMELINE_DOT_TOP +
                                  COP_TIMELINE_DOT_SIZE / 2 -
                                  COP_TIMELINE_THINKING_PLAIN_LINE_HEIGHT_PX / 2,
                              ),
                            }}
                          >
                            <AssistantThinkingMarkdown
                              markdown={entry.item.markdown}
                              live={entry.item.live && !thinkingOnlyCompletedPlain}
                              variant="timeline-plain"
                            />
                          </div>
                        ) : (
                          <AssistantThinkingMarkdown markdown={entry.item.markdown} live={entry.item.live} />
                        )
                      )}
                      {entry.kind === 'copinline' && (
                        <TimelineNarrativeBody text={entry.item.text} tone="primary" live={entry.item.live} />
                      )}
                      {entry.kind === 'done' && (
                        <div
                          style={{
                            fontSize: '13px',
                            color: 'var(--c-text-tertiary)',
                            lineHeight: '18px',
                          }}
                        >
                          {entry.item.label}
                        </div>
                      )}
                      {entry.kind === 'text' && <TimelineNarrativeBody text={entry.item.text} live={!!live} />}
                      {entry.kind === 'code' && (entry.item.language === 'shell'
                        ? <ExecutionCard variant="shell" code={entry.item.code} output={entry.item.output} status={entry.item.status} errorMessage={entry.item.errorMessage} smooth={!!live && entry.item.status === 'running'} />
                        : <CodeExecutionCard language={entry.item.language} code={entry.item.code} output={entry.item.output} errorMessage={entry.item.errorMessage} status={entry.item.status} onOpen={onOpenCodeExecution ? () => onOpenCodeExecution(entry.item as CodeExecution) : undefined} isActive={activeCodeExecutionId === entry.item.id} />
                      )}
                      {entry.kind === 'agent' && (
                        <SubAgentBlock sourceTool={entry.item.sourceTool} nickname={entry.item.nickname} personaId={entry.item.personaId} input={entry.item.input} output={entry.item.output} status={entry.item.status} error={entry.item.error} live={live} currentRunId={entry.item.currentRunId} accessToken={accessToken} baseUrl={baseUrl} />
                      )}
                      {entry.kind === 'fileop' && (
                        <ExecutionCard variant="fileop" toolName={entry.item.toolName} label={entry.item.label} output={entry.item.output} status={entry.item.status} errorMessage={entry.item.errorMessage} smooth={!!live && entry.item.status === 'running'} />
                      )}
                      {entry.kind === 'fetch' && <WebFetchItem fetch={entry.item} live={!!live} />}
                      {entry.kind === 'generic' && (
                        <ExecutionCard
                          variant="fileop"
                          toolName={entry.item.toolName}
                          label={entry.item.label}
                          output={entry.item.output}
                          status={entry.item.status}
                          errorMessage={entry.item.errorMessage}
                          smooth={!!live && entry.item.status === 'running'}
                        />
                      )}
                    </CopTimelineUnifiedRow>
                  )
                })}
                </AnimatePresence>
              </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
