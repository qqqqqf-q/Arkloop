import { useState, useEffect, useRef, Fragment } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { ChevronRight, Globe, Loader2, Search } from 'lucide-react'
import { useTypewriter } from '../hooks/useTypewriter'
import type { WebSource } from '../storage'
import type { SubAgentRef, FileOpRef, WebFetchRef } from '../storage'
import { codeExecutionAccentColor } from '../codeExecutionStatus'
import { CodeExecutionCard, type CodeExecution } from './ThinkingBlock'
import { ShellExecutionBlock } from './ShellExecutionBlock'
import { FileOpBlock } from './FileOpBlock'
import { SubAgentBlock } from './SubAgentBlock'

export type SearchStep = {
  id: string
  kind: 'planning' | 'searching' | 'reviewing' | 'finished'
  label: string
  status: 'active' | 'done'
  queries?: string[]
  seq?: number
}

export type SearchNarrative = {
  id: string
  text: string
  seq: number
}

type Props = {
  steps: SearchStep[]
  sources: WebSource[]
  narratives?: SearchNarrative[]
  isComplete: boolean
  codeExecutions?: CodeExecution[]
  onOpenCodeExecution?: (ce: CodeExecution) => void
  activeCodeExecutionId?: string
  subAgents?: SubAgentRef[]
  fileOps?: FileOpRef[]
  webFetches?: WebFetchRef[]
  headerOverride?: string
  shimmer?: boolean
  live?: boolean
  accessToken?: string
  baseUrl?: string
}

