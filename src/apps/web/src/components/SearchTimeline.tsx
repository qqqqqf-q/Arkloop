import { useState, Fragment } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { ChevronRight, Loader2, Search } from 'lucide-react'
import type { WebSource } from '../storage'
import { CodeExecutionCard, type CodeExecution } from './ThinkingBlock'

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
        gap: '5px',
        padding: '2px 8px',
        borderRadius: '8px',
        background: 'var(--c-bg-menu)',
        border: '0.5px solid var(--c-border-subtle)',
        fontSize: '12px',
        color: 'var(--c-text-secondary)',
        lineHeight: '18px',
      }}
    >
      <Search size={11} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
      {text}
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

export function SearchTimeline({ steps, sources, isComplete, codeExecutions, onOpenCodeExecution, headerOverride, shimmer }: Props) {
  const [collapsed, setCollapsed] = useState(() => isComplete)

  if (steps.length === 0) return null

  const stepsExcludingFinished = steps.filter(s => s.kind !== 'finished').length

  const autoLabel = isComplete
    ? sources.length > 0
      ? `Reviewed ${sources.length} sources`
      : stepsExcludingFinished > 0
        ? `${stepsExcludingFinished} steps completed`
        : 'Completed'
    : steps[steps.length - 1]?.label || 'Searching...'

  const headerLabel = headerOverride ?? autoLabel

  return (
    <motion.div
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3, ease: 'easeOut' }}
      style={{ maxWidth: '663px' }}
    >
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
            <div style={{ position: 'relative', paddingLeft: '24px', paddingTop: '2px', paddingBottom: '2px' }}>
              {/* 连贯实线 */}
              <div
                style={{
                  position: 'absolute',
                  left: '8px',
                  top: '12px',
                  bottom: '10px',
                  width: '1.5px',
                  background: 'var(--c-border-subtle)',
                }}
              />

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
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', paddingBottom: '14px' }}>
                        {codeExecutions.map((ce) => (
                          <CodeExecutionCard
                            key={ce.id}
                            language={ce.language}
                            code={ce.code}
                            output={ce.output}
                            exitCode={ce.exitCode}
                            onOpen={onOpenCodeExecution ? () => onOpenCodeExecution(ce) : undefined}
                          />
                        ))}
                      </div>
                    )}
                    <motion.div
                      initial={{ opacity: 0, x: -10 }}
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
                          top: '5px',
                          width: '8px',
                          height: '8px',
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
                          padding: '4px',
                        }}
                      >
                        {sources.map((s, i) => (
                          <SourceItem key={`${s.url}-${i}`} source={s} />
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
                <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', paddingTop: '8px' }}>
                  {codeExecutions.map((ce) => (
                    <CodeExecutionCard
                      key={ce.id}
                      language={ce.language}
                      code={ce.code}
                      output={ce.output}
                      exitCode={ce.exitCode}
                      onOpen={onOpenCodeExecution ? () => onOpenCodeExecution(ce) : undefined}
                    />
                  ))}
                </div>
              )}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </motion.div>
  )
}
