import { useState, useEffect, useRef, Fragment } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { ChevronRight, Loader2, Search } from 'lucide-react'
import type { WebSource } from '../storage'
import { CodeExecutionCard, type CodeExecution } from './ThinkingBlock'
import { ShellExecutionBlock } from './ShellExecutionBlock'

export type SearchStep = {
  id: string
  kind: 'planning' | 'searching' | 'reviewing' | 'finished'
  label: string
  status: 'active' | 'done'
  queries?: string[]
}

type Props = {
  steps: SearchStep[]
  sources: WebSource[]
  isComplete: boolean
  codeExecutions?: CodeExecution[]
  onOpenCodeExecution?: (ce: CodeExecution) => void
  headerOverride?: string
  shimmer?: boolean
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

function QueryPill({ text }: { text: string }) {
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
          animation: 'timeline-slide-in 0.3s ease-out both',
        }}
      >
        <Search size={11} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
        {text}
      </span>
    </span>
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

const DOT_TOP = 5
const DOT_SIZE = 8

export function SearchTimeline({ steps, sources, isComplete, codeExecutions, onOpenCodeExecution, headerOverride, shimmer }: Props) {
  const [collapsed, setCollapsed] = useState(() => isComplete)
  const prevIsCompleteRef = useRef(isComplete)
  useEffect(() => {
    if (isComplete && !prevIsCompleteRef.current) {
      const timer = setTimeout(() => setCollapsed(true), 80)
      prevIsCompleteRef.current = isComplete
      return () => clearTimeout(timer)
    }
    prevIsCompleteRef.current = isComplete
  }, [isComplete])

  const codeExecCount = codeExecutions?.length ?? 0
  if (steps.length === 0 && codeExecCount === 0) return null

  const stepsExcludingFinished = steps.filter(s => s.kind !== 'finished').length
  const effectiveStepCount = stepsExcludingFinished || codeExecCount

  const autoLabel = isComplete
    ? sources.length > 0
      ? `Reviewed ${sources.length} sources`
      : effectiveStepCount > 0
        ? `${effectiveStepCount} step${effectiveStepCount === 1 ? '' : 's'} completed`
        : 'Completed'
    : steps.length > 0
      ? (steps[steps.length - 1]?.label || 'Searching...')
      : 'Thinking'

  const headerLabel = headerOverride ?? autoLabel
  const dottedStepCount = steps.filter(s => s.kind !== 'searching').length

  return (
    <div style={{ maxWidth: '663px' }}>
      <button
        type="button"
        onClick={() => setCollapsed((p) => !p)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '6px',
          padding: '6px 0',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          color: 'var(--c-text-secondary)',
          fontSize: '13px',
          fontWeight: 500,
        }}
      >
        {!isComplete ? (
          <Loader2
            size={13}
            className="animate-spin"
            style={{ flexShrink: 0, color: 'var(--c-text-secondary)' }}
          />
        ) : null}
        <span className={shimmer ? 'thinking-shimmer' : undefined}>{headerLabel}</span>
        {isComplete && (
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
            <div style={{ position: 'relative', paddingLeft: steps.length > 0 || codeExecCount > 0 ? '24px' : undefined, paddingTop: '2px', paddingBottom: '2px' }}>
              {dottedStepCount >= 2 && (
                <div
                  key={`tl-${dottedStepCount}`}
                  style={{
                    position: 'absolute',
                    left: '8px',
                    top: '12px',
                    bottom: '10px',
                    width: '1.5px',
                    background: 'var(--c-border-subtle)',
                    transformOrigin: 'top',
                    animation: 'timeline-line-grow 0.35s cubic-bezier(0.4, 0, 0.2, 1) both',
                  }}
                />
              )}

              <AnimatePresence initial={false}>
              {steps.map((step, idx) => {
                const isLast = idx === steps.length - 1
                const hasDot = step.kind !== 'searching'
                const dotColor =
                  step.status === 'active'
                    ? 'var(--c-text-secondary)'
                    : step.kind === 'finished'
                      ? 'var(--c-text-secondary)'
                      : 'var(--c-text-muted)'

                return (
                  <Fragment key={step.id}>
                    {step.kind === 'finished' && codeExecutions && codeExecutions.length > 0 && (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', paddingBottom: '14px', position: 'relative' }}>
                        {codeExecutions.map((ce) =>
                          ce.language === 'shell'
                            ? <ShellExecutionBlock key={ce.id} code={ce.code} output={ce.output} exitCode={ce.exitCode} isStreaming={!isComplete} />
                            : <CodeExecutionCard
                                key={ce.id}
                                language={ce.language}
                                code={ce.code}
                                output={ce.output}
                                exitCode={ce.exitCode}
                                onOpen={onOpenCodeExecution ? () => onOpenCodeExecution(ce) : undefined}
                              />
                        )}
                      </div>
                    )}
                    <motion.div
                      initial={{ opacity: 0, x: -6 }}
                      animate={{ opacity: 1, x: 0 }}
                      exit={{ opacity: 0 }}
                      transition={{ duration: 0.22, ease: 'easeOut' }}
                      style={{ position: 'relative', paddingBottom: isLast ? 0 : '14px' }}
                    >

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
                      {step.status === 'active' && (
                        <Loader2
                          size={12}
                          className="animate-spin"
                          style={{ color: 'var(--c-text-secondary)', flexShrink: 0 }}
                        />
                      )}
                      <span className={step.kind === 'reviewing' && step.status === 'active' ? 'thinking-shimmer-dim' : undefined}>
                        {step.kind === 'reviewing' ? 'Reviewing sources' : step.label}
                      </span>
                    </div>

                    {step.kind === 'searching' && step.queries && step.queries.length > 0 && (
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', marginTop: '6px' }}>
                        {step.queries.map((q) => (
                          <QueryPill key={q} text={q} />
                        ))}
                      </div>
                    )}

                    {step.kind === 'reviewing' && step.status === 'done' && sources.length > 0 && (
                      <div
                        style={{
                          marginTop: '8px',
                          borderRadius: '10px',
                          border: '0.5px solid var(--c-border-subtle)',
                          background: 'var(--c-bg-menu)',
                          maxHeight: '240px',
                          overflowY: 'auto',
                          overflow: 'hidden',
                          padding: '4px',
                        }}
                      >
                        {sources.map((s, i) => (
                          <div
                            key={`${s.url}-${i}`}
                            style={{
                              animation: 'timeline-slide-in 0.25s ease-out both',
                              animationDelay: `${i * 0.03}s`,
                            }}
                          >
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

              {/* 有 finished 步骤时不在底部渲染，已在步骤循环内处理 */}
              {codeExecutions && codeExecutions.length > 0 && !steps.some((s) => s.kind === 'finished') && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '0px', paddingTop: steps.length > 0 ? '8px' : '0' }}>
                  {codeExecutions.map((ce, idx) => {
                    const isLast = idx === codeExecutions.length - 1
                    const showDot = codeExecutions.length > 0
                    const showLine = codeExecutions.length >= 2
                    return (
                      <div
                        key={ce.id}
                        style={{
                          position: 'relative',
                          paddingBottom: isLast ? 0 : '8px',
                        }}
                      >
                        {showLine && !isLast && (
                          <div
                            style={{
                              position: 'absolute',
                              left: '-15.75px',
                              top: '13px',
                              bottom: '-13px',
                              width: '1.5px',
                              background: 'var(--c-border-subtle)',
                            }}
                          />
                        )}
                        {showDot && (
                          <div
                            style={{
                              position: 'absolute',
                              left: '-19px',
                              top: '50%',
                              transform: 'translateY(-50%)',
                              width: `${DOT_SIZE}px`,
                              height: `${DOT_SIZE}px`,
                              borderRadius: '50%',
                              background: ce.exitCode != null
                                ? ce.exitCode === 0 ? 'var(--c-border-subtle)' : '#ef4444'
                                : 'var(--c-text-secondary)',
                              border: '2px solid var(--c-bg-page)',
                              zIndex: 1,
                            }}
                          />
                        )}
                        {ce.language === 'shell'
                          ? <ShellExecutionBlock code={ce.code} output={ce.output} exitCode={ce.exitCode} isStreaming={!isComplete} />
                          : <CodeExecutionCard
                              language={ce.language}
                              code={ce.code}
                              output={ce.output}
                              exitCode={ce.exitCode}
                              onOpen={onOpenCodeExecution ? () => onOpenCodeExecution(ce) : undefined}
                            />
                        }
                      </div>
                    )
                  })}
                </div>
              )}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
