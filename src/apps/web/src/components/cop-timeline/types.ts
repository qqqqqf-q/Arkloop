import type { WebSource } from '../../storage'
import type { SubAgentRef, FileOpRef, WebFetchRef } from '../../storage'
import type { ExploreGroupRef } from '../../toolPresentation'
import type { CodeExecution } from '../CodeExecutionCard'
import { codeExecutionAccentColor } from '../../codeExecutionStatus'

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

export type Props = {
  steps: WebSearchPhaseStep[]
  sources: WebSource[]
  narratives?: SearchNarrative[]
  isComplete: boolean
  codeExecutions?: CodeExecution[]
  onOpenCodeExecution?: (ce: CodeExecution) => void
  activeCodeExecutionId?: string
  subAgents?: SubAgentRef[]
  onOpenSubAgent?: (agent: SubAgentRef) => void
  fileOps?: FileOpRef[]
  exploreGroups?: ExploreGroupRef[]
  webFetches?: WebFetchRef[]
  genericTools?: Array<{ id: string; toolName: string; label: string; displayDescription?: string; output?: string; emptyLabel?: string; status: 'running' | 'success' | 'failed'; errorMessage?: string; seq?: number }>
  headerOverride?: string
  shimmer?: boolean
  live?: boolean
  preserveExpanded?: boolean
  accessToken?: string
  baseUrl?: string
  /** 与 tool 同序交错的多段 thinking（seq 与工具池子对齐排序） */
  thinkingRows?: Array<{ id: string; markdown: string; live?: boolean; seq: number; durationSec?: number; startedAtMs?: number }> | null
  /** COP 内可见短正文（与 thinking / 工具行同序） */
  copInlineTextRows?: Array<{ id: string; text: string; live?: boolean; seq: number }> | null
  /** 与 narrative / 工具行同一套 unified 点线，仅多一行 Markdown（无 thinkingRows 时的单块） */
  assistantThinking?: { markdown: string; live?: boolean } | null
  /** 仅 pendingThinking 壳子用：无 thinkingRows 时配合 assistantThinking 估时长 */
  thinkingStartedAt?: number
  /** 后一段为助手正文且已有字符时收起本段 COP（不依赖 isStreaming，避免 run 结束帧错过） */
  trailingAssistantTextPresent?: boolean
  /** thinking 流式阶段 COP header 使用的随机提示句（不含 ...） */
  thinkingHint?: string
  segmentLive?: boolean
  forceCollapsed?: boolean
  debugMeta?: Record<string, unknown>
}

export type DoneTimelineEntry = { kind: 'done'; id: string; seq: number; item: { label: string } }

export type UEntry =
  | { kind: 'thinking'; id: string; seq: number; item: { markdown: string; live: boolean; durationSec?: number; startedAtMs?: number } }
  | DoneTimelineEntry
  | { kind: 'copinline'; id: string; seq: number; item: { text: string; live: boolean } }
  | { kind: 'step'; id: string; seq: number; item: WebSearchPhaseStep }
  | { kind: 'text'; id: string; seq: number; item: SearchNarrative }
  | { kind: 'code'; id: string; seq: number; item: CodeExecution }
  | { kind: 'agent'; id: string; seq: number; item: SubAgentRef }
  | { kind: 'fileop'; id: string; seq: number; item: FileOpRef }
  | { kind: 'explore'; id: string; seq: number; item: ExploreGroupRef }
  | { kind: 'fetch'; id: string; seq: number; item: WebFetchRef }
  | { kind: 'generic'; id: string; seq: number; item: NonNullable<Props['genericTools']>[number] }

const DEFAULT_SEARCHING_LABEL = 'Searching'
const COMPLETED_SEARCHING_LABEL = 'Search completed'

export function timelineStepDisplayLabel(step: Pick<WebSearchPhaseStep, 'kind' | 'label' | 'status'>): string {
  if (step.kind === 'reviewing') return 'Reviewing sources'
  if (step.kind === 'searching' && step.status === 'done' && step.label.trim() === DEFAULT_SEARCHING_LABEL) {
    return COMPLETED_SEARCHING_LABEL
  }
  return step.label
}

