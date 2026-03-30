import { useState, useRef, useEffect, useLayoutEffect, useCallback, useMemo, Fragment, type ReactNode } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { ChevronDown, ChevronRight, Globe, Loader2, Search } from 'lucide-react'
import { useTypewriter } from '../hooks/useTypewriter'
import type { WebSource } from '../storage'
import type { SubAgentRef, FileOpRef, WebFetchRef } from '../storage'
import { codeExecutionAccentColor } from '../codeExecutionStatus'
import { CodeExecutionCard, type CodeExecution } from './CodeExecutionCard'
import { MarkdownRenderer } from './MarkdownRenderer'
import { useLocale } from '../contexts/LocaleContext'
import { ExecutionCard } from './ExecutionCard'
import { SubAgentBlock } from './SubAgentBlock'
import { markdownToSingleLinePreview } from '../copThinkingPlainPreview'

/** CopTimeline 左轴点线几何；ChatPage 顶层条与之对齐 */
export const COP_TIMELINE_DOT_NUDGE_Y = 1
export const COP_TIMELINE_DOT_TOP = 5 + COP_TIMELINE_DOT_NUDGE_Y
export const COP_TIMELINE_DOT_SIZE = 8
export const COP_TIMELINE_SHELL_DOT_TOP = 9 + COP_TIMELINE_DOT_NUDGE_Y
export const COP_TIMELINE_PYTHON_DOT_TOP = 16 + COP_TIMELINE_DOT_NUDGE_Y
export const COP_TIMELINE_LINE_LEFT_PX = -16
export const COP_TIMELINE_DOT_LEFT_PX = -19
export const COP_TIMELINE_CONTENT_PADDING_LEFT_PX = 24
/** 仅 thinking 完结直出时正文行高，与 DOT 几何中心对齐：DOT_TOP + DOT_SIZE/2 − lh/2 */
export const COP_TIMELINE_THINKING_PLAIN_LINE_HEIGHT_PX = 18

export type WebSearchPhaseStep = {
  id: string
  kind: 'planning' | 'searching' | 'reviewing' | 'finished'
  label: string
  status: 'active' | 'done'
  queries?: string[]
  sources?: WebSource[]
  seq?: number
  resultSeq?: number
}

export type SearchNarrative = {
  id: string
  text: string
  seq: number
}

type Props = {
  steps: WebSearchPhaseStep[]
  sources: WebSource[]
  narratives?: SearchNarrative[]
  isComplete: boolean
  codeExecutions?: CodeExecution[]
  onOpenCodeExecution?: (ce: CodeExecution) => void
  activeCodeExecutionId?: string
  subAgents?: SubAgentRef[]
  fileOps?: FileOpRef[]
  webFetches?: WebFetchRef[]
  genericTools?: Array<{ id: string; toolName: string; label: string; output?: string; status: 'running' | 'success' | 'failed'; errorMessage?: string; seq?: number }>
  headerOverride?: string
  shimmer?: boolean
  live?: boolean
  preserveExpanded?: boolean
  accessToken?: string
  baseUrl?: string
  /** 与 tool 同序交错的多段 thinking（seq 与工具池子对齐排序） */
  thinkingRows?: Array<{ id: string; markdown: string; live?: boolean; seq: number; durationSec?: number }> | null
  /** COP 内可见短正文（与 thinking / 工具行同序） */
  copInlineTextRows?: Array<{ id: string; text: string; live?: boolean; seq: number }> | null
  /** 与 narrative / 工具行同一套 unified 点线，仅多一行 Markdown（无 thinkingRows 时的单块） */
  assistantThinking?: { markdown: string; live?: boolean } | null
  /** 仅 pendingThinking 壳子用：无 thinkingRows 时配合 assistantThinking 估时长 */
  thinkingStartedAt?: number
  /** 后一段为助手正文且已有字符时收起本段 COP（不依赖 isStreaming，避免 run 结束帧错过） */
  trailingAssistantTextPresent?: boolean
}

function TypewriterText({ text, className, live }: { text: string; className?: string; live?: boolean }) {
  const displayed = useTypewriter(text, !live)
  return <span className={className}>{live ? displayed : text}</span>
}

const HEADER_TYPE_CPS = 38

/** 非流式：首帧直接出字；之后每次标题变更从空打字；流式仍用 useTypewriter */
function useCopTimelineHeaderDisplay(fullText: string, live: boolean): string {
  const streamed = useTypewriter(fullText, !live)
  const [settled, setSettled] = useState(fullText)
  const prevFullRef = useRef<string | null>(null)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    if (live) {
      if (intervalRef.current) {
        clearInterval(intervalRef.current)
        intervalRef.current = null
      }
      prevFullRef.current = fullText
      return
    }

    if (prevFullRef.current === null) {
      prevFullRef.current = fullText
      setSettled(fullText)
      return
    }

    if (fullText === prevFullRef.current) {
      setSettled(fullText)
      return
    }

    prevFullRef.current = fullText
    if (intervalRef.current) {
      clearInterval(intervalRef.current)
      intervalRef.current = null
    }

    setSettled('')
    let pos = 0
    const ms = Math.max(12, Math.floor(1000 / HEADER_TYPE_CPS))
    intervalRef.current = setInterval(() => {
      pos += 1
      if (pos >= fullText.length) {
        setSettled(fullText)
        if (intervalRef.current) {
          clearInterval(intervalRef.current)
          intervalRef.current = null
        }
      } else {
        setSettled(fullText.slice(0, pos))
      }
    }, ms)

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current)
        intervalRef.current = null
      }
    }
  }, [fullText, live])

  return live ? streamed : settled
}

