import { motion } from 'framer-motion'
import { Search } from 'lucide-react'
import { useTypewriter } from '../../hooks/useTypewriter'

/** CopTimeline 左轴点线几何；ChatPage 顶层条与之对齐 */
export const COP_TIMELINE_DOT_NUDGE_Y = 1
export const COP_TIMELINE_DOT_TOP = 5 + COP_TIMELINE_DOT_NUDGE_Y
export const COP_TIMELINE_DOT_SIZE = 8
export const COP_TIMELINE_SHELL_DOT_TOP = 5 + COP_TIMELINE_DOT_NUDGE_Y
export const COP_TIMELINE_PYTHON_DOT_TOP = 16 + COP_TIMELINE_DOT_NUDGE_Y
export const COP_TIMELINE_LINE_LEFT_PX = -16
export const COP_TIMELINE_DOT_LEFT_PX = -19
export const COP_TIMELINE_CONTENT_PADDING_LEFT_PX = 24
/** 仅 thinking 完结直出时正文行高，与 DOT 几何中心对齐：DOT_TOP + DOT_SIZE/2 - lh/2 */
export const COP_TIMELINE_THINKING_PLAIN_LINE_HEIGHT_PX = 18

export const DEVELOPER_SHOW_DEBUG_PANEL_KEY = 'arkloop:web:developer_show_debug_panel'

export function TypewriterText({ text, className, live }: { text: string; className?: string; live?: boolean }) {
  const displayed = useTypewriter(text, !live)
  return <span className={className}>{live ? displayed : text}</span>
}

export function QueryPill({ text, live }: { text: string; live?: boolean }) {
  return (
    <motion.span
      initial={live ? { opacity: 0, x: -6, scale: 0.98 } : false}
      animate={{ opacity: 1, x: 0, scale: 1 }}
      transition={{ duration: 0.22, ease: 'easeOut' }}
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
    </motion.span>
  )
}

export function getDomain(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

export function getDomainShort(url: string): string {
  try {
    const hostname = new URL(url).hostname.replace(/^www\./, '')
    const parts = hostname.split('.')
    return parts.length >= 2 ? parts[parts.length - 2] : hostname
  } catch {
    return url
  }
}

export function isHttpUrl(url: string): boolean {
  try {
    const p = new URL(url)
    return p.protocol === 'http:' || p.protocol === 'https:'
  } catch {
    return false
  }
}

export function getShortName(url: string): string {
  try {
    const hostname = new URL(url).hostname.replace(/^www\./, '')
    const parts = hostname.split('.')
    return parts.length >= 2 ? parts[parts.length - 2] : hostname
  } catch {
    return url
  }
}

export function getUrlScheme(url: string): string {
  try {
    return new URL(url).protocol.replace(/:$/, '')
  } catch {
    const match = /^([a-z][a-z0-9+.-]*):/i.exec(url.trim())
    return match?.[1]?.toLowerCase() ?? ''
  }
}

export function initialThinkingElapsedSec(
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

export function firstThinkingStartMs(
  thinkingRows: Array<{ startedAtMs?: number }>,
  fallback?: number,
): number | undefined {
  const first = thinkingRows.find((row) => typeof row.startedAtMs === 'number')?.startedAtMs
  return first ?? fallback
}

export const REVIEWING_SOURCE_PREVIEW_COUNT = 12
export const FAVICON_REVEAL_DELAY_MS = 140