const DOT_COLOR_MAP: Record<UEntry['kind'], (entry: UEntry) => string> = {
  thinking: (e) => {
    const item = (e as Extract<UEntry, { kind: 'thinking' }>).item
    return item.live ? 'var(--c-text-secondary)' : 'var(--c-border-mid)'
  },
  done: () => 'var(--c-text-muted)',
  copinline: () => 'var(--c-border-mid)',
  step: (e) => {
    const item = (e as Extract<UEntry, { kind: 'step' }>).item
    return item.status === 'active' ? 'var(--c-text-secondary)' : 'var(--c-text-muted)'
  },
  text: () => 'var(--c-border-mid)',
  code: (e) => {
    const item = (e as Extract<UEntry, { kind: 'code' }>).item
    return codeExecutionAccentColor(item.status)
  },
  agent: (e) => {
    const item = (e as Extract<UEntry, { kind: 'agent' }>).item
    return item.status === 'completed' ? 'var(--c-text-muted)'
      : item.status === 'failed' ? 'var(--c-status-error-text, #ef4444)'
      : 'var(--c-text-secondary)'
  },
  fileop: (e) => {
    const item = (e as Extract<UEntry, { kind: 'fileop' }>).item
    return item.status === 'failed' ? 'var(--c-status-error-text, #ef4444)'
      : item.status === 'running' ? 'var(--c-text-secondary)'
      : 'var(--c-text-muted)'
  },
  explore: (e) => {
    const item = (e as Extract<UEntry, { kind: 'explore' }>).item
    return item.status === 'failed' ? 'var(--c-status-error-text, #ef4444)'
      : item.status === 'running' ? 'var(--c-text-secondary)'
      : 'var(--c-text-muted)'
  },
  fetch: (e) => {
    const item = (e as Extract<UEntry, { kind: 'fetch' }>).item
    return item.status === 'failed' ? 'var(--c-status-error-text, #ef4444)'
      : item.status === 'fetching' ? 'var(--c-text-secondary)'
      : 'var(--c-text-muted)'
  },
  generic: (e) => {
    const item = (e as Extract<UEntry, { kind: 'generic' }>).item
    return item.status === 'failed' ? 'var(--c-status-error-text, #ef4444)'
      : item.status === 'running' ? 'var(--c-text-secondary)'
      : 'var(--c-text-muted)'
  },
}

export function dotColor(entry: UEntry): string {
  return DOT_COLOR_MAP[entry.kind](entry)
}

export function autoLabel(opts: {
  anyThinkingLive: boolean
  hasAnyThinking: boolean
  live: boolean
  thinkingLiveHeaderLabel: string
  isComplete: boolean
  hasThinkingOnly: boolean
  sourceCount: number
  effectiveStepCount: number
  thoughtDurationLabel: string
  showPendingThinkingHeader: boolean
  thinkingHint?: string
  visibleSteps: WebSearchPhaseStep[]
  t: { copTimelineLiveProgress: string }
}): string {
  const {
    anyThinkingLive, hasAnyThinking, live, thinkingLiveHeaderLabel,
    isComplete, hasThinkingOnly, sourceCount, effectiveStepCount,
    thoughtDurationLabel, showPendingThinkingHeader, thinkingHint,
    visibleSteps, t,
  } = opts

  switch (true) {
    case anyThinkingLive || (hasAnyThinking && live):
      return thinkingLiveHeaderLabel

    case hasAnyThinking && isComplete && !hasThinkingOnly:
      return sourceCount > 0
        ? `Reviewed ${sourceCount} sources`
        : effectiveStepCount > 0
          ? `${effectiveStepCount} step${effectiveStepCount === 1 ? '' : 's'} completed`
          : thoughtDurationLabel

    case hasAnyThinking:
      return thoughtDurationLabel

    case showPendingThinkingHeader:
      return `${thinkingHint}...`

    case isComplete:
      return sourceCount > 0
        ? `Reviewed ${sourceCount} sources`
        : effectiveStepCount > 0
          ? `${effectiveStepCount} step${effectiveStepCount === 1 ? '' : 's'} completed`
          : 'Completed'

    case visibleSteps.length > 0:
      return visibleSteps[visibleSteps.length - 1]
        ? timelineStepDisplayLabel(visibleSteps[visibleSteps.length - 1]!)
        : 'Searching...'

    case effectiveStepCount > 0:
      return t.copTimelineLiveProgress

    default:
      return thinkingHint ? `${thinkingHint}...` : 'Searching...'
  }
}

export const ENTRY_SORT_PRIORITY: Record<UEntry['kind'], number> = {
  thinking: -1,
  done: 0,
  copinline: 0,
  step: 1,
  text: 2,
  code: 3,
  agent: 4,
  fileop: 5,
  explore: 5,
  fetch: 6,
  generic: 7,
}