function CopTimelineHeaderLabel({ text, live, shimmer }: { text: string; live: boolean; shimmer?: boolean }) {
  const shown = useCopTimelineHeaderDisplay(text, live)
  return <span className={shimmer ? 'thinking-shimmer' : undefined}>{shown}</span>
}

function getDomain(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

function getDomainShort(url: string): string {
  try {
    const hostname = new URL(url).hostname.replace(/^www\./, '')
    const parts = hostname.split('.')
    return parts.length >= 2 ? parts[parts.length - 2] : hostname
  } catch {
    return url
  }
}

function isHttpUrl(url: string): boolean {
  try {
    const p = new URL(url)
    return p.protocol === 'http:' || p.protocol === 'https:'
  } catch {
    return false
  }
}

function QueryPill({ text, live }: { text: string; live?: boolean }) {
  const displayed = useTypewriter(text, !live)
  const shown = live ? displayed : text
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        padding: '2px 8px',
        borderRadius: '8px',
        background: 'var(--c-bg-menu)',
        border: '0.5px solid var(--c-border-subtle)',
        fontSize: '12px',
        color: 'var(--c-text-secondary)',
        lineHeight: '18px',
        overflow: 'hidden',
      }}
    >
      <span
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: '5px',
        }}
      >
        <Search size={11} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
        {shown}
      </span>
    </span>
  )
}

export function AssistantThinkingMarkdown({
  markdown,
  live,
  variant = 'default',
}: {
  markdown: string
  live: boolean
  /** timeline-plain：时间轴内直出，较大字号并与圆点竖直对齐 */
  variant?: 'default' | 'timeline-plain'
}) {
  const displayed = useTypewriter(markdown, !live)
  const { t } = useLocale()
  const rootClass =
    variant === 'timeline-plain'
      ? 'cop-thinking-output-md cop-thinking-output-md--timeline-plain'
      : 'cop-thinking-output-md'
  return (
    <div className={rootClass}>
      {!markdown.trim() && live ? (
        <span className="thinking-shimmer cop-thinking-output-placeholder">{t.assistantStreamThinkingPlaceholder}</span>
      ) : (
        <MarkdownRenderer content={live ? displayed : markdown} disableMath trimTrailingMargin compact />
      )}
    </div>
  )
}

const THINKING_EXPAND_TRANSITION = { duration: 0.25, ease: [0.4, 0, 0.2, 1] as const }

function initialThinkingElapsedSec(
  thinkingStartedAt: number | undefined,
  thinkingRows: Array<{ live?: boolean }> | null | undefined,
  assistantThinking: { markdown: string; live?: boolean } | null | undefined,
): number {
  if (!thinkingStartedAt) return 0
  const list = thinkingRows ?? []
  const anyLive = list.some((r) => r.live) || !!assistantThinking?.live
  if (anyLive) return 0
  const hasAny =
    list.length > 0 ||
    !!(assistantThinking && (assistantThinking.markdown.trim() !== '' || !!assistantThinking.live))
  if (!hasAny) return 0
  return Math.max(0, Math.round((Date.now() - thinkingStartedAt) / 1000))
}

function CopThinkingPreviewScrollArea({
  live,
  scrollKey,
  className,
  dataTestId,
  children,
}: {
  live: boolean
  scrollKey: string
  className: string
  dataTestId?: string
  children: ReactNode
}) {
  const outerRef = useRef<HTMLDivElement>(null)
  const innerRef = useRef<HTMLSpanElement>(null)
  const scrollToEnd = useCallback(() => {
    const outer = outerRef.current
    if (!outer) return
    outer.scrollLeft = outer.scrollWidth - outer.clientWidth
  }, [])

  useLayoutEffect(() => {
    if (!live) return
    scrollToEnd()
  }, [live, scrollKey, scrollToEnd])

  useEffect(() => {
    if (!live) return
    const inner = innerRef.current
    const outer = outerRef.current
    if (!inner || !outer) return
    const ro = new ResizeObserver(() => {
      outer.scrollLeft = outer.scrollWidth - outer.clientWidth
    })
    ro.observe(inner)
    outer.scrollLeft = outer.scrollWidth - outer.clientWidth
    return () => ro.disconnect()
  }, [live, scrollKey, scrollToEnd])

  return (
    <div ref={outerRef} className={className} data-testid={dataTestId}>
      <span ref={innerRef} className="cop-thinking-preview-scroll-inner">
        {children}
      </span>
    </div>
  )
}

type CopThinkingBlockProps = {
  markdown: string
  live: boolean
  /** thinking 已结束时的秒数，用于折叠行与卡片顶栏「Thought for Xs」 */
  thoughtDurationSeconds: number
}

