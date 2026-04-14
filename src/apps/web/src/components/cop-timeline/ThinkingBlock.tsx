import { useState, useRef, useEffect, useLayoutEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useTypewriter } from '../../hooks/useTypewriter'
import { useLocale } from '../../contexts/LocaleContext'
import { MarkdownRenderer } from '../MarkdownRenderer'
import { CopTimelineHeaderLabel } from './CopTimelineHeader'

const THINK_MAX_LINES = 10
const THINK_LINE_HEIGHT_PX = 21.025
const THINK_COLLAPSED_HEIGHT = THINK_MAX_LINES * THINK_LINE_HEIGHT_PX
const THINK_FADE_HEIGHT = THINK_LINE_HEIGHT_PX * 2

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
  const [expanded, setExpanded] = useState(false)
  const [fullHeight, setFullHeight] = useState<number | null>(null)
  const contentRef = useRef<HTMLDivElement>(null)

  useLayoutEffect(() => {
    const el = contentRef.current
    if (!el) return
    const prev = el.style.maxHeight
    el.style.maxHeight = 'none'
    const h = el.scrollHeight
    el.style.maxHeight = prev
    setFullHeight(h > THINK_COLLAPSED_HEIGHT + 1 ? h : null)
  }, [markdown])

  const overflows = fullHeight !== null
  const rootClass =
    variant === 'timeline-plain'
      ? 'cop-thinking-output-md cop-thinking-output-md--timeline-plain'
      : 'cop-thinking-output-md'

  const isCollapsed = overflows && !expanded
  const fadeMask = `linear-gradient(to bottom, black calc(100% - ${THINK_FADE_HEIGHT}px), transparent)`

  return (
    <div className={rootClass}>
      <div
        ref={contentRef}
        className="cop-thinking-body"
        style={{
          maxHeight: isCollapsed
            ? `${THINK_COLLAPSED_HEIGHT}px`
            : fullHeight != null
              ? `${fullHeight}px`
              : undefined,
          overflow: 'hidden',
          ...(isCollapsed
            ? { WebkitMaskImage: fadeMask, maskImage: fadeMask }
            : { WebkitMaskImage: 'none', maskImage: 'none' }),
        }}
      >
        {!markdown.trim() && live ? (
          <span className="thinking-shimmer cop-thinking-output-placeholder">{t.assistantStreamThinkingPlaceholder}</span>
        ) : (
          <MarkdownRenderer content={live ? displayed : markdown} disableMath streaming={live} trimTrailingMargin compact />
        )}
      </div>
      {overflows && (
        <button
          type="button"
          onClick={() => setExpanded((p) => !p)}
          className="cop-think-toggle-btn"
        >
          {expanded ? t.copThinkShowLess : t.copThinkShowMore}
        </button>
      )}
    </div>
  )
}

export type CopThoughtSummaryRowProps = {
  markdown: string
  live: boolean
  thoughtDurationSeconds: number
  startedAtMs?: number
}

export function CopThoughtSummaryRow({ markdown, live, thoughtDurationSeconds, startedAtMs }: CopThoughtSummaryRowProps) {
  const { t } = useLocale()
  const [expanded, setExpanded] = useState(false)
  const [liveElapsed, setLiveElapsed] = useState(0)
  const liveLabel = live
    ? (liveElapsed > 0 ? t.copTimelineThinkingForSeconds(liveElapsed) : t.copThinkingInlineTitle)
    : ''
  const thoughtLabel = thoughtDurationSeconds > 0 ? t.copTimelineThoughtForSeconds(thoughtDurationSeconds) : t.copTimelineThinkingDoneNoDuration
  const currentLabel = live ? liveLabel : thoughtLabel
  const shouldAnimateLabel = currentLabel !== ''

  useEffect(() => {
    if (!live || !startedAtMs) {
      setLiveElapsed(0)
      return
    }
    setLiveElapsed(Math.max(0, Math.round((Date.now() - startedAtMs) / 1000)))
    const id = setInterval(() => {
      setLiveElapsed(Math.max(0, Math.round((Date.now() - startedAtMs) / 1000)))
    }, 1000)
    return () => clearInterval(id)
  }, [live, startedAtMs])

  return (
    <div>
      <button
        type="button"
        className="cop-thinking-card-trigger"
        data-testid="cop-thought-summary-row"
        onClick={() => setExpanded((prev) => !prev)}
      >
        <span className={live ? 'cop-thinking-card-trigger-label thinking-shimmer-dim' : 'cop-thinking-card-trigger-label'}>
          <CopTimelineHeaderLabel
            text={currentLabel}
            phaseKey={live ? 'thinking' : 'thought'}
            incremental={shouldAnimateLabel}
          />
        </span>
        {expanded ? (
          <ChevronDown size={12} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
        ) : (
          <ChevronRight size={12} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
        )}
      </button>
      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.2, ease: 'easeOut' }}
            style={{ overflow: 'hidden' }}
          >
            <div className="cop-thinking-card-outer">
              <div className="cop-thinking-card-scroll">
                <AssistantThinkingMarkdown markdown={markdown} live={live} />
              </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}

export function TimelineNarrativeBody({ text, tone = 'secondary', live }: { text: string; tone?: 'primary' | 'secondary'; live?: boolean }) {
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
