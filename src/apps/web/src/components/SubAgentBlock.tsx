import { useState, useRef, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { ChevronRight, ChevronDown, Loader2 } from 'lucide-react'
import { useLocale } from '../contexts/LocaleContext'
import { useTypewriter } from '../hooks/useTypewriter'

type Props = {
  nickname?: string
  role?: string
  personaId?: string
  input?: string
  output?: string
  status: 'spawning' | 'active' | 'completed' | 'failed' | 'closed'
  error?: string
  live?: boolean
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

export function SubAgentBlock({ nickname, personaId, input, output, status, error, live }: Props) {
  const [expanded, setExpanded] = useState(false)
  const { t } = useLocale()
  const outputRef = useRef<HTMLDivElement>(null)
  const [scrollEdge, setScrollEdge] = useState<ScrollEdge>('none')

  const rawLabel = nickname || personaId || t.agentSubAgent
  const displayedLabel = useTypewriter(live ? rawLabel : '')
  const label = live ? displayedLabel : rawLabel
  const displayOutput = output?.trim() ? output : error?.trim() ? error : undefined
  const hasOutput = !!displayOutput
  const isWaiting = status === 'spawning' || status === 'active'

  useEffect(() => {
    const el = outputRef.current
    if (!el || !expanded) return
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
  }, [expanded, displayOutput])

  const mask = maskFor(scrollEdge)

  return (
    <div style={{ maxWidth: 'min(100%, 720px)' }}>
      <div
        role="button"
        tabIndex={0}
        onClick={() => setExpanded((p) => !p)}
        onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setExpanded((p) => !p) } }}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: '5px',
          padding: '4px 0',
          border: 'none',
          cursor: 'pointer',
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
          {label}
        </span>
        {expanded
          ? <ChevronDown size={12} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
          : <ChevronRight size={12} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} strokeWidth={2} />
        }
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
            <div style={{ borderRadius: '8px', background: 'var(--c-bg-menu)', overflow: 'hidden', marginTop: '4px' }}>
              {/* type label */}
              <div style={{ padding: '6px 10px 2px', fontSize: '10px', color: 'var(--c-text-muted)', fontFamily: MONO, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                agent
              </div>

              {/* Input */}
              {input?.trim() && (
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
                    {input.trim()}
                  </pre>
                </div>
              )}

              {/* Output */}
              {(hasOutput || isWaiting) && (
                <div style={{ position: 'relative' }}>
                  {hasOutput && (
                    <div style={{ padding: '2px 10px 0', fontSize: '10px', color: 'var(--c-text-muted)', fontFamily: MONO, marginBottom: '2px' }}>
                      {t.agentOutput}
                    </div>
                  )}
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
                    {hasOutput ? (
                      <pre style={{
                        margin: 0,
                        fontSize: '10.5px',
                        lineHeight: '1.4',
                        color: status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-secondary)',
                        fontFamily: MONO,
                        whiteSpace: 'pre-wrap',
                        wordBreak: 'break-word',
                      }}>
                        {displayOutput!.trimEnd()}
                      </pre>
                    ) : (
                      <div style={{ minHeight: '48px', display: 'flex', alignItems: 'center', padding: '4px 0' }}>
                        <span style={{ fontFamily: MONO, fontSize: '10.5px', color: 'var(--c-text-muted)', animation: 'terminal-blink 1.2s step-start infinite' }}>▮</span>
                      </div>
                    )}
                  </div>
                </div>
              )}

              {/* No output */}
              {!hasOutput && !isWaiting && (
                <div style={{ padding: '4px 10px 8px', fontSize: '10.5px', color: 'var(--c-text-muted)', fontStyle: 'italic', fontFamily: MONO }}>
                  {t.agentNoOutput}
                </div>
              )}

              {/* Status bottom-right */}
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