function CopThinkingBlock({ markdown, live, thoughtDurationSeconds }: CopThinkingBlockProps) {
  const { t } = useLocale()
  const [expanded, setExpanded] = useState(false)
  const cardScrollRef = useRef<HTMLDivElement>(null)
  const plain = markdownToSingleLinePreview(markdown)
  const displayedPreview = useTypewriter(plain, !live)
  const previewShown = live ? displayedPreview : plain
  const previewScrollKey = `${markdown.length}:${previewShown.length}`
  const showInlinePreviewRow = live
  const toggleExpanded = () => setExpanded((p) => !p)
  const cardHeadline =
    live
      ? t.copThinkCardTitle
      : thoughtDurationSeconds > 0
        ? t.copTimelineThoughtForSeconds(thoughtDurationSeconds)
        : t.copTimelineThinkingDoneNoDuration

  useLayoutEffect(() => {
    if (!live || !expanded) return
    const el = cardScrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [live, expanded, markdown])

  useEffect(() => {
    if (!live || !expanded) return
    const el = cardScrollRef.current
    if (!el) return
    const inner = el.firstElementChild
    if (!inner) return
    const ro = new ResizeObserver(() => {
      el.scrollTop = el.scrollHeight
    })
    ro.observe(inner)
    return () => ro.disconnect()
  }, [live, expanded, markdown])

  return (
    <div className="cop-thinking-block" data-testid="cop-thinking-block">
      {showInlinePreviewRow && (
        <div
          role="button"
          tabIndex={0}
          className="cop-thinking-preview-trigger"
          data-testid="cop-thinking-preview-trigger"
          onClick={toggleExpanded}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              toggleExpanded()
            }
          }}
        >
          <span className="cop-thinking-preview-title">{t.copThinkingInlineTitle}</span>
          <CopThinkingPreviewScrollArea
            live={live}
            scrollKey={previewScrollKey}
            className="cop-thinking-preview-text cop-thinking-preview-scroll"
            data-testid="cop-thinking-preview-text"
          >
            {!markdown.trim() ? (
              <span className="thinking-shimmer cop-thinking-preview-placeholder">{t.assistantStreamThinkingPlaceholder}</span>
            ) : (
              previewShown
            )}
          </CopThinkingPreviewScrollArea>
          {expanded ? (
            <ChevronDown size={13} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
          ) : (
            <ChevronRight size={13} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
          )}
        </div>
      )}

      {!showInlinePreviewRow && (
        <div
          role="button"
          tabIndex={0}
          className="cop-thinking-card-trigger"
          data-testid="cop-thinking-card-trigger"
          onClick={toggleExpanded}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              toggleExpanded()
            }
          }}
        >
          <span className="cop-thinking-card-trigger-label">{cardHeadline}</span>
          {expanded ? (
            <ChevronDown size={12} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
          ) : (
            <ChevronRight size={12} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
          )}
        </div>
      )}

      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div
            key="cop-thinking-expand"
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={THINKING_EXPAND_TRANSITION}
            style={{ overflow: 'hidden' }}
          >
            <div className="cop-thinking-card-outer">
              <div ref={cardScrollRef} className="cop-thinking-card-scroll">
                <AssistantThinkingMarkdown markdown={markdown} live={live} />
              </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}

function TimelineNarrativeBody({ text, tone = 'secondary', live }: { text: string; tone?: 'primary' | 'secondary'; live?: boolean }) {
  const displayed = useTypewriter(text, !live)
  const color = tone === 'primary' ? 'var(--c-text-primary)' : 'var(--c-text-secondary)'
  return (
    <div
      style={{
        fontSize: '14px',
        lineHeight: '1.6',
        color,
        ...(tone === 'primary' ? {} : { fontWeight: 'var(--c-narrative-weight, 275)' as const }),
        whiteSpace: 'pre-wrap',
        wordBreak: 'break-word',
      }}
    >
      {live ? displayed : text}
    </div>
  )
}

function SourceItem({ source }: { source: WebSource }) {
  if (!isHttpUrl(source.url)) return null
  const domain = getDomain(source.url)
  const shortDomain = getDomainShort(source.url)
  return (
    <a
      href={source.url}
      target="_blank"
      rel="noopener noreferrer"
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        padding: '5px 10px',
        borderRadius: '8px',
        textDecoration: 'none',
        color: 'inherit',
        transition: 'background 0.1s',
      }}
      onMouseEnter={(e) => {
        ;(e.currentTarget as HTMLAnchorElement).style.background = 'var(--c-bg-deep)'
      }}
      onMouseLeave={(e) => {
        ;(e.currentTarget as HTMLAnchorElement).style.background = 'transparent'
      }}
    >
      <img
        src={`https://www.google.com/s2/favicons?sz=16&domain=${domain}`}
        alt=""
        width={14}
        height={14}
        style={{ flexShrink: 0, borderRadius: '2px' }}
      />
      <span
        style={{
          fontSize: '12px',
          color: 'var(--c-text-primary)',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          flex: 1,
        }}
      >
        {source.title || domain}
      </span>
      <span style={{ fontSize: '11px', color: 'var(--c-text-muted)', flexShrink: 0 }}>
        {shortDomain}
      </span>
    </a>
  )
}

function getShortName(url: string): string {
  try {
    const hostname = new URL(url).hostname.replace(/^www\./, '')
    const parts = hostname.split('.')
    return parts.length >= 2 ? parts[parts.length - 2] : hostname
  } catch {
    return url
  }
}

function getUrlScheme(url: string): string {
  try {
    return new URL(url).protocol.replace(/:$/, '')
  } catch {
    const match = /^([a-z][a-z0-9+.-]*):/i.exec(url.trim())
    return match?.[1]?.toLowerCase() ?? ''
  }
}