function TypewriterText({ text, className, active }: { text: string; className?: string; active: boolean }) {
  const displayed = useTypewriter(active ? text : '')
  return <span className={className}>{active ? displayed : text}</span>
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

export function WebFetchItem({ fetch: f }: { fetch: WebFetchRef }) {
  const [faviconFailed, setFaviconFailed] = useState(false)
  const isFetching = f.status === 'fetching'
  const isHttp = isHttpUrl(f.url)
  const isFailed = f.status === 'failed'
  const domain = isHttp ? getDomain(f.url) : ''
  const scheme = getUrlScheme(f.url)
  const shortName = isHttp ? getShortName(f.url) : (scheme || 'invalid')
  const primaryText = f.title || (isHttp ? domain : (f.url || 'Invalid URL'))
  const secondaryText = typeof f.statusCode === 'number'
    ? `${f.statusCode}`
    : shortName
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
        {primaryText}
      </span>
      <span style={{ fontSize: '11px', color: 'var(--c-text-muted)', flexShrink: 0 }}>
        {secondaryText}
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

const TIMELINE_DOT_NUDGE_Y = 1
const DOT_TOP = 5 + TIMELINE_DOT_NUDGE_Y
const DOT_SIZE = 8
const SHELL_DOT_TOP = 9 + TIMELINE_DOT_NUDGE_Y
// CodeExecutionCard: border(0.5) + padding(6) + icon-center(14) = 20.5 → dot top ≈ 16
const PYTHON_DOT_TOP = 16 + TIMELINE_DOT_NUDGE_Y

export function SearchTimeline({ steps, sources, narratives, isComplete, codeExecutions, onOpenCodeExecution, activeCodeExecutionId, subAgents, fileOps, webFetches, headerOverride, shimmer, live, accessToken, baseUrl }: Props) {
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

  const visibleSteps = steps.filter((step) => step.kind !== 'finished')
  const textEntries = narratives ?? []
  const codeExecCount = codeExecutions?.length ?? 0
  const subAgentCount = subAgents?.length ?? 0
  const fileOpCount = fileOps?.length ?? 0
  const webFetchCount = webFetches?.length ?? 0
  const [hovered, setHovered] = useState(false)
  if (visibleSteps.length === 0 && textEntries.length === 0 && codeExecCount === 0 && subAgentCount === 0 && fileOpCount === 0 && webFetchCount === 0 && !headerOverride) return null

  type UEntry =
    | { kind: 'step'; id: string; seq: number; item: SearchStep }
    | { kind: 'text'; id: string; seq: number; item: SearchNarrative }
    | { kind: 'code'; id: string; seq: number; item: CodeExecution }
    | { kind: 'agent'; id: string; seq: number; item: SubAgentRef }
    | { kind: 'fileop'; id: string; seq: number; item: FileOpRef }
    | { kind: 'fetch'; id: string; seq: number; item: WebFetchRef }

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
  const totalUnifiableItems = visibleSteps.length + textEntries.length + codeExecCount + subAgentCount + fileOpCount + webFetchCount
  const useUnified = allUnified.length === totalUnifiableItems && totalUnifiableItems > 0
  if (useUnified) {
    const priority: Record<UEntry['kind'], number> = {
      step: 0,
      text: 1,
      code: 2,
      agent: 3,
      fileop: 4,
      fetch: 5,
    }
    allUnified.sort((a, b) => a.seq - b.seq || priority[a.kind] - priority[b.kind] || a.id.localeCompare(b.id))
  }

  const effectiveStepCount = visibleSteps.length || (codeExecCount + subAgentCount + fileOpCount + webFetchCount)

  const autoLabel = isComplete
    ? sources.length > 0
      ? `Reviewed ${sources.length} sources`
      : effectiveStepCount > 0
        ? `${effectiveStepCount} step${effectiveStepCount === 1 ? '' : 's'} completed`
        : 'Completed'
    : visibleSteps.length > 0
      ? (visibleSteps[visibleSteps.length - 1]?.label || 'Searching...')
      : 'Thinking'

  const headerLabel = headerOverride ?? autoLabel
  const dottedStepCount = visibleSteps.length

  return (
    <div style={{ maxWidth: '663px' }}>
      <button
        type="button"
        onClick={() => setCollapsed((p) => !p)}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '6px',
          padding: '6px 0',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          color: hovered
            ? 'var(--c-text-primary)'
            : isComplete && collapsed
              ? 'var(--c-text-tertiary)'
              : 'var(--c-text-secondary)',
          fontSize: '13px',
          fontWeight: 400,
          transition: 'color 0.15s ease',
        }}
      >
        <TypewriterText text={headerLabel} className={shimmer ? 'thinking-shimmer' : undefined} active={!!live} />
        {isComplete && sources.length > 0 && (
          <span style={{ fontSize: '12px', color: hovered ? 'var(--c-text-secondary)' : 'var(--c-text-muted)', fontWeight: 400, transition: 'color 0.15s ease' }}>
            {sources.length} sources
          </span>
        )}
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
            <div style={{ position: 'relative', paddingLeft: visibleSteps.length > 0 || textEntries.length > 0 || codeExecCount > 0 || subAgentCount > 0 || webFetchCount > 0 || fileOpCount > 0 ? '24px' : undefined, paddingTop: '2px', paddingBottom: '2px' }}>

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
                        active={!!live}
                      />
                    </div>

                    {step.kind === 'searching' && step.queries && step.queries.length > 0 && (
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', marginTop: '6px' }}>
                        {step.queries.map((q) => (
                          <QueryPill key={q} text={q} />
                        ))}
                      </div>
                    )}

                    {step.kind === 'reviewing' && sources.length > 0 && (
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
                        {sources.map((s, i) => (
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
                    const dotColor = entry.kind === 'step'
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
                    const dotBackground = entry.kind === 'text' ? 'var(--c-bg-page)' : dotColor
                    const dotBorder = entry.kind === 'text'
                      ? '1.5px solid var(--c-border-mid)'
                      : '2px solid var(--c-bg-page)'
                    return (
                      <div key={entry.id} style={{ position: 'relative', paddingBottom: isLast ? 0 : '6px' }}>
                        {!isLast && (
                          <div style={{ position: 'absolute', left: '-16px', top: `${dotTop + DOT_SIZE}px`, bottom: 0, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                        )}
                        {multiItems && !isFirst && (
                          <div style={{ position: 'absolute', left: '-16px', top: 0, height: `${dotTop}px`, width: '1.5px', background: 'var(--c-border-subtle)', zIndex: 0 }} />
                        )}
                        <div style={{ position: 'absolute', left: '-19px', top: `${dotTop}px`, width: `${DOT_SIZE}px`, height: `${DOT_SIZE}px`, borderRadius: '50%', background: dotBackground, border: dotBorder, zIndex: 1 }} />
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
                                active={!!live}
                              />
                            </div>

                            {entry.item.kind === 'searching' && entry.item.queries && entry.item.queries.length > 0 && (
                              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', marginTop: '6px' }}>
                                {entry.item.queries.map((q) => (
                                  <QueryPill key={q} text={q} />
                                ))}
                              </div>
                            )}

                            {entry.item.kind === 'reviewing' && sources.length > 0 && (
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
                                {sources.map((s, sourceIdx) => (
                                  <div key={`${s.url}-${sourceIdx}`}>
                                    <SourceItem source={s} />
                                  </div>
                                ))}
                              </div>
                            )}
                          </div>
                        )}
                        {entry.kind === 'text' && (
                          <div
                            style={{
                              fontSize: '14px',
                              lineHeight: '1.6',
                              color: 'var(--c-text-primary)',
                              whiteSpace: 'pre-wrap',
                              wordBreak: 'break-word',
                            }}
                          >
                            {entry.item.text}
                          </div>
                        )}
                        {entry.kind === 'code' && (entry.item.language === 'shell'
                          ? <ShellExecutionBlock code={entry.item.code} output={entry.item.output} status={entry.item.status} errorMessage={entry.item.errorMessage} />
                          : <CodeExecutionCard language={entry.item.language} code={entry.item.code} output={entry.item.output} errorMessage={entry.item.errorMessage} status={entry.item.status} onOpen={onOpenCodeExecution ? () => onOpenCodeExecution(entry.item as CodeExecution) : undefined} isActive={activeCodeExecutionId === entry.item.id} />
                        )}
                        {entry.kind === 'agent' && (
                          <SubAgentBlock nickname={entry.item.nickname} personaId={entry.item.personaId} input={entry.item.input} output={entry.item.output} status={entry.item.status} error={entry.item.error} live={live} currentRunId={entry.item.currentRunId} accessToken={accessToken} baseUrl={baseUrl} />
                        )}
                        {entry.kind === 'fileop' && (
                          <FileOpBlock toolName={entry.item.toolName} label={entry.item.label} output={entry.item.output} status={entry.item.status} errorMessage={entry.item.errorMessage} />
                        )}
                        {entry.kind === 'fetch' && <WebFetchItem fetch={entry.item} />}
                      </div>
                    )
                  })}
                </div>
              ) : (
                <>
                  {textEntries.length > 0 && (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', paddingTop: visibleSteps.length > 0 ? '8px' : '0' }}>
                      {textEntries.map((entry) => (
                        <div key={entry.id} style={{ fontSize: '14px', lineHeight: '1.6', color: 'var(--c-text-primary)', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
                          {entry.text}
                        </div>
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
                              ? <ShellExecutionBlock code={ce.code} output={ce.output} status={ce.status} errorMessage={ce.errorMessage} />
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
                            <SubAgentBlock nickname={agent.nickname} personaId={agent.personaId} input={agent.input} output={agent.output} status={agent.status} error={agent.error} live={live} currentRunId={agent.currentRunId} accessToken={accessToken} baseUrl={baseUrl} />
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
                            <FileOpBlock toolName={op.toolName} label={op.label} output={op.output} status={op.status} errorMessage={op.errorMessage} />
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
                            <WebFetchItem fetch={f} />
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
