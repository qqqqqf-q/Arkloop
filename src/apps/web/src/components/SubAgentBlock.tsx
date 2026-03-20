import { useState, useRef, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { ChevronRight, ChevronDown, Loader2 } from 'lucide-react'
import { useLocale } from '../contexts/LocaleContext'
import { useTypewriter } from '../hooks/useTypewriter'
import { useSubAgentCop } from '../hooks/useSubAgentCop'
import { CopTimeline } from './CopTimeline'

type Props = {
  nickname?: string
  role?: string
  personaId?: string
  input?: string
  output?: string
  status: 'spawning' | 'active' | 'completed' | 'failed' | 'closed'
  error?: string
  live?: boolean
  currentRunId?: string
  accessToken?: string
  baseUrl?: string
}

type Status = Props['status']

const MONO = 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace'
const expandTransition = { duration: 0.25, ease: [0.4, 0, 0.2, 1] as const }

type ScrollEdge = 'top' | 'bottom' | 'both' | 'none'

function maskFor(edge: ScrollEdge): string | undefined {
  if (edge === 'top') return 'linear-gradient(to bottom, black 85%, transparent)'
  if (edge === 'bottom') return 'linear-gradient(to top, black 85%, transparent)'
  if (edge === 'both') return 'linear-gradient(to bottom, transparent, black 12%, black 88%, transparent)'
  return undefined
}

export function SubAgentBlock({
  nickname,
  personaId,
  input,
  output,
  status,
  error,
  live,
  currentRunId,
  accessToken = '',
  baseUrl = '',
}: Props) {
  const [expanded, setExpanded] = useState(false)
  const { t } = useLocale()
  const outputRef = useRef<HTMLDivElement>(null)
  const [scrollEdge, setScrollEdge] = useState<ScrollEdge>('none')

  const displayOutput = output?.trim() ? output : error?.trim() ? error : undefined
  const rawLabel = nickname || personaId || t.agentSubAgent
  const streamTw = !!live
  const displayedLabel = useTypewriter(rawLabel, !streamTw)
  const inputTrimmed = input?.trim() ?? ''
  const displayedInput = useTypewriter(inputTrimmed, !streamTw)
  const outputForTw = displayOutput?.trimEnd() ?? ''
  const displayedOutput = useTypewriter(outputForTw, !streamTw)

  const cop = useSubAgentCop({
    runId: currentRunId,
    accessToken,
    baseUrl,
    enabled: expanded && !!currentRunId,
  })

  const hasCop = cop.steps.length > 0 || cop.sources.length > 0

  // COP 激活时不显示原始 output 区域（除非最终 output 有意义且 COP 已完成）
  const showRawOutput = !hasCop && !!displayOutput
  const isWaiting = status === 'spawning' || status === 'active'
  const expandable = isWaiting || !!displayOutput || !!currentRunId || hasCop

  useEffect(() => {
    const el = outputRef.current
    if (!el || !expanded || hasCop) return
    const update = () => {
      const { scrollTop, scrollHeight, clientHeight } = el
      if (scrollHeight <= clientHeight + 1) { setScrollEdge('none'); return }
      const atTop = scrollTop <= 1
      const atBottom = scrollTop + clientHeight >= scrollHeight - 1
      if (atTop && atBottom) setScrollEdge('none')
      else if (atTop) setScrollEdge('top')
      else if (atBottom) setScrollEdge('bottom')
      else setScrollEdge('both')
    }
    update()
    el.addEventListener('scroll', update, { passive: true })
    return () => el.removeEventListener('scroll', update)
  }, [expanded, displayOutput, hasCop])

  const mask = maskFor(scrollEdge)

  return (
    <div style={{ maxWidth: 'min(100%, 720px)' }}>
      <div
        role="button"
        tabIndex={0}
        onClick={() => { if (expandable) setExpanded((p) => !p) }}
        onKeyDown={(e) => {
          if ((e.key === 'Enter' || e.key === ' ') && expandable) {
            e.preventDefault()
            setExpanded((p) => !p)
          }
        }}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: '5px',
          padding: '4px 0',
          border: 'none',
          cursor: expandable ? 'pointer' : 'default',
          width: 'fit-content',
          maxWidth: '100%',
          background: 'transparent',
          userSelect: 'none',
          WebkitUserSelect: 'none',
        }}
      >
        <span style={{
          fontSize: '11px',
          fontFamily: MONO,
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          lineHeight: '16px',
          color: 'var(--c-text-muted)',
          transition: 'color 150ms ease',
        }}>
          {streamTw ? displayedLabel : rawLabel}
        </span>
        {expandable && (
          expanded
            ? <ChevronDown size={12} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
            : <ChevronRight size={12} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
        )}
      </div>

      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={expandTransition}
            style={{ overflow: 'hidden' }}
          >
            <div style={{
              borderRadius: '8px',
              background: 'var(--c-bg-menu)',
              overflow: 'hidden',
              marginTop: '4px',
            }}>
              {/* type label */}
              <div style={{
                padding: '6px 10px 2px',
                fontSize: '10px',
                color: 'var(--c-text-muted)',
                fontFamily: MONO,
                textTransform: 'uppercase',
                letterSpacing: '0.05em',
              }}>
                agent
              </div>

              {/* Input */}
              {inputTrimmed && (
                <div style={{ padding: '2px 10px 8px' }}>
                  <div style={{ fontSize: '10px', color: 'var(--c-text-muted)', fontFamily: MONO, marginBottom: '2px' }}>
                    {t.agentInput}
                  </div>
                  <pre style={{
                    margin: 0,
                    fontSize: '11px',
                    lineHeight: '1.5',
                    color: 'var(--c-text-primary)',
                    fontFamily: MONO,
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-word',
                    maxHeight: '120px',
                    overflowY: 'auto',
                  }}>
                    {streamTw ? displayedInput : inputTrimmed}
                  </pre>
                </div>
              )}

              {/* 嵌套 COP：sub-agent 的 search timeline */}
              {hasCop && (
                <div style={{
                  padding: '4px 10px 8px',
                  borderLeft: '2px solid var(--c-border-subtle)',
                  marginLeft: '10px',
                  marginRight: '10px',
                  marginBottom: '4px',
                }}>
                  <CopTimeline
                    steps={cop.steps}
                    sources={cop.sources}
                    isComplete={cop.isComplete}
                    live={!!(live || cop.isStreaming)}
                  />
                </div>
              )}

              {/* 等待中且无 COP：显示光标 */}
              {!hasCop && isWaiting && !cop.isStreaming && (
                <div style={{ padding: '4px 10px 8px' }}>
                  <div style={{ minHeight: '48px', display: 'flex', alignItems: 'center', padding: '4px 0' }}>
                    <span style={{
                      fontFamily: MONO,
                      fontSize: '10.5px',
                      color: 'var(--c-text-muted)',
                      animation: 'terminal-blink 1.2s step-start infinite',
                    }}>▮</span>
                  </div>
                </div>
              )}

              {/* 原始 output（仅 COP 未激活时显示） */}
              {showRawOutput && (
                <div style={{ position: 'relative' }}>
                  <div style={{ padding: '2px 10px 0', fontSize: '10px', color: 'var(--c-text-muted)', fontFamily: MONO, marginBottom: '2px' }}>
                    {t.agentOutput}
                  </div>
                  <div
                    ref={outputRef}
                    style={{
                      maxHeight: '240px',
                      overflowY: 'auto',
                      padding: '4px 10px 8px',
                      maskImage: mask,
                      WebkitMaskImage: mask,
                    }}
                  >
                    <pre style={{
                      margin: 0,
                      fontSize: '10.5px',
                      lineHeight: '1.4',
                      color: status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-secondary)',
                      fontFamily: MONO,
                      whiteSpace: 'pre-wrap',
                      wordBreak: 'break-word',
                    }}>
                      {streamTw ? displayedOutput : outputForTw}
                    </pre>
                  </div>
                </div>
              )}

              {/* 无内容 */}
              {!hasCop && !showRawOutput && !isWaiting && (
                <div style={{ padding: '4px 10px 8px', fontSize: '10.5px', color: 'var(--c-text-muted)', fontStyle: 'italic', fontFamily: MONO }}>
                  {t.agentNoOutput}
                </div>
              )}

              {/* Status */}
              <div style={{ display: 'flex', justifyContent: 'flex-end', padding: '0 10px 6px' }}>
                <StatusBadge status={status} />
              </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}

function StatusBadge({ status }: { status: Status }) {
  const { t } = useLocale()

  if (status === 'spawning') {
    return (
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: '4px', fontSize: '10px', color: 'var(--c-text-muted)' }}>
        <Loader2 size={10} className="animate-spin" />
        {t.agentSpawning}
      </span>
    )
  }
  if (status === 'active') {
    return (
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: '4px', fontSize: '10px', color: 'var(--c-text-muted)' }}>
        <Loader2 size={10} className="animate-spin" />
        {t.agentRunning}
      </span>
    )
  }
  if (status === 'failed') {
    return (
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px', fontSize: '10px', color: 'var(--c-status-error-text, #ef4444)' }}>
        {t.agentFailed}
      </span>
    )
  }
  if (status === 'closed') {
    return (
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px', fontSize: '10px', color: 'var(--c-text-muted)' }}>
        {t.agentClosed}
      </span>
    )
  }
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '2px', fontSize: '10px', color: 'var(--c-text-muted)' }}>
      {t.agentCompleted}
    </span>
  )
}