export function WebFetchItem({ fetch: f, live }: { fetch: WebFetchRef; live?: boolean }) {
  const [faviconFailed, setFaviconFailed] = useState(false)
  const isFetching = f.status === 'fetching'
  const isHttp = isHttpUrl(f.url)
  const isFailed = f.status === 'failed'
  const domain = isHttp ? getDomain(f.url) : ''
  const scheme = getUrlScheme(f.url)
  const shortName = isHttp ? getShortName(f.url) : (scheme || 'invalid')
  const primaryText = f.title || (isHttp ? domain : (f.url || 'Invalid URL'))
  const displayedPrimary = useTypewriter(primaryText, !live)
  const primaryShown = live ? displayedPrimary : primaryText
  const secondaryText = typeof f.statusCode === 'number'
    ? `${f.statusCode}`
    : shortName
  const displayedSecondary = useTypewriter(secondaryText, !live)
  const secondaryShown = live ? displayedSecondary : secondaryText
  const content = (
    <>
      <div
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: '20px',
          height: '20px',
          borderRadius: '5px',
          border: '0.5px solid var(--c-border-subtle)',
          background: 'var(--c-bg-menu)',
          flexShrink: 0,
        }}
      >
        {isFetching ? (
          <Loader2 size={11} className="animate-spin" style={{ color: 'var(--c-text-muted)', flexShrink: 0 }} />
        ) : (
          isHttp && !faviconFailed ? (
            <img
              src={`https://www.google.com/s2/favicons?sz=16&domain=${domain}`}
              alt=""
              width={12}
              height={12}
              style={{ flexShrink: 0, borderRadius: '2px' }}
              onError={() => setFaviconFailed(true)}
            />
          ) : (
            <Globe
              size={11}
              style={{
                color: isFailed ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-muted)',
                flexShrink: 0,
              }}
            />
          )
        )}
      </div>
      <span
        style={{
          fontSize: '12px',
          color: 'var(--c-text-secondary)',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          flex: 1,
          minWidth: 0,
        }}
      >
        {primaryShown}
      </span>
      <span style={{ fontSize: '11px', color: 'var(--c-text-muted)', flexShrink: 0 }}>
        {secondaryShown}
      </span>
    </>
  )

  if (!isFetching && isHttp) {
    return (
      <a
        href={f.url}
        target="_blank"
        rel="noopener noreferrer"
        title={f.url}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '7px',
          padding: '3px 0',
          textDecoration: 'none',
          cursor: 'pointer',
        }}
      >
        {content}
      </a>
    )
  }

  return (
    <div
      title={f.url}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '7px',
        padding: '3px 0',
        cursor: isFetching ? 'default' : 'not-allowed',
      }}
    >
      {content}
    </div>
  )
}

const DOT_TOP = COP_TIMELINE_DOT_TOP
const DOT_SIZE = COP_TIMELINE_DOT_SIZE
const SHELL_DOT_TOP = COP_TIMELINE_SHELL_DOT_TOP
const PYTHON_DOT_TOP = COP_TIMELINE_PYTHON_DOT_TOP

/** 与 unified 列表项同一套点线（ChatPage 顶层工具条等复用） */
export function CopTimelineUnifiedRow({
  isFirst,
  isLast,
  multiItems,
  dotTop = COP_TIMELINE_DOT_TOP,
  dotColor,
  paddingBottom = 7,
  children,
}: {
  isFirst: boolean
  isLast: boolean
  multiItems: boolean
  dotTop?: number
  dotColor: string
  paddingBottom?: number
  children: ReactNode
}) {
  return (
    <div style={{ position: 'relative', paddingBottom: isLast ? 0 : paddingBottom }}>
      {!isLast && (
        <div
          style={{
            position: 'absolute',
            left: `${COP_TIMELINE_LINE_LEFT_PX}px`,
            top: `${dotTop + COP_TIMELINE_DOT_SIZE}px`,
            bottom: 0,
            width: '1.5px',
            background: 'var(--c-border-subtle)',
            zIndex: 0,
          }}
        />
      )}
      {multiItems && !isFirst && (
        <div
          style={{
            position: 'absolute',
            left: `${COP_TIMELINE_LINE_LEFT_PX}px`,
            top: 0,
            height: `${dotTop}px`,
            width: '1.5px',
            background: 'var(--c-border-subtle)',
            zIndex: 0,
          }}
        />
      )}
      <div
        style={{
          position: 'absolute',
          left: `${COP_TIMELINE_DOT_LEFT_PX}px`,
          top: `${dotTop}px`,
          width: `${COP_TIMELINE_DOT_SIZE}px`,
          height: `${COP_TIMELINE_DOT_SIZE}px`,
          borderRadius: '50%',
          background: dotColor,
          border: '2px solid var(--c-bg-page)',
          zIndex: 1,
        }}
      />
      {children}
    </div>
  )
}

export function CopTimeline({ steps, sources, narratives, isComplete, codeExecutions, onOpenCodeExecution, activeCodeExecutionId, subAgents, fileOps, webFetches, genericTools, headerOverride, shimmer, live, preserveExpanded, accessToken, baseUrl, thinkingRows, copInlineTextRows, assistantThinking, thinkingStartedAt, trailingAssistantTextPresent }: Props) {
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
  const effectiveStepCount = visibleSteps.length || (codeExecCount + subAgentCount + fileOpCount + webFetchCount + genericToolCount)
  const hasThinkingOnly = hasAnyThinking && effectiveStepCount === 0 && sources.length === 0

  /** 流式中展开；仅 thinking 无工具：后无正文时默认展开，已有正文段则默认收起 */
  const [collapsed, setCollapsed] = useState(() => {
    if (preserveExpanded) return false
    if (!isComplete) return false
    if (thinkingRowList.some((r) => r.live) || legacyThinkingLive) return false
    if (hasThinkingOnly) {
      return !!trailingAssistantTextPresent
    }
    return true
  })

  /** 用户点过标题栏折叠开关后，不再用自动收起覆盖其选择 */
  const userToggledCollapsedRef = useRef(false)

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
    if (trailingAssistantTextPresent) setCollapsed(true)
  }, [trailingAssistantTextPresent, preserveExpanded])

  const aggregatedDurationSec = useMemo(() => {
    let sum = 0
    for (const r of thinkingRowList) {
      const d = r.durationSec
      if (typeof d === 'number' && d > 0) sum += d
    }
    if (legacyThinkingVisible && thinkingRowList.length === 0) {
      sum += initialThinkingElapsedSec(thinkingStartedAt, [], assistantThinking ?? null)
    }
    return sum
  }, [thinkingRowList, legacyThinkingVisible, thinkingStartedAt, assistantThinking])

  const [hovered, setHovered] = useState(false)
  if (
    visibleSteps.length === 0 &&
    textEntries.length === 0 &&
    codeExecCount === 0 &&
    subAgentCount === 0 &&
    fileOpCount === 0 &&
    webFetchCount === 0 &&
    genericToolCount === 0 &&
    !headerOverride &&
    !hasAnyThinking &&
    copInlineList.length === 0
  ) {
    return null
  }

  type UEntry =
    | { kind: 'thinking'; id: string; seq: number; item: { markdown: string; live: boolean; durationSec?: number } }
    | { kind: 'copinline'; id: string; seq: number; item: { text: string; live: boolean } }
    | { kind: 'step'; id: string; seq: number; item: WebSearchPhaseStep }
    | { kind: 'text'; id: string; seq: number; item: SearchNarrative }
    | { kind: 'code'; id: string; seq: number; item: CodeExecution }
    | { kind: 'agent'; id: string; seq: number; item: SubAgentRef }
    | { kind: 'fileop'; id: string; seq: number; item: FileOpRef }
    | { kind: 'fetch'; id: string; seq: number; item: WebFetchRef }
    | { kind: 'generic'; id: string; seq: number; item: NonNullable<Props['genericTools']>[number] }

  const allUnified: UEntry[] = []
  for (const step of visibleSteps) {
    if (step.seq != null) allUnified.push({ kind: 'step', id: step.id, seq: step.seq, item: step })
  }
  for (const narrative of textEntries) {
    allUnified.push({ kind: 'text', id: narrative.id, seq: narrative.seq, item: narrative })
  }
  for (const ce of (codeExecutions ?? [])) {
    if (ce.seq != null) allUnified.push({ kind: 'code', id: ce.id, seq: ce.seq, item: ce })
  }
  for (const a of (subAgents ?? [])) {
    if (a.seq != null) allUnified.push({ kind: 'agent', id: a.id, seq: a.seq, item: a })
  }
  for (const op of (fileOps ?? [])) {
    if (op.seq != null) allUnified.push({ kind: 'fileop', id: op.id, seq: op.seq, item: op })
  }
  for (const wf of (webFetches ?? [])) {
    if (wf.seq != null) allUnified.push({ kind: 'fetch', id: wf.id, seq: wf.seq, item: wf })
  }
  for (const tool of (genericTools ?? [])) {
    if (tool.seq != null) allUnified.push({ kind: 'generic', id: tool.id, seq: tool.seq, item: tool })
  }
  for (const row of thinkingRowList) {
    allUnified.push({
      kind: 'thinking',
      id: row.id,
      seq: row.seq,
      item: { markdown: row.markdown, live: !!row.live, durationSec: row.durationSec },
    })
  }
  for (const row of copInlineList) {
    allUnified.push({
      kind: 'copinline',
      id: row.id,
      seq: row.seq,
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
  const hasContent = totalUnifiableItems > 0 || sources.length > 0
  const useUnified = allUnified.length === totalUnifiableItems && totalUnifiableItems > 0
  if (useUnified) {
    const priority: Record<UEntry['kind'], number> = {
      thinking: -1,
      copinline: 0,
      step: 1,
      text: 2,
      code: 3,
      agent: 4,
      fileop: 5,
      fetch: 6,
      generic: 7,
    }
    allUnified.sort((a, b) => a.seq - b.seq || priority[a.kind] - priority[b.kind] || a.id.localeCompare(b.id))
  }

  const thinkingOnlyUnified =
    useUnified &&
    allUnified.length > 0 &&
    copInlineList.length === 0 &&
    allUnified.every((e) => e.kind === 'thinking')

  /** 仅 thinking、无工具：流式结束后内层不再用折叠卡片，避免与标题重复「Thought for Xs」 */
  const thinkingOnlyCompletedPlain =
    thinkingOnlyUnified && isComplete && !anyThinkingLive && hasThinkingOnly

  const thoughtDurationLabel =
    aggregatedDurationSec > 0
      ? t.copTimelineThoughtForSeconds(aggregatedDurationSec)
      : t.copTimelineThinkingDoneNoDuration

  const autoLabel = isComplete
    ? sources.length > 0
      ? `Reviewed ${sources.length} sources`
      : effectiveStepCount > 0
        ? `${effectiveStepCount} step${effectiveStepCount === 1 ? '' : 's'} completed`
        : hasThinkingOnly
          ? thoughtDurationLabel
          : 'Completed'
    : visibleSteps.length > 0
      ? (visibleSteps[visibleSteps.length - 1]?.label || 'Searching...')
      : effectiveStepCount > 0
        ? t.copTimelineLiveProgress
        : hasThinkingOnly
          ? (anyThinkingLive
              ? t.copThinkingInlineTitle
              : (aggregatedDurationSec > 0 ? thoughtDurationLabel : t.copTimelineThinkingDoneNoDuration))
          : 'Searching...'

  const headerLabel = headerOverride ?? autoLabel
  const dottedStepCount = visibleSteps.length

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
        <CopTimelineHeaderLabel text={headerLabel} live={!!live} shimmer={!!shimmer} />
        {isComplete && sources.length > 0 && (
          <span style={{ fontSize: '12px', color: hovered ? 'var(--c-text-secondary)' : 'var(--c-text-muted)', fontWeight: 400, transition: 'color 0.15s ease' }}>
            {sources.length} sources
          </span>
        )}
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

      <AnimatePresence initial={false}>
        {!collapsed && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.25, ease: [0.4, 0, 0.2, 1] }}
            style={{ overflow: 'hidden' }}
          >
            <div style={{ position: 'relative', paddingLeft: visibleSteps.length > 0 || textEntries.length > 0 || codeExecCount > 0 || subAgentCount > 0 || webFetchCount > 0 || fileOpCount > 0 || hasAnyThinking || copInlineList.length > 0 ? `${COP_TIMELINE_CONTENT_PADDING_LEFT_PX}px` : undefined, paddingTop: '3px', paddingBottom: '3px' }}>

              <AnimatePresence initial={false}>
              {!useUnified && visibleSteps.map((step, idx) => {
                const isFirst = idx === 0
                const isLast = idx === visibleSteps.length - 1
                const hasDot = true
                const multiSteps = dottedStepCount >= 2
                const dotColor =
                  step.status === 'active'
                    ? 'var(--c-text-secondary)'
                    : step.kind === 'finished'
                      ? 'var(--c-text-secondary)'
                      : 'var(--c-text-muted)'

                return (
                  <Fragment key={step.id}>
                    <motion.div
                      initial={{ opacity: 0, x: -6 }}
                      animate={{ opacity: 1, x: 0 }}
                      exit={{ opacity: 0 }}
                      transition={{ duration: 0.22, ease: 'easeOut' }}
                      style={{ position: 'relative', paddingBottom: isLast ? 0 : '14px' }}
                    >

                    {/* Per-item line segments */}
                    {multiSteps && !isLast && (
                      <div style={{ position: 'absolute', left: '-16px', top: `${DOT_TOP + DOT_SIZE}px`, bottom: 0, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                    )}
                    {isLast && (codeExecCount > 0 || textEntries.length > 0 || subAgentCount > 0 || fileOpCount > 0 || webFetchCount > 0) && (
                      <div style={{ position: 'absolute', left: '-16px', top: `${DOT_TOP + DOT_SIZE}px`, bottom: 0, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                    )}
                    {multiSteps && !isFirst && (
                      <div style={{ position: 'absolute', left: '-16px', top: 0, height: `${DOT_TOP}px`, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                    )}

                    {hasDot && (
                      <div
                        style={{
                          position: 'absolute',
                          left: '-19px',
                          top: `${DOT_TOP}px`,
                          width: `${DOT_SIZE}px`,
                          height: `${DOT_SIZE}px`,
                          borderRadius: '50%',
                          background: dotColor,
                          border: '2px solid var(--c-bg-page)',
                          zIndex: 1,
                        }}
                      />
                    )}

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
                      {step.status === 'active' && step.kind !== 'reviewing' && (
                        <Loader2
                          size={12}
                          className="animate-spin"
                          style={{ color: 'var(--c-text-secondary)', flexShrink: 0 }}
                        />
                      )}
                      <TypewriterText
                        text={step.kind === 'reviewing' ? 'Reviewing sources' : step.label}
                        className={step.kind === 'reviewing' && step.status === 'active' ? 'thinking-shimmer-dim' : undefined}
                        live={!!live}
                      />
                    </div>

                    {step.kind === 'searching' && step.queries && step.queries.length > 0 && (
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', marginTop: '6px' }}>
                        {step.queries.map((q) => (
                          <QueryPill key={q} text={q} live={!!live} />
                        ))}
                      </div>
                    )}

                    {step.kind === 'reviewing' && (step.sources ?? sources).length > 0 && (
                      <div
                        style={{
                          marginTop: '8px',
                          borderRadius: '10px',
                          border: '0.5px solid var(--c-border-subtle)',
                          background: 'var(--c-bg-menu)',
                          maxHeight: '240px',
                          overflowY: 'auto',
                          overflowX: 'hidden',
                          padding: '4px',
                        }}
                      >
                        {(step.sources ?? sources).map((s, i) => (
                          <div key={`${s.url}-${i}`}>
                            <SourceItem source={s} />
                          </div>
                        ))}
                      </div>
                    )}
                    </motion.div>
                  </Fragment>
                )
              })}
              </AnimatePresence>

              {useUnified ? (
                <div style={{ display: 'flex', flexDirection: 'column', paddingTop: allUnified.length > 0 ? '0' : undefined }}>
                  {allUnified.map((entry, idx) => {
                    const isFirst = idx === 0
                    const isLast = idx === allUnified.length - 1
                    const multiItems = allUnified.length >= 2
                    const dotTop = entry.kind === 'code' && entry.item.language !== 'shell' ? PYTHON_DOT_TOP : DOT_TOP
                    const dotColor = entry.kind === 'thinking'
                      ? entry.item.live && !entry.item.markdown.trim()
                        ? 'var(--c-text-secondary)'
                        : 'var(--c-border-mid)'
                      : entry.kind === 'copinline'
                        ? 'var(--c-border-mid)'
                      : entry.kind === 'step'
                        ? entry.item.status === 'active'
                          ? 'var(--c-text-secondary)'
                          : 'var(--c-text-muted)'
                        : entry.kind === 'text'
                          ? 'var(--c-border-mid)'
                          : entry.kind === 'code'
                            ? codeExecutionAccentColor(entry.item.status)
                            : entry.kind === 'agent'
                              ? entry.item.status === 'completed' ? 'var(--c-text-muted)' : entry.item.status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-secondary)'
                              : entry.kind === 'fileop'
                                ? entry.item.status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : entry.item.status === 'running' ? 'var(--c-text-secondary)' : 'var(--c-text-muted)'
                                : entry.item.status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : entry.item.status === 'fetching' ? 'var(--c-text-secondary)' : 'var(--c-text-muted)'
                    const dotBackground = dotColor
                    return (
                      <CopTimelineUnifiedRow
                        key={entry.id}
                        isFirst={isFirst}
                        isLast={isLast}
                        multiItems={multiItems}
                        dotTop={dotTop}
                        dotColor={dotBackground}
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
                              {entry.item.status === 'active' && entry.item.kind !== 'reviewing' && (
                                <Loader2
                                  size={12}
                                  className="animate-spin"
                                  style={{ color: 'var(--c-text-secondary)', flexShrink: 0 }}
                                />
                              )}
                              <TypewriterText
                                text={entry.item.kind === 'reviewing' ? 'Reviewing sources' : entry.item.label}
                                className={entry.item.kind === 'reviewing' && entry.item.status === 'active' ? 'thinking-shimmer-dim' : undefined}
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

                            {entry.item.kind === 'reviewing' && (entry.item.sources ?? sources).length > 0 && (
                              <div
                                style={{
                                  marginTop: '8px',
                                  borderRadius: '10px',
                                  border: '0.5px solid var(--c-border-subtle)',
                                  background: 'var(--c-bg-menu)',
                                  maxHeight: '240px',
                                  overflowY: 'auto',
                                  overflowX: 'hidden',
                                  padding: '4px',
                                }}
                              >
                                {(entry.item.sources ?? sources).map((s, sourceIdx) => (
                                  <div key={`${s.url}-${sourceIdx}`}>
                                    <SourceItem source={s} />
                                  </div>
                                ))}
                              </div>
                            )}
                          </div>
                        )}
                        {entry.kind === 'thinking' && (
                          thinkingOnlyCompletedPlain ? (
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
                              <AssistantThinkingMarkdown markdown={entry.item.markdown} live={false} variant="timeline-plain" />
                            </div>
                          ) : (
                            <CopThinkingBlock
                              markdown={entry.item.markdown}
                              live={entry.item.live}
                              thoughtDurationSeconds={entry.item.durationSec ?? 0}
                            />
                          )
                        )}
                        {entry.kind === 'copinline' && (
                          <TimelineNarrativeBody text={entry.item.text} tone="primary" live={entry.item.live} />
                        )}
                        {entry.kind === 'text' && <TimelineNarrativeBody text={entry.item.text} live={!!live} />}
                        {entry.kind === 'code' && (entry.item.language === 'shell'
                          ? <ExecutionCard variant="shell" code={entry.item.code} output={entry.item.output} status={entry.item.status} errorMessage={entry.item.errorMessage} smooth={!!live} />
                          : <CodeExecutionCard language={entry.item.language} code={entry.item.code} output={entry.item.output} errorMessage={entry.item.errorMessage} status={entry.item.status} onOpen={onOpenCodeExecution ? () => onOpenCodeExecution(entry.item as CodeExecution) : undefined} isActive={activeCodeExecutionId === entry.item.id} />
                        )}
                        {entry.kind === 'agent' && (
                          <SubAgentBlock sourceTool={entry.item.sourceTool} nickname={entry.item.nickname} personaId={entry.item.personaId} input={entry.item.input} output={entry.item.output} status={entry.item.status} error={entry.item.error} live={live} currentRunId={entry.item.currentRunId} accessToken={accessToken} baseUrl={baseUrl} />
                        )}
                        {entry.kind === 'fileop' && (
                          <ExecutionCard variant="fileop" toolName={entry.item.toolName} label={entry.item.label} output={entry.item.output} status={entry.item.status} errorMessage={entry.item.errorMessage} smooth={!!live} />
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
                            smooth={!!live}
                          />
                        )}
                      </CopTimelineUnifiedRow>
                    )
                  })}
                </div>
              ) : (
                <>
                  {textEntries.length > 0 && (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', paddingTop: visibleSteps.length > 0 ? '8px' : '0' }}>
                      {textEntries.map((entry) => (
                        <TimelineNarrativeBody key={entry.id} text={entry.text} tone="primary" live={!!live} />
                      ))}
                    </div>
                  )}
                  {codeExecutions && codeExecutions.length > 0 && (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '0px', paddingTop: visibleSteps.length > 0 || textEntries.length > 0 ? '8px' : '0' }}>
                      {codeExecutions.map((ce, idx) => {
                        const isLast = idx === codeExecutions.length - 1
                        const isFirst = idx === 0
                        const multiItems = codeExecutions.length >= 2
                        const dotTop = ce.language === 'shell' ? SHELL_DOT_TOP : PYTHON_DOT_TOP
                        return (
                          <div key={ce.id} style={{ position: 'relative', paddingBottom: isLast ? 0 : '8px' }}>
                            {(multiItems && !isLast) || (isLast && (subAgentCount > 0 || fileOpCount > 0 || webFetchCount > 0)) ? (
                              <div style={{ position: 'absolute', left: '-16px', top: `${dotTop + DOT_SIZE}px`, bottom: 0, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                            ) : null}
                            {multiItems && !isFirst && (
                              <div style={{ position: 'absolute', left: '-16px', top: 0, height: `${dotTop}px`, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                            )}
                            {isFirst && (visibleSteps.length > 0 || textEntries.length > 0) && (
                              <div style={{ position: 'absolute', left: '-16px', top: '-8px', height: `${dotTop + 8}px`, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                            )}
                            <div style={{ position: 'absolute', left: '-19px', top: `${dotTop}px`, width: `${DOT_SIZE}px`, height: `${DOT_SIZE}px`, borderRadius: '50%', background: codeExecutionAccentColor(ce.status), border: '2px solid var(--c-bg-page)', zIndex: 1 }} />
                            {ce.language === 'shell'
                              ? <ExecutionCard variant="shell" code={ce.code} output={ce.output} status={ce.status} errorMessage={ce.errorMessage} smooth={!!live} />
                              : <CodeExecutionCard language={ce.language} code={ce.code} output={ce.output} errorMessage={ce.errorMessage} status={ce.status} onOpen={onOpenCodeExecution ? () => onOpenCodeExecution(ce) : undefined} isActive={activeCodeExecutionId === ce.id} />
                            }
                          </div>
                        )
                      })}
                    </div>
                  )}
                  {subAgents && subAgents.length > 0 && (
                    <div style={{ display: 'flex', flexDirection: 'column', paddingTop: visibleSteps.length > 0 || textEntries.length > 0 || codeExecCount > 0 ? '8px' : '0' }}>
                      {subAgents.map((agent, idx) => {
                        const isFirst = idx === 0
                        const isLast = idx === subAgents.length - 1
                        const dotTop = SHELL_DOT_TOP
                        const hasPrevItems = visibleSteps.length > 0 || textEntries.length > 0 || codeExecCount > 0
                        const multiItems = subAgents.length >= 2
                        return (
                          <div key={agent.id} style={{ position: 'relative', paddingBottom: isLast ? 0 : '6px' }}>
                            {multiItems && !isLast && (
                              <div style={{ position: 'absolute', left: '-16px', top: `${dotTop + DOT_SIZE}px`, bottom: 0, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                            )}
                            {multiItems && !isFirst && (
                              <div style={{ position: 'absolute', left: '-16px', top: 0, height: `${dotTop}px`, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                            )}
                            {isFirst && hasPrevItems && (
                              <div style={{ position: 'absolute', left: '-16px', top: '-8px', height: `${dotTop + 8}px`, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                            )}
                            <div style={{ position: 'absolute', left: '-19px', top: `${dotTop}px`, width: `${DOT_SIZE}px`, height: `${DOT_SIZE}px`, borderRadius: '50%', background: agent.status === 'completed' ? 'var(--c-text-muted)' : agent.status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-secondary)', border: '2px solid var(--c-bg-page)', zIndex: 1 }} />
                            <SubAgentBlock sourceTool={agent.sourceTool} nickname={agent.nickname} personaId={agent.personaId} input={agent.input} output={agent.output} status={agent.status} error={agent.error} live={live} currentRunId={agent.currentRunId} accessToken={accessToken} baseUrl={baseUrl} />
                          </div>
                        )
                      })}
                    </div>
                  )}
                  {fileOps && fileOps.length > 0 && (
                    <div style={{ display: 'flex', flexDirection: 'column', paddingTop: visibleSteps.length > 0 || textEntries.length > 0 || codeExecCount > 0 || subAgentCount > 0 ? '8px' : '0' }}>
                      {fileOps.map((op, idx) => {
                        const isFirst = idx === 0
                        const isLast = idx === fileOps.length - 1
                        const dotTop = SHELL_DOT_TOP
                        const hasPrevItems = visibleSteps.length > 0 || textEntries.length > 0 || codeExecCount > 0 || subAgentCount > 0
                        const multiItems = fileOps.length >= 2
                        const dotColor = op.status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : op.status === 'running' ? 'var(--c-text-secondary)' : 'var(--c-text-muted)'
                        return (
                          <div key={op.id} style={{ position: 'relative', paddingBottom: isLast ? 0 : '4px' }}>
                            {multiItems && !isLast && (
                              <div style={{ position: 'absolute', left: '-16px', top: `${dotTop + DOT_SIZE}px`, bottom: 0, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                            )}
                            {multiItems && !isFirst && (
                              <div style={{ position: 'absolute', left: '-16px', top: 0, height: `${dotTop}px`, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                            )}
                            {isFirst && hasPrevItems && (
                              <div style={{ position: 'absolute', left: '-16px', top: '-8px', height: `${dotTop + 8}px`, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                            )}
                            <div style={{ position: 'absolute', left: '-19px', top: `${dotTop}px`, width: `${DOT_SIZE}px`, height: `${DOT_SIZE}px`, borderRadius: '50%', background: dotColor, border: '2px solid var(--c-bg-page)', zIndex: 1 }} />
                            <ExecutionCard variant="fileop" toolName={op.toolName} label={op.label} output={op.output} status={op.status} errorMessage={op.errorMessage} smooth={!!live} />
                          </div>
                        )
                      })}
                    </div>
                  )}
                  {webFetches && webFetches.length > 0 && (
                    <div style={{ display: 'flex', flexDirection: 'column', paddingTop: visibleSteps.length > 0 || textEntries.length > 0 || codeExecCount > 0 || subAgentCount > 0 || fileOpCount > 0 ? '8px' : '0' }}>
                      {webFetches.map((f, idx) => {
                        const isFirst = idx === 0
                        const isLast = idx === webFetches.length - 1
                        const dotTop = SHELL_DOT_TOP
                        const hasPrevItems = visibleSteps.length > 0 || textEntries.length > 0 || codeExecCount > 0 || subAgentCount > 0 || fileOpCount > 0
                        const multiItems = webFetches.length >= 2
                        const dotColor = f.status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : f.status === 'fetching' ? 'var(--c-text-secondary)' : 'var(--c-text-muted)'
                        return (
                          <div key={f.id} style={{ position: 'relative', paddingBottom: isLast ? 0 : '4px' }}>
                            {multiItems && !isLast && (
                              <div style={{ position: 'absolute', left: '-16px', top: `${dotTop + DOT_SIZE}px`, bottom: 0, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                            )}
                            {multiItems && !isFirst && (
                              <div style={{ position: 'absolute', left: '-16px', top: 0, height: `${dotTop}px`, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                            )}
                            {isFirst && hasPrevItems && (
                              <div style={{ position: 'absolute', left: '-16px', top: '-8px', height: `${dotTop + 8}px`, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                            )}
                            <div style={{ position: 'absolute', left: '-19px', top: `${dotTop}px`, width: `${DOT_SIZE}px`, height: `${DOT_SIZE}px`, borderRadius: '50%', background: dotColor, border: '2px solid var(--c-bg-page)', zIndex: 1 }} />
                            <WebFetchItem fetch={f} live={!!live} />
                          </div>
                        )
                      })}
                    </div>
                  )}
                </>
              )}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
