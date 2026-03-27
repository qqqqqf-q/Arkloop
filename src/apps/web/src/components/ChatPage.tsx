import React, { useState, useEffect, useLayoutEffect, useRef, useCallback, useMemo, Fragment, type ComponentProps } from 'react'
import { useParams, useLocation, useOutletContext, useNavigate } from 'react-router-dom'
import { createPortal } from 'react-dom'
import { motion } from 'framer-motion'
import { ArrowDown, Check, ChevronDown, Glasses, Loader2, Pencil, Share2, Star, Trash2, X, AlertCircle } from 'lucide-react'
import { isDesktop } from '@arkloop/shared/desktop'
import { codeExecutionAccentColor } from '../codeExecutionStatus'
import { ChatInput, type Attachment } from './ChatInput'
import { MessageBubble } from './MessageBubble'
import { RunDetailPanel } from './RunDetailPanel'
import type { CodeExecution } from './CodeExecutionCard'
import { CodeExecutionCard } from './CodeExecutionCard'
import { ExecutionCard } from './ExecutionCard'
import { SubAgentBlock } from './SubAgentBlock'
import {
  CopTimeline,
  CopTimelineUnifiedRow,
  WebFetchItem,
  type WebSearchPhaseStep,
  COP_TIMELINE_CONTENT_PADDING_LEFT_PX,
  COP_TIMELINE_DOT_TOP,
  COP_TIMELINE_PYTHON_DOT_TOP,
} from './CopTimeline'
import { MarkdownRenderer } from './MarkdownRenderer'
import { useTypewriter } from '../hooks/useTypewriter'
import { ArtifactStreamBlock, extractPartialArtifactFields, type StreamingArtifactEntry } from './ArtifactStreamBlock'
import { WidgetBlock } from './WidgetBlock'
import UserInputCard from './UserInputCard'
import { resolveMessageSourcesForRender } from './chatSourceResolver'
import { ErrorCallout, type AppError } from './ErrorCallout'
import { ShareModal } from './ShareModal'
import { NotificationBell } from './NotificationBell'
import { ModeSwitch } from './ModeSwitch'
import { SourcesPanel } from './SourcesPanel'
import { CodeExecutionPanel } from './CodeExecutionPanel'
import { DocumentPanel } from './DocumentPanel'
import { ClawRightPanel } from './ClawRightPanel'
import { useSSE } from '../hooks/useSSE'
import { SSEApiError } from '../sse'
import { getInjectionBlockMessage, shouldSuppressLiveRunEventAfterInjectionBlock } from '../liveRunSecurity'
import {
  applyCodeExecutionToolCall,
  applyCodeExecutionToolResult,
  applyTerminalDelta,
  buildMessageCodeExecutionsFromRunEvents,
  patchCodeExecutionList,
  buildMessageThinkingFromRunEvents,
  buildMessageWidgetsFromRunEvents,
  findAssistantMessageForRun,
  selectFreshRunEvents,
  shouldRefetchCompletedRunMessages,
  shouldReplayMessageCodeExecutions,
  applyBrowserToolCall,
  applyBrowserToolResult,
  buildMessageBrowserActionsFromRunEvents,
  applySubAgentToolCall,
  applySubAgentToolResult,
  buildMessageSubAgentsFromRunEvents,
  applyFileOpToolCall,
  applyFileOpToolResult,
  buildMessageFileOpsFromRunEvents,
  applyWebFetchToolCall,
  applyWebFetchToolResult,
  buildMessageWebFetchesFromRunEvents,
} from '../runEventProcessing'
import {
  assistantTurnPlainText,
  assistantTurnThinkingPlainText,
  buildAssistantTurnFromRunEvents,
  copSegmentCalls,
  createEmptyAssistantTurnFoldState,
  drainAssistantTurnForPersist,
  foldAssistantTurnEvent,
  requestAssistantTurnThinkingBreak,
  snapshotAssistantTurn,
  type AssistantTurnSegment,
  type AssistantTurnUi,
} from '../assistantTurnSegments'
import { copTimelinePayloadForSegment, toolCallIdsInCopTimelines } from '../copSegmentTimeline'
import { applyRunEventToWebSearchSteps, isWebSearchToolName } from '../webSearchTimelineFromRunEvent'
import { useLocale } from '../contexts/LocaleContext'
import { apiBaseUrl } from '@arkloop/shared/api'
import { isACPDelegateEventData } from '@arkloop/shared'
import type { UserInputRequest, UserInputResponse, RequestedSchema } from '../userInputTypes'
import {
  createMessage,
  createRun,
  cancelRun,
  provideInput,
  retryThread,
  editMessage,
  forkThread,
  getThread,
  listMessages,
  listRunEvents,
  listThreadRuns,
  createThreadShare,
  uploadThreadAttachment,
  starThread,
  unstarThread,
  updateThreadTitle,
  deleteThread,
  listStarredThreadIds,
  isApiError,
  type MessageContent,
  type MessageResponse,
  type ThreadResponse,
} from '../api'
import { buildMessageRequest } from '../messageContent'
import {
  addSearchThreadId,
  SEARCH_PERSONA_KEY,
  isSearchThreadId,
  readSelectedPersonaKeyFromStorage,
  readSelectedModelFromStorage,
  readMessageSources,
  writeMessageSources,
  readMessageArtifacts,
  writeMessageArtifacts,
  readMessageCodeExecutions,
  writeMessageCodeExecutions,
  readMessageThinking,
  writeMessageThinking,
  readMessageBrowserActions,
  writeMessageBrowserActions,

  readMessageSearchSteps,
  writeMessageSearchSteps,
  readMessageAssistantTurn,
  writeMessageAssistantTurn,
  type WebSource,
  type ArtifactRef,
  type CodeExecutionRef,
  type BrowserActionRef,
  type SubAgentRef,
  type FileOpRef,
  type MessageThinkingRef,
  type MessageSearchStepRef,
  readMessageSubAgents,
  writeMessageSubAgents,
  readMessageFileOps,
  writeMessageFileOps,
  readMessageWebFetches,
  writeMessageWebFetches,
  type WebFetchRef,
  readMessageWidgets,
  writeMessageWidgets,
  type WidgetRef,
  migrateMessageMetadata,
  readDeveloperShowRunEvents,
  readMsgRunEvents,
  writeMsgRunEvents,
  type MsgRunEvent,
  readThreadClawFolder,
} from '../storage'

const sidePanelWidth = 360
const documentPanelWidth = 560

const TERMINAL_RUN_EVENT_TYPES = new Set([
  'run.completed',
  'run.cancelled',
  'run.failed',
  'run.interrupted',
])

function isTerminalRunEventType(type: string): boolean {
  return TERMINAL_RUN_EVENT_TYPES.has(type)
}

function normalizeError(error: unknown): AppError {
  if (isApiError(error)) {
    return { message: error.message, traceId: error.traceId, code: error.code }
  }
  if (error instanceof SSEApiError) {
    return { message: error.message, traceId: error.traceId, code: error.code }
  }
  if (error instanceof Error) {
    return { message: error.message }
  }
  return { message: '请求失败' }
}

type OutletContext = {
  accessToken: string
  onLoggedOut: () => void
  onRunStarted: (threadId: string) => void
  onRunEnded: (threadId: string) => void
  onThreadCreated: (thread: ThreadResponse) => void
  onThreadTitleUpdated: (threadId: string, title: string) => void
  refreshCredits: () => void
  onOpenNotifications: () => void
  notificationVersion: number
  creditsBalance: number
  isPrivateMode: boolean
  onTogglePrivateMode: () => void
  privateThreadIds: Set<string>
  onSetPendingIncognito: (v: boolean) => void
  onRightPanelChange?: (open: boolean) => void
  threads: ThreadResponse[]
  onThreadDeleted: (threadId: string) => void
  appMode: import('../storage').AppMode
  availableAppModes: import('../storage').AppMode[]
  onSetAppMode: (mode: import('../storage').AppMode) => void
  onOpenSettings?: (tab: string) => void
  /** Desktop 标题栏眼镜与 Chat 共用同一套点击逻辑 */
  setTitleBarIncognitoClick?: (fn: (() => void) | null) => void
}

type LocationState = { initialRunId?: string; isSearch?: boolean; isIncognitoFork?: boolean; forkBaseCount?: number; userEnterMessageId?: string } | null

type DocumentPanelState = {
  artifact: ArtifactRef
  artifacts: ArtifactRef[]
  runId?: string
}

// finalizeSearchSteps converts live WebSearchPhaseStep[] to the storage format.
// Identical to finalizeBlockSteps but kept as a standalone function for the
// legacy (non-COP) search path.
function finalizeSearchSteps(steps: WebSearchPhaseStep[]): MessageSearchStepRef[] {
  return finalizeBlockSteps(steps)
}

// patchLegacySearchSteps normalises search step refs loaded from localStorage.
// readMessageSearchSteps already validates structure, so no structural changes
// are needed today — we just return a no-op result.
function patchLegacySearchSteps(steps: MessageSearchStepRef[]): { steps: MessageSearchStepRef[]; changed: boolean } {
  return { steps, changed: false }
}

function finalizeBlockSteps(steps: WebSearchPhaseStep[]): MessageSearchStepRef[] {
  if (steps.length === 0) return []
  return steps.map((step) => ({
    id: step.id,
    kind: step.kind,
    label: step.label,
    status: 'done',
    queries: step.queries ? [...step.queries] : undefined,
    seq: step.seq,
  }))
}

function collectCompletedWidgets(entries: StreamingArtifactEntry[]): WidgetRef[] {
  return entries
    .filter((entry) => entry.toolName === 'show_widget' && entry.complete && entry.content && entry.toolCallId)
    .map((entry) => ({
      id: entry.toolCallId!,
      title: entry.title ?? 'Widget',
      html: entry.content!,
    }))
}

type CopSegment = Extract<AssistantTurnSegment, { type: 'cop' }>

function widgetToolCallIdsPlacedInTurn(turn: AssistantTurnUi, widgets: WidgetRef[] | undefined | null): Set<string> {
  const placed = new Set<string>()
  const want = new Set((widgets ?? []).map((w) => w.id))
  if (want.size === 0) return placed
  for (const s of turn.segments) {
    if (s.type !== 'cop') continue
    for (const c of copSegmentCalls(s)) {
      if (c.toolName === 'show_widget' && want.has(c.toolCallId)) placed.add(c.toolCallId)
    }
  }
  return placed
}

function historicWidgetsForCop(seg: CopSegment, widgets: WidgetRef[] | undefined | null): WidgetRef[] {
  if (!widgets?.length) return []
  const ids = new Set(copSegmentCalls(seg).filter((c) => c.toolName === 'show_widget').map((c) => c.toolCallId))
  if (ids.size === 0) return []
  return widgets.filter((w) => ids.has(w.id))
}

function liveStreamingWidgetEntriesForCop(seg: CopSegment, entries: StreamingArtifactEntry[]): StreamingArtifactEntry[] {
  const out: StreamingArtifactEntry[] = []
  for (const c of copSegmentCalls(seg)) {
    if (c.toolName !== 'show_widget') continue
    const e = entries.find((x) => x.toolName === 'show_widget' && x.toolCallId === c.toolCallId)
    if (!e) continue
    if ((e.content != null && e.content.length > 0) || (e.loadingMessages != null && e.loadingMessages.length > 0)) {
      out.push(e)
    }
  }
  return out
}

function liveInlineArtifactEntriesForCop(seg: CopSegment, entries: StreamingArtifactEntry[]): StreamingArtifactEntry[] {
  const out: StreamingArtifactEntry[] = []
  for (const c of copSegmentCalls(seg)) {
    if (c.toolName !== 'create_artifact') continue
    const e = entries.find((x) => x.toolName === 'create_artifact' && x.toolCallId === c.toolCallId)
    if (e && e.content && e.display !== 'panel') out.push(e)
  }
  return out
}

function liveCopShowWidgetCallIds(turn: AssistantTurnUi | null): Set<string> {
  const ids = new Set<string>()
  if (!turn) return ids
  for (const s of turn.segments) {
    if (s.type !== 'cop') continue
    for (const c of copSegmentCalls(s)) {
      if (c.toolName === 'show_widget' && c.toolCallId) ids.add(c.toolCallId)
    }
  }
  return ids
}

function liveCopCreateArtifactCallIds(turn: AssistantTurnUi | null): Set<string> {
  const ids = new Set<string>()
  if (!turn) return ids
  for (const s of turn.segments) {
    if (s.type !== 'cop') continue
    for (const c of copSegmentCalls(s)) {
      if (c.toolName === 'create_artifact' && c.toolCallId) ids.add(c.toolCallId)
    }
  }
  return ids
}

function LiveTurnMarkdown({
  content,
  typewriterDone,
  ...rest
}: {
  content: string
  typewriterDone: boolean
} & Omit<ComponentProps<typeof MarkdownRenderer>, 'content'>) {
  const displayed = useTypewriter(content, typewriterDone)
  return <MarkdownRenderer content={displayed} {...rest} />
}

function liveTurnHasThinkingSegment(turn: AssistantTurnUi | null): boolean {
  if (!turn) return false
  return turn.segments.some(
    (s) => s.type === 'cop' && s.items.some((i) => i.kind === 'thinking'),
  )
}

function thinkingBlockDurationSec(
  it: CopSegment['items'][number],
): number {
  if (it.kind !== 'thinking') return 0
  if (it.startedAtMs == null || it.endedAtMs == null) return 0
  return Math.max(0, Math.round((it.endedAtMs - it.startedAtMs) / 1000))
}

function thinkingRowsForCop(
  seg: CopSegment,
  opts: { live: boolean; segmentIndex: number; lastSegmentIndex: number },
): Array<{ id: string; markdown: string; live?: boolean; seq: number; durationSec: number }> {
  let lastThinkIdx = -1
  for (let i = seg.items.length - 1; i >= 0; i--) {
    if (seg.items[i]?.kind === 'thinking') {
      lastThinkIdx = i
      break
    }
  }
  const tailKind = seg.items[seg.items.length - 1]?.kind
  const out: Array<{ id: string; markdown: string; live?: boolean; seq: number; durationSec: number }> = []
  seg.items.forEach((it, itemIdx) => {
    if (it.kind !== 'thinking') return
    const rowLive =
      opts.live &&
      opts.segmentIndex === opts.lastSegmentIndex &&
      itemIdx === lastThinkIdx &&
      tailKind === 'thinking'
    out.push({
      id: `think-${opts.segmentIndex}-${itemIdx}-${it.seq}`,
      markdown: it.content,
      seq: it.seq,
      live: rowLive,
      durationSec: thinkingBlockDurationSec(it),
    })
  })
  return out
}

function copInlineTextRowsForCop(
  seg: CopSegment,
  opts: { live: boolean; segmentIndex: number; lastSegmentIndex: number },
): Array<{ id: string; text: string; live?: boolean; seq: number }> {
  let lastInlineIdx = -1
  for (let i = seg.items.length - 1; i >= 0; i--) {
    if (seg.items[i]?.kind === 'assistant_text') {
      lastInlineIdx = i
      break
    }
  }
  const out: Array<{ id: string; text: string; live?: boolean; seq: number }> = []
  seg.items.forEach((it, itemIdx) => {
    if (it.kind !== 'assistant_text') return
    const rowLive =
      opts.live && opts.segmentIndex === opts.lastSegmentIndex && itemIdx === lastInlineIdx
    out.push({
      id: `inline-${opts.segmentIndex}-${itemIdx}-${it.seq}`,
      text: it.content,
      seq: it.seq,
      live: rowLive,
    })
  })
  return out
}

function turnHasCopThinkingItems(turn: AssistantTurnUi): boolean {
  return turn.segments.some(
    (s) => s.type === 'cop' && s.items.some((i) => i.kind === 'thinking'),
  )
}

export function ChatPage() {
  const { accessToken, onLoggedOut, onRunStarted, onRunEnded, onThreadCreated, onThreadTitleUpdated, refreshCredits, onOpenNotifications, notificationVersion, creditsBalance: _creditsBalance, onTogglePrivateMode, privateThreadIds, onSetPendingIncognito, setTitleBarIncognitoClick, onRightPanelChange, threads, onThreadDeleted, appMode, availableAppModes, onSetAppMode, onOpenSettings } = useOutletContext<OutletContext>()
  const threadsRef = useRef(threads)
  useEffect(() => { threadsRef.current = threads }, [threads])
  const { threadId } = useParams<{ threadId: string }>()
  const location = useLocation()
  const locationState = location.state as LocationState
  const navigate = useNavigate()
  const { t } = useLocale()

  const baseUrl = apiBaseUrl()

  const [isSearchThread, setIsSearchThread] = useState(
    () => locationState?.isSearch === true || isSearchThreadId(threadId ?? ''),
  )

  const [messages, setMessages] = useState<MessageResponse[]>([])
  const [messagesLoading, setMessagesLoading] = useState(false)
  const [userEnterMessageId, setUserEnterMessageId] = useState<string | null>(null)
  const [draft, setDraft] = useState('')
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const assistantTurnFoldStateRef = useRef(createEmptyAssistantTurnFoldState())
  const [liveAssistantTurn, setLiveAssistantTurn] = useState<AssistantTurnUi | null>(null)
  const seenFirstToolCallInRunRef = useRef(false)
  const [activeRunId, setActiveRunId] = useState<string | null>(
    locationState?.initialRunId ?? null,
  )
  const [sending, setSending] = useState(false)
  const [cancelSubmitting, setCancelSubmitting] = useState(false)
  const [error, setError] = useState<AppError | null>(null)
  const [injectionBlocked, setInjectionBlocked] = useState<string | null>(null)
  const injectionBlockedRunIdRef = useRef<string | null>(null)
  const [queuedDraft, setQueuedDraft] = useState<string | null>(null)
  const [awaitingInput, setAwaitingInput] = useState(false)
  const [checkInDraft, setCheckInDraft] = useState('')
  const [checkInSubmitting, setCheckInSubmitting] = useState(false)
  const [pendingUserInput, setPendingUserInput] = useState<UserInputRequest | null>(null)
  const [shareModalOpen, setShareModalOpen] = useState(false)
  const [sharingMessageId, setSharingMessageId] = useState<string | null>(null)
  const [sharedMessageId, setSharedMessageId] = useState<string | null>(null)
  const [pendingIncognito, setPendingIncognito] = useState(false)
  const [contextCompactBar, setContextCompactBar] = useState<{ type: 'persist'; status: 'running' | 'done' | 'llm_failed' } | { type: 'trim'; status: 'done'; dropped: number } | null>(null)
  const contextCompactHideTimerRef = useRef<number | null>(null)

  const clearContextCompactHideTimer = useCallback(() => {
    if (contextCompactHideTimerRef.current != null) {
      clearTimeout(contextCompactHideTimerRef.current)
      contextCompactHideTimerRef.current = null
    }
  }, [])

  // web 引用来源：messageId -> WebSource[]
  const [messageSourcesMap, setMessageSourcesMap] = useState<Map<string, WebSource[]>>(new Map())
  // 当前 run 累积的搜索结果（按工具调用顺序拼接，1-indexed）
  const currentRunSourcesRef = useRef<WebSource[]>([])
  // artifact 产物：messageId -> ArtifactRef[]
  const [messageArtifactsMap, setMessageArtifactsMap] = useState<Map<string, ArtifactRef[]>>(new Map())
  const currentRunArtifactsRef = useRef<ArtifactRef[]>([])
  // 代码执行记录：messageId -> CodeExecutionRef[]
  const [messageCodeExecutionsMap, setMessageCodeExecutionsMap] = useState<Map<string, CodeExecutionRef[]>>(new Map())
  const currentRunCodeExecutionsRef = useRef<CodeExecutionRef[]>([])
  // 浏览器操作记录：messageId -> BrowserActionRef[]
  const [messageBrowserActionsMap, setMessageBrowserActionsMap] = useState<Map<string, BrowserActionRef[]>>(new Map())
  const currentRunBrowserActionsRef = useRef<BrowserActionRef[]>([])
  // sub-agent 记录
  const [messageSubAgentsMap, setMessageSubAgentsMap] = useState<Map<string, SubAgentRef[]>>(new Map())
  const currentRunSubAgentsRef = useRef<SubAgentRef[]>([])
  const [topLevelSubAgents, setTopLevelSubAgents] = useState<SubAgentRef[]>([])
  // 文件操作记录
  const [messageFileOpsMap, setMessageFileOpsMap] = useState<Map<string, FileOpRef[]>>(new Map())
  const currentRunFileOpsRef = useRef<FileOpRef[]>([])
  const [topLevelFileOps, setTopLevelFileOps] = useState<FileOpRef[]>([])
  // web fetch 记录
  const [messageWebFetchesMap, setMessageWebFetchesMap] = useState<Map<string, WebFetchRef[]>>(new Map())
  const currentRunWebFetchesRef = useRef<WebFetchRef[]>([])
  const [topLevelWebFetches, setTopLevelWebFetches] = useState<WebFetchRef[]>([])
  // streaming artifact 状态
  const streamingArtifactsRef = useRef<StreamingArtifactEntry[]>([])
  const [streamingArtifacts, setStreamingArtifacts] = useState<StreamingArtifactEntry[]>([])
  const [messageThinkingMap, setMessageThinkingMap] = useState<Map<string, MessageThinkingRef>>(new Map())
  // Search 时间轴缓存：messageId -> steps
  const [messageSearchStepsMap, setMessageSearchStepsMap] = useState<Map<string, MessageSearchStepRef[]>>(new Map())
  // Live search steps for the legacy (non-COP) search path
  const [searchSteps, setSearchSteps] = useState<WebSearchPhaseStep[]>([])
  const searchStepsRef = useRef<WebSearchPhaseStep[]>([])
  const [messageAssistantTurnMap, setMessageAssistantTurnMap] = useState<Map<string, AssistantTurnUi>>(new Map())
  // show_widget 缓存：messageId -> WidgetRef[]
  const [messageWidgetsMap, setMessageWidgetsMap] = useState<Map<string, WidgetRef[]>>(new Map())
  // 跟踪未响应的用户消息，用于取消后重发时替换
  const noResponseMsgIdRef = useRef<string | null>(null)
  const replaceOnCancelRef = useRef<string | null>(null)

  // sources 侧边面板：显示哪条消息的来源
  const [sourcePanelMessageId, setSourcePanelMessageId] = useState<string | null>(null)
  // 代码执行侧边面板
  const [codePanelExecution, setCodePanelExecution] = useState<CodeExecution | null>(null)
  const lastCodePanelRef = useRef<CodeExecution | null>(null)
  // 文档预览侧边面板
  const [documentPanelArtifact, setDocumentPanelArtifact] = useState<DocumentPanelState | null>(null)
  const lastDocumentPanelRef = useRef<DocumentPanelState | null>(null)
  // 关闭动画期间保留上一次的数据
  const lastPanelSourcesRef = useRef<WebSource[] | undefined>(undefined)
  const lastPanelQueryRef = useRef<string | undefined>(undefined)
  // segment 状态：用于渲染 Agent 规划轮折叠块
  type Segment = { segmentId: string; kind: string; mode: string; label: string; content: string; isStreaming: boolean; codeExecutions: CodeExecution[] }
  const [segments, setSegments] = useState<Segment[]>([])
  const activeSegmentIdRef = useRef<string | null>(null)
  const segmentsRef = useRef<Segment[]>([])
  // Enter 后、首包 thinking 前进来的占位（与 Cop 点线对齐）
  const [pendingThinking, setPendingThinking] = useState(false)
  /** 本轮首条 thinking 到达时刻，供 COP 标题「思考 N 秒」 */
  const [copThinkingStartedAtMs, setCopThinkingStartedAtMs] = useState<number | undefined>(undefined)
  // segment 外的顶层代码执行（Ultra/Pro 模式，无 segment 包裹）
  const [topLevelCodeExecutions, setTopLevelCodeExecutions] = useState<CodeExecution[]>([])

  // --- Claw todo 进度 ---
  const [clawTodos, setClawTodos] = useState<Array<{ id: string; content: string; status: string }>>([])

  // --- 开发者调试 ---
  const [showRunEvents, setShowRunEvents] = useState(() => readDeveloperShowRunEvents())
  const [runDetailPanelRunId, setRunDetailPanelRunId] = useState<string | null>(null)
  const [_msgRunEventsMap, setMsgRunEventsMap] = useState<Map<string, MsgRunEvent[]>>(new Map())

  useEffect(() => {
    const handleChange = (e: Event) => {
      setShowRunEvents((e as CustomEvent<boolean>).detail)
    }
    window.addEventListener('arkloop:developer_show_run_events', handleChange)
    return () => window.removeEventListener('arkloop:developer_show_run_events', handleChange)
  }, [])

  // --- 标题下拉菜单 ---
  const [titleMenuOpen, setTitleMenuOpen] = useState(false)
  const [titleMenuPos, setTitleMenuPos] = useState({ x: 0, y: 0 })
  const [starredIds, setStarredIds] = useState<string[]>([])
  const [editingTitle, setEditingTitle] = useState<string | null>(null)
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const titleMenuRef = useRef<HTMLDivElement>(null)
  const titleContainerRef = useRef<HTMLDivElement>(null)
  const titleChevronRef = useRef<HTMLButtonElement>(null)
  const editTitleInputRef = useRef<HTMLInputElement>(null)
  const renameCancelledRef = useRef(false)

  const currentThread = threadId ? threads.find(th => th.id === threadId) : undefined
  const currentTitle = currentThread ? ((currentThread.title ?? '').trim() || t.untitled) : null

  useEffect(() => {
    listStarredThreadIds(accessToken)
      .then((ids) => setStarredIds(ids))
      .catch(() => {})
  }, [accessToken])

  useEffect(() => {
    setTitleMenuOpen(false)
    setEditingTitle(null)
  }, [threadId])

  useEffect(() => {
    if (!titleMenuOpen) return
    const handler = (e: MouseEvent) => {
      if (titleMenuRef.current && !titleMenuRef.current.contains(e.target as Node) &&
          titleContainerRef.current && !titleContainerRef.current.contains(e.target as Node)) {
        setTitleMenuOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [titleMenuOpen])

  useEffect(() => {
    if (editingTitle !== null && editTitleInputRef.current) {
      editTitleInputRef.current.focus()
      editTitleInputRef.current.select()
    }
  }, [editingTitle])

  const openTitleMenu = useCallback(() => {
    if (titleChevronRef.current) {
      const rect = titleChevronRef.current.getBoundingClientRect()
      setTitleMenuPos({ x: rect.right, y: rect.bottom + 4 })
    }
    setTitleMenuOpen(prev => !prev)
  }, [])

  const toggleStar = useCallback(() => {
    if (!threadId) return
    const wasStarred = starredIds.includes(threadId)
    setStarredIds(prev =>
      wasStarred ? prev.filter(x => x !== threadId) : [threadId, ...prev]
    )
    setTitleMenuOpen(false)
    const req = wasStarred ? unstarThread(accessToken, threadId) : starThread(accessToken, threadId)
    req.catch(() => {
      setStarredIds(prev =>
        wasStarred ? [threadId, ...prev] : prev.filter(x => x !== threadId)
      )
    })
  }, [accessToken, threadId, starredIds])

  const startRename = useCallback(() => {
    if (!currentThread) return
    setTitleMenuOpen(false)
    const title = (currentThread.title ?? '').trim()
    setEditingTitle(title || '')
  }, [currentThread])

  const commitRename = useCallback(async (newTitle: string) => {
    if (!threadId) return
    setEditingTitle(null)
    const trimmed = newTitle.trim()
    if (!trimmed) return
    try {
      await updateThreadTitle(accessToken, threadId, trimmed)
      onThreadTitleUpdated(threadId, trimmed)
    } catch {
      // 忽略重命名失败，输入框已回收
    }
  }, [accessToken, threadId, onThreadTitleUpdated])

  const confirmDelete = useCallback(() => {
    setTitleMenuOpen(false)
    setDeleteConfirmOpen(true)
  }, [])

  const handleDeleteThread = useCallback(async () => {
    if (!threadId) return
    setDeleteConfirmOpen(false)
    try {
      await deleteThread(accessToken, threadId)
      onThreadDeleted(threadId)
    } catch {
      // 忽略删除失败，保留当前页
    }
  }, [accessToken, threadId, onThreadDeleted])

  const handleShareFromMenu = useCallback(() => {
    setTitleMenuOpen(false)
    setShareModalOpen(true)
  }, [])

  const resetAssistantTurnLive = useCallback(() => {
    assistantTurnFoldStateRef.current = createEmptyAssistantTurnFoldState()
    setLiveAssistantTurn(null)
  }, [])
  const bumpAssistantTurnSnapshot = useCallback(() => {
    setLiveAssistantTurn(snapshotAssistantTurn(assistantTurnFoldStateRef.current))
  }, [])
  const resetSearchSteps = useCallback(() => {
    searchStepsRef.current = []
    setSearchSteps([])
  }, [])
  // applySearchSteps queues finalized steps for storage once the message ID is
  // known (handled in the run.completed refreshMessages callback).
  const pendingSearchStepsRef = useRef<MessageSearchStepRef[] | null>(null)
  const applySearchSteps = useCallback((getter: () => MessageSearchStepRef[]) => {
    pendingSearchStepsRef.current = getter()
  }, [])
  const pendingMessageRef = useRef<string | null>(null)
  const clearQueuedDraft = useCallback(() => {
    pendingMessageRef.current = null
    setQueuedDraft(null)
  }, [])
  const restoreQueuedDraftToInput = useCallback(() => {
    const pending = pendingMessageRef.current
    pendingMessageRef.current = null
    setQueuedDraft(null)
    if (!pending) return
    setDraft((prev) => prev.trim() ? prev : pending)
  }, [])
  const clearLiveRunTransientState = useCallback(() => {
    streamingArtifactsRef.current = []
    setStreamingArtifacts([])
    currentRunSourcesRef.current = []
    currentRunArtifactsRef.current = []
    currentRunCodeExecutionsRef.current = []
    currentRunBrowserActionsRef.current = []
    currentRunSubAgentsRef.current = []
    currentRunFileOpsRef.current = []
    currentRunWebFetchesRef.current = []
  }, [])
  const clearLiveRunSecurityArtifacts = useCallback(() => {
    clearLiveRunTransientState()
    resetAssistantTurnLive()
    seenFirstToolCallInRunRef.current = false
    setPendingThinking(false)
    setTopLevelCodeExecutions([])
    setSegments([])
    activeSegmentIdRef.current = null
    currentRunSourcesRef.current = []
    currentRunArtifactsRef.current = []
    currentRunCodeExecutionsRef.current = []
    currentRunBrowserActionsRef.current = []
    resetSearchSteps()
    pendingSearchStepsRef.current = null
    setAwaitingInput(false)
    setPendingUserInput(null)
    setCheckInDraft('')
  }, [clearLiveRunTransientState, resetAssistantTurnLive, resetSearchSteps])

  /** run 结束后等 refreshMessages 落盘再清；避免「流式 DOM 拆掉、助手消息尚未进列表」的一帧空洞 */
  const clearDeferredLiveRunUi = useCallback(() => {
    setLiveAssistantTurn(null)
    setTopLevelCodeExecutions([])
    setTopLevelSubAgents([])
    setTopLevelFileOps([])
    setTopLevelWebFetches([])
    streamingArtifactsRef.current = []
    setStreamingArtifacts([])
    setSegments([])
    activeSegmentIdRef.current = null
    currentRunArtifactsRef.current = []
    currentRunCodeExecutionsRef.current = []
    currentRunBrowserActionsRef.current = []
    currentRunSubAgentsRef.current = []
    currentRunFileOpsRef.current = []
    currentRunWebFetchesRef.current = []
    resetSearchSteps()
  }, [resetSearchSteps])

  const bottomRef = useRef<HTMLDivElement>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const copCodeExecScrollRef = useRef<HTMLDivElement>(null)
  const lastUserMsgRef = useRef<HTMLDivElement>(null)
  const documentPanelScrollFrameRef = useRef<number | null>(null)
  const wasLoadingRef = useRef(false)
  const processedEventCountRef = useRef(0)
  const freezeCutoffRef = useRef<number | null>(null)
  const messageSyncVersionRef = useRef(0)
  // 仅在当前 run 的 SSE 确认进入过连接态后，才允许触发终端兜底。
  const sseTerminalFallbackRunIdRef = useRef<string | null>(null)
  const sseTerminalFallbackArmedRef = useRef(false)
  // 用户是否停留在底部区域（距底部 80px 以内视为"在底部"）
  const isAtBottomRef = useRef(true)
  const [isAtBottom, setIsAtBottom] = useState(true)

  useEffect(() => {
    segmentsRef.current = segments
  }, [segments])

  const beginMessageSync = useCallback(() => {
    messageSyncVersionRef.current += 1
    return messageSyncVersionRef.current
  }, [])

  const isMessageSyncCurrent = useCallback((version: number) => {
    return messageSyncVersionRef.current === version
  }, [])

  const invalidateMessageSync = useCallback(() => {
    messageSyncVersionRef.current += 1
  }, [])

  const readConsistentMessages = useCallback(async (requiredCompletedRunId?: string): Promise<MessageResponse[]> => {
    if (!threadId) return []

    let items = await listMessages(accessToken, threadId)
    if (requiredCompletedRunId && !findAssistantMessageForRun(items, requiredCompletedRunId)) {
      const retriedItems = await listMessages(accessToken, threadId)
      if (
        findAssistantMessageForRun(retriedItems, requiredCompletedRunId) ||
        retriedItems.length >= items.length
      ) {
        items = retriedItems
      }
    }
    return items
  }, [accessToken, threadId])

  const syncBottomState = useCallback((el: HTMLDivElement) => {
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight <= 80
    isAtBottomRef.current = atBottom
    setIsAtBottom(atBottom)
  }, [])

  const handleScrollContainerScroll = useCallback(() => {
    const el = scrollContainerRef.current
    if (!el) return
    syncBottomState(el)
  }, [syncBottomState])

  const stabilizeDocumentPanelScroll = useCallback((trigger?: HTMLElement | null) => {
    const container = scrollContainerRef.current
    if (!container) return

    if (documentPanelScrollFrameRef.current !== null) {
      cancelAnimationFrame(documentPanelScrollFrameRef.current)
      documentPanelScrollFrameRef.current = null
    }

    const anchor = trigger && container.contains(trigger) ? trigger : null
    const anchorTop = anchor
      ? anchor.getBoundingClientRect().top - container.getBoundingClientRect().top
      : null
    const distanceFromBottom = container.scrollHeight - container.scrollTop - container.clientHeight
    const startedAt = performance.now()

    const step = () => {
      const currentContainer = scrollContainerRef.current
      if (!currentContainer) return

      if (anchor && anchorTop !== null && anchor.isConnected && currentContainer.contains(anchor)) {
        const nextTop = anchor.getBoundingClientRect().top - currentContainer.getBoundingClientRect().top
        currentContainer.scrollTop += nextTop - anchorTop
      } else {
        currentContainer.scrollTop = Math.max(0, currentContainer.scrollHeight - currentContainer.clientHeight - distanceFromBottom)
      }

      syncBottomState(currentContainer)

      if (performance.now() - startedAt < 360) {
        documentPanelScrollFrameRef.current = requestAnimationFrame(step)
        return
      }

      documentPanelScrollFrameRef.current = null
    }

    documentPanelScrollFrameRef.current = requestAnimationFrame(step)
  }, [syncBottomState])

  useEffect(() => {
    return () => {
      if (documentPanelScrollFrameRef.current !== null) {
        cancelAnimationFrame(documentPanelScrollFrameRef.current)
      }
    }
  }, [])

  const buildLiveThinkingSnapshot = useCallback((): MessageThinkingRef | null => {
    const snap = snapshotAssistantTurn(assistantTurnFoldStateRef.current)
    const fromTurn = assistantTurnThinkingPlainText(snap)
    const liveSegments = segmentsRef.current
      .filter((s) => s.mode !== 'hidden' && s.content.trim() !== '')
      .map((s) => ({
        segmentId: s.segmentId,
        kind: s.kind,
        mode: s.mode,
        label: s.label,
        content: s.content,
      }))
    if (liveSegments.length === 0 && fromTurn.trim() === '') {
      return null
    }
    return {
      thinkingText: fromTurn,
      segments: liveSegments,
    }
  }, [])

  const prevActiveRunIdRef = useRef<string | null>(null)
  useEffect(() => {
    if (activeRunId && activeRunId !== prevActiveRunIdRef.current) {
      setClawTodos([])
    }
    prevActiveRunIdRef.current = activeRunId
  }, [activeRunId])

  useEffect(() => {
    return () => {
      clearContextCompactHideTimer()
    }
  }, [clearContextCompactHideTimer])

  useEffect(() => {
    if (!activeRunId) {
      clearContextCompactHideTimer()
      setContextCompactBar(null)
    }
  }, [activeRunId, clearContextCompactHideTimer])

  const sse = useSSE({ runId: activeRunId ?? '', accessToken, baseUrl })
  const disconnectSSE = sse.disconnect

  const isStreaming = activeRunId != null

  useEffect(() => {
    if (!activeRunId) setCopThinkingStartedAtMs(undefined)
  }, [activeRunId])

  useEffect(() => {
    if (!activeRunId || !liveAssistantTurn) return
    const hasThinking = liveAssistantTurn.segments.some(
      (s) => s.type === 'cop' && s.items.some((i) => i.kind === 'thinking'),
    )
    if (hasThinking) {
      setCopThinkingStartedAtMs((p) => p ?? Date.now())
    }
  }, [activeRunId, liveAssistantTurn])

  const canCancel =
    activeRunId != null &&
    (sse.state === 'connecting' || sse.state === 'connected' || sse.state === 'reconnecting')

  const refreshMessages = useCallback(async (options?: {
    syncVersion?: number
    requiredCompletedRunId?: string
  }): Promise<MessageResponse[]> => {
    if (!threadId) return []
    const syncVersion = options?.syncVersion ?? beginMessageSync()
    try {
      const items = await readConsistentMessages(options?.requiredCompletedRunId)
      if (!isMessageSyncCurrent(syncVersion)) return []
      setMessages(items)
      return items
    } catch (err) {
      setError(normalizeError(err))
      return []
    }
  }, [threadId, beginMessageSync, readConsistentMessages, isMessageSyncCurrent])

  // 仅用于 streaming 结束后自动发送排队消息（无附件）
  const sendMessage = useCallback(async (text: string) => {
    if (!threadId) {
      setError({ message: '当前没有活动会话，无法发送组件消息。' })
      return
    }
    const normalized = text.trim()
    if (!normalized) return
    if (activeRunId || sending) {
      pendingMessageRef.current = normalized
      setQueuedDraft(normalized)
      return
    }

    const personaKey = readSelectedPersonaKeyFromStorage()
    const modelOverride = readSelectedModelFromStorage() ?? undefined

    setSending(true)
    setError(null)
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null
    try {
      const message = await createMessage(accessToken, threadId, buildMessageRequest(normalized, []))
      invalidateMessageSync()
      setUserEnterMessageId(message.id)
      setMessages((prev) => [...prev, message])
      noResponseMsgIdRef.current = message.id
      const run = await createRun(accessToken, threadId, personaKey, modelOverride, readThreadClawFolder(threadId) ?? undefined)
      if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(threadId)
      resetSearchSteps()
      setActiveRunId(run.run_id)
      onRunStarted(threadId)
      isAtBottomRef.current = true
      setIsAtBottom(true)
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    } finally {
      setSending(false)
    }
  }, [accessToken, threadId, activeRunId, sending, onLoggedOut, onRunStarted, invalidateMessageSync, resetSearchSteps])

  // 用 ref 持有最新的 sendMessage，避免 widget 回调闭包中捕获旧引用
  const sendMessageRef = useRef(sendMessage)
  useEffect(() => { sendMessageRef.current = sendMessage }, [sendMessage])

  const handleArtifactAction = useCallback((action: { type: string; text?: string; message?: string; url?: string }) => {
    if (action.type === 'prompt' && typeof action.text === 'string' && action.text.trim()) {
      void sendMessageRef.current(action.text.trim())
      return
    }
    if (action.type === 'open_link' && typeof action.url === 'string') {
      const u = action.url.trim()
      if (u.startsWith('https://') || u.startsWith('http://')) {
        window.open(u, '_blank', 'noopener,noreferrer')
      }
      return
    }
    if (action.type === 'error' && typeof action.message === 'string' && action.message.trim()) {
      setError({ message: action.message.trim() })
    }
  }, [])

  // 加载 thread 数据
  useEffect(() => {
    if (!threadId) return
    const syncVersion = beginMessageSync()
    let disposed = false

    setMessagesLoading(true)
    setUserEnterMessageId(null)
    setError(null)
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null

    const navUserEnterMessageId = locationState?.userEnterMessageId

    void (async () => {
      let loadedItems: MessageResponse[] | null = null
      try {
        const [initialItems, runs] = await Promise.all([
          listMessages(accessToken, threadId),
          listThreadRuns(accessToken, threadId, 1),
        ])
        if (disposed || !isMessageSyncCurrent(syncVersion)) return

        const latest = runs[0]
        const items = shouldRefetchCompletedRunMessages({ messages: initialItems, latestRun: latest })
          ? await readConsistentMessages(latest.run_id)
          : initialItems
        if (disposed || !isMessageSyncCurrent(syncVersion)) return

        loadedItems = items
        setMessages(items)
        if (latest?.status === 'interrupted') {
          setError({ message: t.runInterrupted })
        }

        // 加载各消息缓存的 web 来源
        const sourcesMap = new Map<string, WebSource[]>()
        const artifactsMap = new Map<string, ArtifactRef[]>()
        const widgetsMap = new Map<string, WidgetRef[]>()
        const codeExecMap = new Map<string, CodeExecutionRef[]>()
        const browserActionsMap = new Map<string, BrowserActionRef[]>()
        const subAgentsMap = new Map<string, SubAgentRef[]>()
        const fileOpsMap = new Map<string, FileOpRef[]>()
        const webFetchesMap = new Map<string, WebFetchRef[]>()
        const thinkingMap = new Map<string, MessageThinkingRef>()
        const searchStepsMap = new Map<string, MessageSearchStepRef[]>()

        const runEventsMap = new Map<string, MsgRunEvent[]>()
        const assistantTurnMap = new Map<string, AssistantTurnUi>()
        for (const msg of items) {
          if (msg.role !== 'assistant') continue

          const cached = readMessageSources(msg.id)
          if (cached) sourcesMap.set(msg.id, cached)
          const cachedArt = readMessageArtifacts(msg.id)
          if (cachedArt) artifactsMap.set(msg.id, cachedArt)
          const cachedWidgets = readMessageWidgets(msg.id)
          if (cachedWidgets) widgetsMap.set(msg.id, cachedWidgets)
          const cachedExec = readMessageCodeExecutions(msg.id)
          if (cachedExec) codeExecMap.set(msg.id, cachedExec)
          const cachedBrowserActions = readMessageBrowserActions(msg.id)
          if (cachedBrowserActions) browserActionsMap.set(msg.id, cachedBrowserActions)
          const cachedSubAgents = readMessageSubAgents(msg.id)
          if (cachedSubAgents) subAgentsMap.set(msg.id, cachedSubAgents)
          const cachedFileOps = readMessageFileOps(msg.id)
          if (cachedFileOps) fileOpsMap.set(msg.id, cachedFileOps)
          const cachedWebFetches = readMessageWebFetches(msg.id)
          if (cachedWebFetches) webFetchesMap.set(msg.id, cachedWebFetches)
          const cachedThinking = readMessageThinking(msg.id)
          if (cachedThinking) thinkingMap.set(msg.id, cachedThinking)
          const cachedSearchSteps = readMessageSearchSteps(msg.id)
          if (cachedSearchSteps) {
            const patched = patchLegacySearchSteps(cachedSearchSteps)
            if (patched.changed) writeMessageSearchSteps(msg.id, patched.steps)
            searchStepsMap.set(msg.id, patched.steps)
          }

          const cachedRunEvents = readMsgRunEvents(msg.id)
          if (cachedRunEvents) runEventsMap.set(msg.id, cachedRunEvents)
          const cachedTurn = readMessageAssistantTurn(msg.id)
          if (cachedTurn) assistantTurnMap.set(msg.id, cachedTurn)
        }

        // 服务端回放：补齐最新一轮的 thinking / 代码执行缓存
        const lastAssistant = latest
          ? findAssistantMessageForRun(items, latest.run_id)
          : [...items].reverse().find((m) => m.role === 'assistant')
        const replayThinkingNeeded = !!(lastAssistant && !thinkingMap.has(lastAssistant.id))
        const replayWidgetsNeeded = !!(lastAssistant && !widgetsMap.has(lastAssistant.id))
        const replayCodeExecNeeded = !!(lastAssistant && shouldReplayMessageCodeExecutions(codeExecMap.get(lastAssistant.id)))
        const replayBrowserActionsNeeded = !!(lastAssistant && !browserActionsMap.has(lastAssistant.id))
        const replaySubAgentsNeeded = !!(lastAssistant && !subAgentsMap.has(lastAssistant.id))
        const replayFileOpsNeeded = !!(lastAssistant && !fileOpsMap.has(lastAssistant.id))
        const replayWebFetchesNeeded = !!(lastAssistant && !webFetchesMap.has(lastAssistant.id))
        const replayAssistantTurnNeeded = !!(lastAssistant && !assistantTurnMap.has(lastAssistant.id))
        if (latest && latest.status !== 'running' && lastAssistant && (replayThinkingNeeded || replayWidgetsNeeded || replayCodeExecNeeded || replayBrowserActionsNeeded || replaySubAgentsNeeded || replayFileOpsNeeded || replayWebFetchesNeeded || replayAssistantTurnNeeded)) {
          try {
            const replayEvents = await listRunEvents(accessToken, latest.run_id, { follow: false })
            if (replayThinkingNeeded) {
              const thinking = buildMessageThinkingFromRunEvents(replayEvents)
              if (thinking) {
                thinkingMap.set(lastAssistant.id, thinking)
                writeMessageThinking(lastAssistant.id, thinking)
              }
            }
            if (replayWidgetsNeeded) {
              const replayWidgets = buildMessageWidgetsFromRunEvents(replayEvents)
              if (replayWidgets.length > 0) {
                widgetsMap.set(lastAssistant.id, replayWidgets)
                writeMessageWidgets(lastAssistant.id, replayWidgets)
              }
            }
            if (replayCodeExecNeeded) {
              const replayExecs = buildMessageCodeExecutionsFromRunEvents(replayEvents)
              codeExecMap.set(lastAssistant.id, replayExecs)
              writeMessageCodeExecutions(lastAssistant.id, replayExecs)
            }
            if (replayBrowserActionsNeeded) {
              const replayActions = buildMessageBrowserActionsFromRunEvents(replayEvents)
              if (replayActions.length > 0) {
                browserActionsMap.set(lastAssistant.id, replayActions)
                writeMessageBrowserActions(lastAssistant.id, replayActions)
              }
            }
            if (replaySubAgentsNeeded) {
              const replayAgents = buildMessageSubAgentsFromRunEvents(replayEvents)
              if (replayAgents.length > 0) {
                subAgentsMap.set(lastAssistant.id, replayAgents)
                writeMessageSubAgents(lastAssistant.id, replayAgents)
              }
            }
            if (replayFileOpsNeeded) {
              const replayFileOps = buildMessageFileOpsFromRunEvents(replayEvents)
              if (replayFileOps.length > 0) {
                fileOpsMap.set(lastAssistant.id, replayFileOps)
                writeMessageFileOps(lastAssistant.id, replayFileOps)
              }
            }
            if (replayWebFetchesNeeded) {
              const replayWebFetches = buildMessageWebFetchesFromRunEvents(replayEvents)
              if (replayWebFetches.length > 0) {
                webFetchesMap.set(lastAssistant.id, replayWebFetches)
                writeMessageWebFetches(lastAssistant.id, replayWebFetches)
              }
            }
            if (replayAssistantTurnNeeded) {
              const replayTurn = buildAssistantTurnFromRunEvents(replayEvents)
              if (replayTurn.segments.length > 0) {
                assistantTurnMap.set(lastAssistant.id, replayTurn)
                writeMessageAssistantTurn(lastAssistant.id, replayTurn)
              }
            }
          } catch {
            // 回放失败不影响主流程
          }
        }

        setMessageSourcesMap(sourcesMap)
        setMessageArtifactsMap(artifactsMap)
        setMessageWidgetsMap(widgetsMap)
        setMessageCodeExecutionsMap(codeExecMap)
        setMessageBrowserActionsMap(browserActionsMap)
        setMessageSubAgentsMap(subAgentsMap)
        setMessageFileOpsMap(fileOpsMap)
        setMessageThinkingMap(thinkingMap)
        setMessageSearchStepsMap(searchStepsMap)

        setMsgRunEventsMap(runEventsMap)
        setMessageAssistantTurnMap(assistantTurnMap)
        setMessageWebFetchesMap(webFetchesMap)

        // 若 location state 已提供 initialRunId，优先使用（来自 WelcomePage 新建后导航）
        // 必须显式调用 setActiveRunId，因为 React Router 复用组件实例，useState 初始值不会重新求值
        if (
          locationState?.initialRunId &&
          (!latest || (latest.run_id === locationState.initialRunId && latest.status === 'running'))
        ) {
          setActiveRunId(locationState.initialRunId)
          if (threadId) onRunStarted(threadId)
        } else {
          const isRunning = latest?.status === 'running'
          setActiveRunId(isRunning ? latest.run_id : null)
          if (isRunning && threadId) onRunStarted(threadId)
          else if (threadId) onRunEnded(threadId)
        }
      } catch (err) {
        if (isApiError(err) && err.status === 401) {
          onLoggedOut()
          return
        }
        setError(normalizeError(err))
      } finally {
        if (!disposed && isMessageSyncCurrent(syncVersion)) {
          if (
            navUserEnterMessageId &&
            loadedItems &&
            loadedItems.some((m) => m.id === navUserEnterMessageId && m.role === 'user')
          ) {
            setUserEnterMessageId(navUserEnterMessageId)
          }
          setMessagesLoading(false)
          if (locationState?.userEnterMessageId) {
            const rest: LocationState = { ...locationState }
            delete rest.userEnterMessageId
            queueMicrotask(() => {
              navigate('.', { replace: true, state: Object.keys(rest as object).length > 0 ? rest : undefined })
            })
          }
        }
      }
    })()
    return () => {
      disposed = true
    }
  // 只在 threadId 变化时重新加载，避免依赖 locationState 导致重复触发
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accessToken, threadId])

  // 切换 thread 时清理 SSE 和排队消息，并重置 pendingIncognito
  useEffect(() => {
    setActiveRunId(null)
    resetAssistantTurnLive()
    seenFirstToolCallInRunRef.current = false
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null
    setPendingThinking(false)
    setSegments([])
    activeSegmentIdRef.current = null
    setTopLevelCodeExecutions([])
    setTopLevelSubAgents([])
    setTopLevelFileOps([])
    setTopLevelWebFetches([])
    setCancelSubmitting(false)
    setAwaitingInput(false)
    setPendingUserInput(null)
    setCheckInDraft('')
    pendingMessageRef.current = null
    setQueuedDraft(null)
    currentRunSourcesRef.current = []
    currentRunArtifactsRef.current = []
    currentRunCodeExecutionsRef.current = []
    currentRunBrowserActionsRef.current = []
    currentRunSubAgentsRef.current = []
    currentRunFileOpsRef.current = []
    currentRunWebFetchesRef.current = []
    setMessageSourcesMap(new Map())
    setMessageArtifactsMap(new Map())
    setMessageWidgetsMap(new Map())
    setMessageCodeExecutionsMap(new Map())
    setMessageBrowserActionsMap(new Map())
    setMessageSubAgentsMap(new Map())
    setMessageFileOpsMap(new Map())
    setMessageWebFetchesMap(new Map())
    setMessageThinkingMap(new Map())
    setMessageSearchStepsMap(new Map())
    setMessageAssistantTurnMap(new Map())
    setMsgRunEventsMap(new Map())
    setSourcePanelMessageId(null)
    disconnectSSE()
    sse.clearEvents()
    streamingArtifactsRef.current = []
    setStreamingArtifacts([])
    resetSearchSteps()
    // 不重置 processedEventCountRef: clearEvents 是异步的，若此处归零，
    // 同一 effects 阶段内事件处理 effect 会重放旧事件导致串线。
    // activeRunId effect 在新 run 启动时负责归零。
    setPendingIncognito(false)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [threadId, resetAssistantTurnLive])

  // 同步 pendingIncognito 到 AppLayout（用于 Sidebar 无痕 UI）
  useEffect(() => {
    onSetPendingIncognito(pendingIncognito)
    return () => { onSetPendingIncognito(false) }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pendingIncognito])

  const handleTitleBarIncognitoClick = useCallback(() => {
    if (!threadId) return
    if (privateThreadIds.has(threadId)) return
    if (pendingIncognito) {
      setPendingIncognito(false)
      return
    }
    if (messages.length > 0) {
      setPendingIncognito(true)
      return
    }
    onTogglePrivateMode()
  }, [threadId, privateThreadIds, pendingIncognito, messages.length, onTogglePrivateMode])

  useLayoutEffect(() => {
    if (!isDesktop() || !setTitleBarIncognitoClick) return
    setTitleBarIncognitoClick(handleTitleBarIncognitoClick)
    return () => { setTitleBarIncognitoClick(null) }
  }, [setTitleBarIncognitoClick, handleTitleBarIncognitoClick])

  // 连接 SSE
  useEffect(() => {
    if (!activeRunId) return
    freezeCutoffRef.current = null
    injectionBlockedRunIdRef.current = null
    sseTerminalFallbackRunIdRef.current = activeRunId
    sseTerminalFallbackArmedRef.current = false
    seenFirstToolCallInRunRef.current = false
    sse.reset()
    sse.connect()
    processedEventCountRef.current = 0
    currentRunSourcesRef.current = []
    currentRunArtifactsRef.current = []
    currentRunCodeExecutionsRef.current = []
    currentRunBrowserActionsRef.current = []
    currentRunSubAgentsRef.current = []
    currentRunFileOpsRef.current = []
    currentRunWebFetchesRef.current = []
    resetAssistantTurnLive()
    setSegments([])
    activeSegmentIdRef.current = null
    setTopLevelCodeExecutions([])
    setTopLevelSubAgents([])
    setTopLevelFileOps([])
    setTopLevelWebFetches([])
    streamingArtifactsRef.current = []
    setStreamingArtifacts([])
    setCancelSubmitting(false)
    return () => { sse.disconnect() }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeRunId, baseUrl, resetAssistantTurnLive])

  // 避免上一轮 run 的 closed/error 状态误触发当前 run 的终端兜底。
  useEffect(() => {
    if (!activeRunId) {
      sseTerminalFallbackRunIdRef.current = null
      sseTerminalFallbackArmedRef.current = false
      return
    }
    if (
      sse.state === 'connecting' ||
      sse.state === 'connected' ||
      sse.state === 'reconnecting'
    ) {
      sseTerminalFallbackRunIdRef.current = activeRunId
      sseTerminalFallbackArmedRef.current = true
    }
  }, [activeRunId, sse.state])

  // 页面从后台回到前台时，若 SSE 已断开则重连
  useEffect(() => {
    const onVisibilityChange = () => {
      if (document.visibilityState !== 'visible') return
      if (!activeRunId) return
      const s = sse.state
      if (s === 'closed' || s === 'error' || s === 'idle') {
        sse.reconnect()
      }
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => document.removeEventListener('visibilitychange', onVisibilityChange)
  }, [activeRunId, sse.state, sse.reconnect]) // eslint-disable-line react-hooks/exhaustive-deps

  // 处理 SSE 事件
  useEffect(() => {
    if (!activeRunId) return
    const resetTerminalRunState = (options?: {
      restoreQueuedDraft?: boolean
      preserveSearchSteps?: boolean
    }) => {
      freezeCutoffRef.current = null
      injectionBlockedRunIdRef.current = null
      sse.disconnect()
      setActiveRunId(null)
      setCancelSubmitting(false)
      setPendingThinking(false)
      setTopLevelCodeExecutions([])
      setTopLevelSubAgents([])
      setTopLevelFileOps([])
      setTopLevelWebFetches([])
      if (options?.restoreQueuedDraft) {
        restoreQueuedDraftToInput()
      } else {
        clearQueuedDraft()
      }
      streamingArtifactsRef.current = []
      setStreamingArtifacts([])
      setSegments([])
      resetAssistantTurnLive()
      activeSegmentIdRef.current = null
      currentRunSourcesRef.current = []
      currentRunArtifactsRef.current = []
      currentRunCodeExecutionsRef.current = []
      currentRunBrowserActionsRef.current = []
      currentRunSubAgentsRef.current = []
      currentRunFileOpsRef.current = []
      currentRunWebFetchesRef.current = []
      if (!options?.preserveSearchSteps) {
        resetSearchSteps()
      }
      pendingSearchStepsRef.current = null
      setAwaitingInput(false)
      setPendingUserInput(null)
      setCheckInDraft('')
      if (threadId) onRunEnded(threadId)
    }
    const { fresh, nextProcessedCount } = selectFreshRunEvents({
      events: sse.events,
      activeRunId,
      processedCount: processedEventCountRef.current,
    })
    processedEventCountRef.current = nextProcessedCount
    for (const event of fresh) {
      const freezeCutoff = freezeCutoffRef.current
      if (
        freezeCutoff != null &&
        typeof event.seq === 'number' &&
        event.seq > freezeCutoff &&
        !isTerminalRunEventType(event.type)
      ) {
        continue
      }
      if (shouldSuppressLiveRunEventAfterInjectionBlock({
        activeRunId,
        blockedRunId: injectionBlockedRunIdRef.current,
        event,
      })) {
        continue
      }

      const nextWebSearchSteps = applyRunEventToWebSearchSteps(searchStepsRef.current, event)
      if (nextWebSearchSteps !== searchStepsRef.current) {
        searchStepsRef.current = nextWebSearchSteps
        setSearchSteps(nextWebSearchSteps)
      }

      if (event.type === 'run.segment.start') {
        const obj = event.data as { segment_id?: unknown; kind?: unknown; display?: unknown }
        const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
        const kind = typeof obj.kind === 'string' ? obj.kind : 'planning_round'
        const display = (obj.display ?? {}) as { mode?: unknown; label?: unknown; queries?: unknown }
        const mode = typeof display.mode === 'string' ? display.mode : 'collapsed'
        const label = typeof display.label === 'string' ? display.label : ''
        if (!segmentId) continue
        activeSegmentIdRef.current = segmentId
        requestAssistantTurnThinkingBreak(assistantTurnFoldStateRef.current)
        if (kind.startsWith('search_')) {
          continue
        }
        setSegments((prev) => [...prev, { segmentId, kind, mode, label, content: '', isStreaming: true, codeExecutions: [] }])
        continue
      }

      if (event.type === 'run.context_compact') {
        const obj = event.data as { phase?: unknown; op?: unknown; dropped_prefix?: unknown }
        const op = typeof obj.op === 'string' ? obj.op : undefined
        const phase = typeof obj.phase === 'string' ? obj.phase : undefined

        if (op === 'persist') {
          if (phase === 'started') {
            clearContextCompactHideTimer()
            setContextCompactBar({ type: 'persist', status: 'running' })
          } else if (phase === 'completed' || phase === undefined) {
            clearContextCompactHideTimer()
            setContextCompactBar({ type: 'persist', status: 'done' })
            contextCompactHideTimerRef.current = window.setTimeout(() => {
              setContextCompactBar(null)
              contextCompactHideTimerRef.current = null
            }, 2800)
          } else if (phase === 'llm_failed') {
            // LLM summarization failed - show brief warning then clear
            clearContextCompactHideTimer()
            setContextCompactBar({ type: 'persist', status: 'llm_failed' })
            contextCompactHideTimerRef.current = window.setTimeout(() => {
              setContextCompactBar(null)
              contextCompactHideTimerRef.current = null
            }, 4000)
          }
        } else if (op === 'trim') {
          if (phase === 'completed') {
            const dropped = typeof obj.dropped_prefix === 'number' ? obj.dropped_prefix : 0
            if (dropped > 0) {
              clearContextCompactHideTimer()
              setContextCompactBar({ type: 'trim', status: 'done', dropped })
              contextCompactHideTimerRef.current = window.setTimeout(() => {
                setContextCompactBar(null)
                contextCompactHideTimerRef.current = null
              }, 1500)
            }
          }
        }
        continue
      }

      if (event.type === 'todo.updated') {
        const obj = event.data as { todos?: unknown }
        if (Array.isArray(obj.todos)) {
          const items = (obj.todos as unknown[]).flatMap((t) => {
            if (!t || typeof t !== 'object') return []
            const item = t as { id?: unknown; content?: unknown; status?: unknown }
            if (typeof item.id !== 'string' || typeof item.content !== 'string' || typeof item.status !== 'string') return []
            return [{ id: item.id, content: item.content, status: item.status }]
          })
          setClawTodos(items)
        }
        continue
      }

      if (event.type === 'run.segment.end') {
        const obj = event.data as { segment_id?: unknown }
        const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
        if (segmentId && activeSegmentIdRef.current === segmentId) {
          activeSegmentIdRef.current = null
        }
        requestAssistantTurnThinkingBreak(assistantTurnFoldStateRef.current)
        setSegments((prev) =>
          prev.map((s) => (s.segmentId === segmentId ? { ...s, isStreaming: false } : s)),
        )
        continue
      }

      if (event.type === 'message.delta') {
        if (isACPDelegateEventData(event.data)) continue
        noResponseMsgIdRef.current = null
        const obj = event.data as { content_delta?: unknown; role?: unknown; channel?: unknown }
        if (obj.role != null && obj.role !== 'assistant') continue
        if (typeof obj.content_delta !== 'string' || !obj.content_delta) continue
        const delta = obj.content_delta
        const isThinking = obj.channel === 'thinking'
        const activeSeg = activeSegmentIdRef.current
        if (isThinking) {
          setPendingThinking(false)
          setCopThinkingStartedAtMs((prev) => prev ?? Date.now())
          foldAssistantTurnEvent(assistantTurnFoldStateRef.current, event)
          bumpAssistantTurnSnapshot()
          continue
        }
        setPendingThinking(false)
        if (activeSeg) {
          requestAssistantTurnThinkingBreak(assistantTurnFoldStateRef.current)
          setSegments((prev) =>
            prev.map((s) =>
              s.segmentId === activeSeg && s.mode !== 'hidden'
                ? { ...s, content: s.content + delta }
                : s,
            ),
          )
          continue
        }
        foldAssistantTurnEvent(assistantTurnFoldStateRef.current, event)
        bumpAssistantTurnSnapshot()
        continue
      }

      if (event.type === 'tool.call.delta') {
        const obj = event.data as { tool_call_index?: number; tool_call_id?: string; tool_name?: string; arguments_delta?: string }
        const idx = typeof obj.tool_call_index === 'number' ? obj.tool_call_index : -1
        if (idx >= 0 && typeof obj.arguments_delta === 'string') {
          let entry = streamingArtifactsRef.current.find((e) => e.toolCallIndex === idx)
          if (!entry) {
            entry = { toolCallIndex: idx, argumentsBuffer: '', complete: false }
            streamingArtifactsRef.current = [...streamingArtifactsRef.current, entry]
          }
          if (obj.tool_call_id) entry.toolCallId = obj.tool_call_id
          if (obj.tool_name) entry.toolName = obj.tool_name
          entry.argumentsBuffer += obj.arguments_delta

          if (entry.toolName === 'show_widget' || entry.toolName === 'create_artifact' || (!entry.toolName && (entry.argumentsBuffer.includes('"content"') || entry.argumentsBuffer.includes('"widget_code"')))) {
            const parsed = extractPartialArtifactFields(entry.argumentsBuffer)
            if (parsed.title !== undefined) entry.title = parsed.title
            if (parsed.filename !== undefined) entry.filename = parsed.filename
            if (parsed.display !== undefined) entry.display = parsed.display as 'inline' | 'panel'
            if (parsed.content !== undefined) entry.content = parsed.content
            setStreamingArtifacts([...streamingArtifactsRef.current])
          }
        }
        continue
      }

      if (event.type === 'tool.call') {
        if (isACPDelegateEventData(event.data)) continue
        setPendingThinking(false)
        seenFirstToolCallInRunRef.current = true
        const obj = event.data as { tool_name?: unknown; llm_name?: unknown; tool_call_id?: unknown; arguments?: unknown }
        const toolName = typeof obj.tool_name === 'string' ? obj.tool_name : event.tool_name
        const codeExecutionCall = applyCodeExecutionToolCall(currentRunCodeExecutionsRef.current, event)
        if (codeExecutionCall.appended) {
          const entry: CodeExecution = codeExecutionCall.appended
          currentRunCodeExecutionsRef.current = codeExecutionCall.nextExecutions
          const activeSeg = activeSegmentIdRef.current
          if (activeSeg) {
            setSegments((prev) =>
              prev.map((s) =>
                s.segmentId === activeSeg
                  ? { ...s, codeExecutions: [...s.codeExecutions, entry] }
                  : s,
              ),
            )
          }
          setTopLevelCodeExecutions((prev) => [...prev, entry])
        }
        const browserCall = applyBrowserToolCall(currentRunBrowserActionsRef.current, event)
        if (browserCall.appended) {
          currentRunBrowserActionsRef.current = browserCall.nextActions
        }
        const subAgentCall = applySubAgentToolCall(currentRunSubAgentsRef.current, event)
        if (subAgentCall.appended) {
          currentRunSubAgentsRef.current = subAgentCall.nextAgents
          setTopLevelSubAgents((prev) => [...prev, subAgentCall.appended!])
        }
        const fileOpCall = applyFileOpToolCall(currentRunFileOpsRef.current, event)
        if (fileOpCall.appended) {
          currentRunFileOpsRef.current = fileOpCall.nextOps
          setTopLevelFileOps((prev) => [...prev, fileOpCall.appended!])
        }
        const webFetchCall = applyWebFetchToolCall(currentRunWebFetchesRef.current, event)
        if (webFetchCall.appended) {
          currentRunWebFetchesRef.current = webFetchCall.nextFetches
          setTopLevelWebFetches((prev) => [...prev, webFetchCall.appended!])
        }
        // show_widget tool.call: mark streaming entry as complete
        if (toolName === 'show_widget') {
          const args = obj.arguments as Record<string, unknown> | undefined
          const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : undefined
          let entry = callId
            ? streamingArtifactsRef.current.find((e) => e.toolCallId === callId)
            : undefined
          if (!entry) {
            entry = {
              toolCallIndex: streamingArtifactsRef.current.length,
              toolCallId: callId,
              toolName: 'show_widget',
              argumentsBuffer: '',
              complete: false,
            }
            streamingArtifactsRef.current = [...streamingArtifactsRef.current, entry]
          }
          entry.complete = true
          entry.toolName = 'show_widget'
          if (typeof args?.widget_code === 'string') entry.content = args.widget_code
          if (typeof args?.title === 'string') entry.title = args.title
          setStreamingArtifacts([...streamingArtifactsRef.current])
        }
        // create_artifact tool.call: mark streaming entry as complete
        if (toolName === 'create_artifact') {
          const args = obj.arguments as Record<string, unknown> | undefined
          const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : undefined
          let entry = callId
            ? streamingArtifactsRef.current.find((e) => e.toolCallId === callId)
            : undefined
          if (!entry) {
            entry = {
              toolCallIndex: streamingArtifactsRef.current.length,
              toolCallId: callId,
              toolName: 'create_artifact',
              argumentsBuffer: '',
              complete: false,
            }
            streamingArtifactsRef.current = [...streamingArtifactsRef.current, entry]
          }
          entry.complete = true
          entry.toolName = 'create_artifact'
          if (typeof args?.content === 'string') entry.content = args.content
          if (typeof args?.title === 'string') entry.title = args.title
          if (typeof args?.filename === 'string') entry.filename = args.filename
          if (typeof args?.display === 'string') entry.display = args.display as 'inline' | 'panel'
          setStreamingArtifacts([...streamingArtifactsRef.current])
        }
        foldAssistantTurnEvent(assistantTurnFoldStateRef.current, event)
        bumpAssistantTurnSnapshot()
        continue
      }

      // Handle terminal output delta events for real-time streaming
      if (event.type === 'terminal.stdout_delta' || event.type === 'terminal.stderr_delta') {
        const deltaPatch = applyTerminalDelta(currentRunCodeExecutionsRef.current, event)
        if (deltaPatch.updated) {
          currentRunCodeExecutionsRef.current = deltaPatch.nextExecutions
          setTopLevelCodeExecutions((prev) => patchCodeExecutionList(prev, deltaPatch.updated!).next)
          setSegments((prev) =>
            prev.map((segment) => ({
              ...segment,
              codeExecutions: patchCodeExecutionList(segment.codeExecutions, deltaPatch.updated!).next,
            })),
          )
        }
        continue
      }

      if (event.type === 'tool.result') {
        if (isACPDelegateEventData(event.data)) continue
        const obj = event.data as { tool_name?: unknown; tool_call_id?: unknown; result?: unknown; error?: unknown }
        const resultToolName = typeof obj.tool_name === 'string' ? obj.tool_name : ''
        if (isWebSearchToolName(resultToolName)) {
          const result = obj.result as { results?: unknown[] } | undefined
          if (Array.isArray(result?.results)) {
            const newSources: WebSource[] = result.results
              .filter((r): r is Record<string, unknown> => r != null && typeof r === 'object')
              .map((r) => ({
                title: typeof r.title === 'string' ? r.title : '',
                url: typeof r.url === 'string' ? r.url : '',
                snippet: typeof r.snippet === 'string' ? r.snippet : undefined,
              }))
              .filter((s) => !!s.url)
            currentRunSourcesRef.current = [...currentRunSourcesRef.current, ...newSources]
          }
        }
        // 检测 sandbox 执行产物 + document_write / create_artifact 产物 + browser 产物
        if (obj.tool_name === 'python_execute' || obj.tool_name === 'exec_command' || obj.tool_name === 'write_stdin' || obj.tool_name === 'document_write' || obj.tool_name === 'create_artifact' || obj.tool_name === 'browser') {
          const result = obj.result as { artifacts?: unknown[]; stdout?: unknown; stderr?: unknown; exit_code?: unknown; output?: unknown } | undefined
          if (Array.isArray(result?.artifacts)) {
            const newArtifacts: ArtifactRef[] = result.artifacts
              .filter((a): a is Record<string, unknown> => a != null && typeof a === 'object')
              .filter((a) => typeof a.key === 'string' && typeof a.filename === 'string')
              .map((a) => ({
                key: a.key as string,
                filename: a.filename as string,
                size: typeof a.size === 'number' ? a.size : 0,
                mime_type: typeof a.mime_type === 'string' ? a.mime_type : '',
                title: typeof a.title === 'string' ? a.title : undefined,
                display: a.display === 'inline' || a.display === 'panel' ? a.display as 'inline' | 'panel' : undefined,
              }))
            if (newArtifacts.length > 0) {
              currentRunArtifactsRef.current = [...currentRunArtifactsRef.current, ...newArtifacts]
              // link artifact refs to streaming entries
              if (obj.tool_name === 'create_artifact') {
                const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : undefined
                for (const art of newArtifacts) {
                  const entry = callId
                    ? streamingArtifactsRef.current.find((e) => e.toolCallId === callId)
                    : undefined
                  if (entry) {
                    entry.artifactRef = art
                  }
                }
                setStreamingArtifacts([...streamingArtifactsRef.current])
              }
            }
          }
          const codeExecutionResult = applyCodeExecutionToolResult(currentRunCodeExecutionsRef.current, event)
          if (codeExecutionResult.updated) {
            currentRunCodeExecutionsRef.current = codeExecutionResult.nextExecutions
            const target: CodeExecution = codeExecutionResult.updated
            if (codeExecutionResult.appended) {
              setTopLevelCodeExecutions((prev) => [...prev, target])
            } else {
              setTopLevelCodeExecutions((prev) => patchCodeExecutionList(prev, target).next)
              setSegments((prev) =>
                prev.map((segment) => ({
                  ...segment,
                  codeExecutions: patchCodeExecutionList(segment.codeExecutions, target).next,
                })),
              )
            }
          }
        }
        // browser tool.result
        if (obj.tool_name === 'browser') {
          const browserResult = applyBrowserToolResult(currentRunBrowserActionsRef.current, event)
          if (browserResult.updated) {
            currentRunBrowserActionsRef.current = browserResult.nextActions
          }
        }
        // sub-agent tool.result
        const subAgentResult = applySubAgentToolResult(currentRunSubAgentsRef.current, event)
        if (subAgentResult.updated) {
          currentRunSubAgentsRef.current = subAgentResult.nextAgents
          setTopLevelSubAgents((prev) => {
            const idx = prev.findIndex((a) => a.id === subAgentResult.updated!.id)
            if (idx >= 0) return prev.map((a, i) => i === idx ? subAgentResult.updated! : a)
            return [...prev, subAgentResult.updated!]
          })
        }
        // file op tool.result
        const fileOpResult = applyFileOpToolResult(currentRunFileOpsRef.current, event)
        if (fileOpResult.updated) {
          currentRunFileOpsRef.current = fileOpResult.nextOps
          setTopLevelFileOps((prev) => {
            const idx = prev.findIndex((o) => o.id === fileOpResult.updated!.id)
            if (idx >= 0) return prev.map((o, i) => i === idx ? fileOpResult.updated! : o)
            return [...prev, fileOpResult.updated!]
          })
        }
        // web_fetch tool.result
        const webFetchResult = applyWebFetchToolResult(currentRunWebFetchesRef.current, event)
        if (webFetchResult.updated) {
          currentRunWebFetchesRef.current = webFetchResult.nextFetches
          setTopLevelWebFetches((prev) => {
            const idx = prev.findIndex((f) => f.id === webFetchResult.updated!.id)
            if (idx >= 0) return prev.map((f, i) => i === idx ? webFetchResult.updated! : f)
            return [...prev, webFetchResult.updated!]
          })
        }
        foldAssistantTurnEvent(assistantTurnFoldStateRef.current, event)
        bumpAssistantTurnSnapshot()
        continue
      }

      if (event.type === 'thread.title.updated') {
        const obj = event.data as { thread_id?: unknown; title?: unknown }
        const tid = typeof obj.thread_id === 'string' ? obj.thread_id : threadId
        const title = typeof obj.title === 'string' ? obj.title : ''
        if (tid && title) onThreadTitleUpdated(tid, title)
        continue
      }

      if (event.type === 'run.input_requested') {
        const data = event.data as Record<string, unknown> | undefined
        const message = data?.message as string | undefined
        const schema = data?.requestedSchema as RequestedSchema | undefined
        if (message && schema && schema.properties && Object.keys(schema.properties).length > 0) {
          // 规范化 required 字段，防止 LLM 传非数组值导致前端崩溃
          const safeSchema: RequestedSchema = {
            ...schema,
            required: Array.isArray(schema.required) ? schema.required : undefined,
          }
          setPendingUserInput({
            request_id: (data?.request_id as string) ?? '',
            message,
            requestedSchema: safeSchema,
          })
        } else {
          setAwaitingInput(true)
        }
        continue
      }

      if (event.type === 'security.injection.blocked') {
        freezeCutoffRef.current = null
        injectionBlockedRunIdRef.current = event.run_id
        sseTerminalFallbackArmedRef.current = false
        sseTerminalFallbackRunIdRef.current = null
        restoreQueuedDraftToInput()
        sse.disconnect()
        setActiveRunId(null)
        setCancelSubmitting(false)
        setError(null)
        clearLiveRunSecurityArtifacts()
        setInjectionBlocked(getInjectionBlockMessage(event))
        if (threadId) onRunEnded(threadId)
        continue
      }

      if (event.type === 'run.completed') {
        if (isACPDelegateEventData(event.data)) continue
        freezeCutoffRef.current = null
        const completedRunId = event.run_id
        injectionBlockedRunIdRef.current = null
        noResponseMsgIdRef.current = null
        replaceOnCancelRef.current = null
        const runThinking = buildLiveThinkingSnapshot()
        const runWidgets = collectCompletedWidgets(streamingArtifactsRef.current)
        const runAssistantTurn = drainAssistantTurnForPersist(assistantTurnFoldStateRef.current)
        setLiveAssistantTurn(runAssistantTurn.segments.length > 0 ? runAssistantTurn : null)
        sse.disconnect()
        setActiveRunId(null)
        setCancelSubmitting(false)
        setPendingThinking(false)

        const runSearchSteps = finalizeSearchSteps(searchStepsRef.current)
        if (runSearchSteps.length > 0) applySearchSteps(() => runSearchSteps)
        setQueuedDraft(null)
        setAwaitingInput(false)
        setPendingUserInput(null)
        setCheckInDraft('')
        if (threadId) onRunEnded(threadId)
        refreshCredits()
        const runSources = [...currentRunSourcesRef.current]
        const runArtifacts = [...currentRunArtifactsRef.current]
        const runCodeExecs = [...currentRunCodeExecutionsRef.current]
        const runBrowserActions = [...currentRunBrowserActionsRef.current]
        const runSubAgents = [...currentRunSubAgentsRef.current]
        const runFileOps = [...currentRunFileOpsRef.current]
        const runWebFetches = [...currentRunWebFetchesRef.current]
        void refreshMessages({ requiredCompletedRunId: completedRunId })
          .then((items) => {
            const completedAssistant = findAssistantMessageForRun(items, completedRunId)
            if (completedAssistant) {
              if (runWidgets.length > 0) {
                writeMessageWidgets(completedAssistant.id, runWidgets)
                setMessageWidgetsMap((prev) => new Map(prev).set(completedAssistant.id, runWidgets))
              }
              if (runSources.length > 0) {
                writeMessageSources(completedAssistant.id, runSources)
                setMessageSourcesMap((prev) => new Map(prev).set(completedAssistant.id, runSources))
              }
              const pendingSearchSteps = pendingSearchStepsRef.current
              pendingSearchStepsRef.current = null
              if (pendingSearchSteps && pendingSearchSteps.length > 0) {
                writeMessageSearchSteps(completedAssistant.id, pendingSearchSteps)
                setMessageSearchStepsMap((prev) => new Map(prev).set(completedAssistant.id, pendingSearchSteps))
              }
              if (runAssistantTurn.segments.length > 0) {
                writeMessageAssistantTurn(completedAssistant.id, runAssistantTurn)
                setMessageAssistantTurnMap((prev) => new Map(prev).set(completedAssistant.id, runAssistantTurn))
              }
              if (runArtifacts.length > 0) {
                writeMessageArtifacts(completedAssistant.id, runArtifacts)
                setMessageArtifactsMap((prev) => new Map(prev).set(completedAssistant.id, runArtifacts))
              }
              writeMessageCodeExecutions(completedAssistant.id, runCodeExecs)
              setMessageCodeExecutionsMap((prev) => new Map(prev).set(completedAssistant.id, runCodeExecs))
              if (runBrowserActions.length > 0) {
                writeMessageBrowserActions(completedAssistant.id, runBrowserActions)
                setMessageBrowserActionsMap((prev) => new Map(prev).set(completedAssistant.id, runBrowserActions))
              }
              if (runSubAgents.length > 0) {
                writeMessageSubAgents(completedAssistant.id, runSubAgents)
                setMessageSubAgentsMap((prev) => new Map(prev).set(completedAssistant.id, runSubAgents))
              }
              if (runFileOps.length > 0) {
                writeMessageFileOps(completedAssistant.id, runFileOps)
                setMessageFileOpsMap((prev) => new Map(prev).set(completedAssistant.id, runFileOps))
              }
              if (runWebFetches.length > 0) {
                writeMessageWebFetches(completedAssistant.id, runWebFetches)
                setMessageWebFetchesMap((prev) => new Map(prev).set(completedAssistant.id, runWebFetches))
              }
              if (runThinking) {
                writeMessageThinking(completedAssistant.id, runThinking)
                setMessageThinkingMap((prev) => new Map(prev).set(completedAssistant.id, runThinking))
              }

              const completedRunEvents = (sse.events as MsgRunEvent[]).filter(
                (e) => e.run_id === completedRunId,
              )
              if (completedRunEvents.length > 0) {
                writeMsgRunEvents(completedAssistant.id, completedRunEvents)
                setMsgRunEventsMap((prev) => new Map(prev).set(completedAssistant.id, completedRunEvents))
              }
            }
            const pending = pendingMessageRef.current
            if (pending) {
              pendingMessageRef.current = null
              void sendMessageRef.current(pending)
            }
          })
          .finally(clearDeferredLiveRunUi)
        // 标题 summarizer 在 worker 内异步跑，超时约 30s；SSE 在 run.completed 已断，只能靠轮询对齐侧栏
        if (threadId) {
          const tid = threadId
          const pollTitle = (remaining: number) => {
            if (remaining <= 0) return
            setTimeout(() => {
              void getThread(accessToken, tid).then((resp) => {
                const next = (resp.title ?? '').trim()
                const cur = (threadsRef.current.find((th) => th.id === tid)?.title ?? '').trim()
                if (next && next !== cur) onThreadTitleUpdated(tid, next)
                if (remaining > 1) pollTitle(remaining - 1)
              }).catch(() => {
                if (remaining > 1) pollTitle(remaining - 1)
              })
            }, 2000)
          }
          pollTitle(16)
        }
        continue
      }

      if (event.type === 'run.cancelled') {
        if (isACPDelegateEventData(event.data)) continue
        freezeCutoffRef.current = null
        const blockedByInjection = injectionBlockedRunIdRef.current === event.run_id
        resetTerminalRunState({ restoreQueuedDraft: true, preserveSearchSteps: true })
        if (!blockedByInjection) {
          setError(null)
        }
        if (event.run_id) {
          void refreshMessages({ requiredCompletedRunId: event.run_id })
            .finally(clearDeferredLiveRunUi)
        }
        continue
      }

      if (event.type === 'run.failed') {
        if (isACPDelegateEventData(event.data)) continue
        resetTerminalRunState({ restoreQueuedDraft: true, preserveSearchSteps: true })
        const obj = event.data as { message?: unknown; error_class?: unknown; code?: unknown; details?: unknown }
        const errorClass = typeof obj?.error_class === 'string' ? obj.error_class : undefined
        const details = (obj?.details && typeof obj.details === 'object' && !Array.isArray(obj.details))
          ? obj.details as Record<string, unknown>
          : undefined

        if (errorClass === 'security.injection_blocked') {
          // 注入拦截：渲染在对话流中，不用底部 error card
          setInjectionBlocked(typeof obj?.message === 'string' ? obj.message : 'blocked')
        } else {
          setError({
            message: typeof obj?.message === 'string' ? obj.message : '运行失败',
            code: typeof obj?.code === 'string' ? obj.code : errorClass,
            details,
          })
        }
        continue
      }

      if (event.type === 'run.interrupted') {
        if (isACPDelegateEventData(event.data)) continue
        resetTerminalRunState({ restoreQueuedDraft: true, preserveSearchSteps: true })
        const obj = event.data as { message?: unknown; error_class?: unknown; code?: unknown; details?: unknown }
        const errorClass = typeof obj?.error_class === 'string' ? obj.error_class : undefined
        const details = (obj?.details && typeof obj.details === 'object' && !Array.isArray(obj.details))
          ? obj.details as Record<string, unknown>
          : undefined

        setError({
          message: typeof obj?.message === 'string' ? obj.message : t.runInterrupted,
          code: typeof obj?.code === 'string' ? obj.code : errorClass,
          details,
        })
        if (event.run_id) {
          void refreshMessages({ requiredCompletedRunId: event.run_id })
            .finally(clearDeferredLiveRunUi)
        }
        continue
      }
    }
  }, [activeRunId, clearContextCompactHideTimer, clearDeferredLiveRunUi, clearLiveRunSecurityArtifacts, clearQueuedDraft, refreshMessages, refreshCredits, resetSearchSteps, restoreQueuedDraftToInput, sse.events]) // eslint-disable-line react-hooks/exhaustive-deps

  // 401 SSE 错误时登出
  useEffect(() => {
    if (sse.error instanceof SSEApiError && sse.error.status === 401) {
      onLoggedOut()
    }
  }, [sse.error, onLoggedOut])

  // SSE 流结束但未收到终端事件时的兜底清理
  // 正常流程中 run.completed 会先 setActiveRunId(null)，所以此处不会触发
  // 仅在 SSE 异常断连（如网络中断达到重试上限）时才生效
  useEffect(() => {
    if (!activeRunId) return
    if (sse.state !== 'closed' && sse.state !== 'error') return
    if (!sseTerminalFallbackArmedRef.current) return
    if (sseTerminalFallbackRunIdRef.current !== activeRunId) return
    const terminalRunId = activeRunId

    // run.completed 等终端事件处理中会同步 setActiveRunId(null)，
    // React 批量更新后 activeRunId 已经为 null，不会到达此处。
    // 到达此处说明 SSE 关闭时确实没有处理过终端事件。
    sseTerminalFallbackArmedRef.current = false
    sseTerminalFallbackRunIdRef.current = null
    const runThinking = buildLiveThinkingSnapshot()
    const runSources = [...currentRunSourcesRef.current]
    const runArtifacts = [...currentRunArtifactsRef.current]
    const runWidgets = collectCompletedWidgets(streamingArtifactsRef.current)
    const runAssistantTurn = drainAssistantTurnForPersist(assistantTurnFoldStateRef.current)
    setLiveAssistantTurn(runAssistantTurn.segments.length > 0 ? runAssistantTurn : null)
    const runCodeExecs = [...currentRunCodeExecutionsRef.current]
    const runBrowserActions = [...currentRunBrowserActionsRef.current]
    const runSubAgents = [...currentRunSubAgentsRef.current]
    const runFileOps = [...currentRunFileOpsRef.current]
    const runWebFetches2 = [...currentRunWebFetchesRef.current]

    setActiveRunId(null)
    setPendingThinking(false)
    setQueuedDraft(null)
    setAwaitingInput(false)
    setPendingUserInput(null)
    setCheckInDraft('')
    if (threadId) onRunEnded(threadId)
    refreshCredits()

    void refreshMessages({ requiredCompletedRunId: terminalRunId })
      .then((items) => {
        const completedAssistant = findAssistantMessageForRun(items, terminalRunId)
        if (completedAssistant) {
          if (runWidgets.length > 0) {
            writeMessageWidgets(completedAssistant.id, runWidgets)
            setMessageWidgetsMap((prev) => new Map(prev).set(completedAssistant.id, runWidgets))
          }
          if (runSources.length > 0) {
            writeMessageSources(completedAssistant.id, runSources)
            setMessageSourcesMap((prev) => new Map(prev).set(completedAssistant.id, runSources))
          }
          if (runAssistantTurn.segments.length > 0) {
            writeMessageAssistantTurn(completedAssistant.id, runAssistantTurn)
            setMessageAssistantTurnMap((prev) => new Map(prev).set(completedAssistant.id, runAssistantTurn))
          }
          if (runArtifacts.length > 0) {
            writeMessageArtifacts(completedAssistant.id, runArtifacts)
            setMessageArtifactsMap((prev) => new Map(prev).set(completedAssistant.id, runArtifacts))
          }
          writeMessageCodeExecutions(completedAssistant.id, runCodeExecs)
          setMessageCodeExecutionsMap((prev) => new Map(prev).set(completedAssistant.id, runCodeExecs))
          if (runBrowserActions.length > 0) {
            writeMessageBrowserActions(completedAssistant.id, runBrowserActions)
            setMessageBrowserActionsMap((prev) => new Map(prev).set(completedAssistant.id, runBrowserActions))
          }
          if (runSubAgents.length > 0) {
            writeMessageSubAgents(completedAssistant.id, runSubAgents)
            setMessageSubAgentsMap((prev) => new Map(prev).set(completedAssistant.id, runSubAgents))
          }
          if (runFileOps.length > 0) {
            writeMessageFileOps(completedAssistant.id, runFileOps)
            setMessageFileOpsMap((prev) => new Map(prev).set(completedAssistant.id, runFileOps))
          }
          if (runWebFetches2.length > 0) {
            writeMessageWebFetches(completedAssistant.id, runWebFetches2)
            setMessageWebFetchesMap((prev) => new Map(prev).set(completedAssistant.id, runWebFetches2))
          }
          if (runThinking) {
            writeMessageThinking(completedAssistant.id, runThinking)
            setMessageThinkingMap((prev) => new Map(prev).set(completedAssistant.id, runThinking))
          }
        }
      })
      .finally(clearDeferredLiveRunUi)
  }, [activeRunId, sse.state, buildLiveThinkingSnapshot, clearDeferredLiveRunUi]) // eslint-disable-line react-hooks/exhaustive-deps

  // 初始加载完成后，将最后一条 user 消息滚动至顶部
  useEffect(() => {
    if (messagesLoading) {
      wasLoadingRef.current = true
      return
    }
    if (!wasLoadingRef.current) return
    wasLoadingRef.current = false
    requestAnimationFrame(() => {
      lastUserMsgRef.current?.scrollIntoView({ behavior: 'instant', block: 'start' })
    })
  }, [messagesLoading])

  // 新消息/流式内容时，仅在用户停留在底部时自动滚动
  useEffect(() => {
    if (!isAtBottomRef.current) return
    const liveHandoffPaint =
      liveAssistantTurn != null && liveAssistantTurn.segments.length > 0
    bottomRef.current?.scrollIntoView({
      behavior: isStreaming || liveHandoffPaint ? 'instant' : 'smooth',
    })
  }, [messages, liveAssistantTurn, isStreaming])

  // COP 代码执行列表：新 item 添加时自动滚动到底部
  useEffect(() => {
    const el = copCodeExecScrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [topLevelCodeExecutions.length, liveAssistantTurn])

  // 发送新消息时强制滚动到底部（用户主动操作，应该跟上）
  const scrollToBottom = useCallback(() => {
    isAtBottomRef.current = true
    setIsAtBottom(true)
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [])

  const revokeDraftAttachment = useCallback((attachment: Attachment) => {
    if (attachment.preview_url) URL.revokeObjectURL(attachment.preview_url)
  }, [])

  const attachmentsRef = useRef<Attachment[]>([])

  useEffect(() => {
    attachmentsRef.current = attachments
  }, [attachments])

  useEffect(() => {
    return () => {
      attachmentsRef.current.forEach((attachment) => revokeDraftAttachment(attachment))
    }
  }, [revokeDraftAttachment])

  const handleAttachFiles = useCallback((files: File[]) => {
    const newAttachments = files.map((file) => ({
      id: `${file.name}-${file.size}-${file.lastModified}`,
      file,
      name: file.name,
      size: file.size,
      mime_type: file.type || 'application/octet-stream',
      preview_url: file.type.startsWith('image/') ? URL.createObjectURL(file) : undefined,
      status: 'uploading' as const,
    }))
    if (newAttachments.length === 0) return
    setAttachments((prev) => {
      const existingIDs = new Set(prev.map((item) => item.id))
      const deduped = newAttachments.filter((item) => !existingIDs.has(item.id))
      return [...prev, ...deduped]
    })
    if (!threadId) return
    for (const att of newAttachments) {
      uploadThreadAttachment(accessToken, threadId, att.file)
        .then((uploaded) => {
          setAttachments((prev) =>
            prev.map((a) => a.id === att.id ? { ...a, status: 'ready' as const, uploaded } : a),
          )
        })
        .catch(() => {
          setAttachments((prev) =>
            prev.map((a) => a.id === att.id ? { ...a, status: 'error' as const } : a),
          )
        })
    }
  }, [accessToken, threadId])

  const handlePasteContent = useCallback((text: string) => {
    const ts = Math.floor(Date.now() / 1000)
    const filename = `pasted-${ts}.txt`
    const blob = new Blob([text], { type: 'text/plain' })
    const file = new File([blob], filename, { type: 'text/plain', lastModified: Date.now() })
    const lineCount = text.split('\n').length
    const att: Attachment = {
      id: `${filename}-${file.size}-${Date.now()}`,
      file,
      name: filename,
      size: file.size,
      mime_type: 'text/plain',
      status: 'uploading',
      pasted: { text, lineCount },
    }
    setAttachments((prev) => [...prev, att])
    if (!threadId) return
    uploadThreadAttachment(accessToken, threadId, file)
      .then((uploaded) => {
        setAttachments((prev) =>
          prev.map((a) => a.id === att.id ? { ...a, status: 'ready' as const, uploaded } : a),
        )
      })
      .catch(() => {
        setAttachments((prev) =>
          prev.map((a) => a.id === att.id ? { ...a, status: 'error' as const } : a),
        )
      })
  }, [accessToken, threadId])

  const handleRemoveAttachment = useCallback((id: string) => {
    setAttachments((prev) => {
      const target = prev.find((item) => item.id === id)
      if (target) revokeDraftAttachment(target)
      return prev.filter((item) => item.id !== id)
    })
  }, [revokeDraftAttachment])

  const handleSend = async (e: React.FormEvent<HTMLFormElement>, personaKey: string, modelOverride?: string) => {
    e.preventDefault()
    if (sending || !threadId) return

    // streaming 期间排队，输出结束后自动发送
    if (isStreaming) {
      const text = draft.trim()
      if (text) {
        pendingMessageRef.current = text
        setQueuedDraft(text)
        setDraft('')
      }
      return
    }

    const text = draft.trim()
    if (!text && attachments.length === 0) return

    setSending(true)
    setPendingThinking(true)
    setError(null)
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null

    try {
      const uploadAttachments = async (targetThreadId: string) => {
        return await Promise.all(
          attachments.map(async (attachment) => {
            if (attachment.uploaded) return attachment.uploaded
            return await uploadThreadAttachment(accessToken, targetThreadId, attachment.file)
          }),
        )
      }

      // 首次在无痕模式下发送：先 fork 出一个 private thread，再在其中发送
      if (pendingIncognito && messages.length > 0) {
        const lastMessageId = messages[messages.length - 1].id
        const forked = await forkThread(accessToken, threadId, lastMessageId, true)
        if (forked.id_mapping) migrateMessageMetadata(forked.id_mapping)
        onThreadCreated(forked)
        const uploaded = await uploadAttachments(forked.id)
        const forkUserMessage = await createMessage(accessToken, forked.id, buildMessageRequest(text, uploaded))
        const run = await createRun(accessToken, forked.id, personaKey, modelOverride, readThreadClawFolder(threadId) ?? undefined)
        if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(forked.id)
        attachments.forEach((attachment) => revokeDraftAttachment(attachment))
        setDraft('')
        setAttachments([])
        navigate(`/t/${forked.id}`, {
          state: {
            isIncognitoFork: true,
            initialRunId: run.run_id,
            forkBaseCount: messages.length,
            userEnterMessageId: forkUserMessage.id,
          },
          replace: false,
        })
        onRunStarted(forked.id)
        return
      }

      const replaceMessageId = replaceOnCancelRef.current
      replaceOnCancelRef.current = null

      if (replaceMessageId && attachments.length === 0) {
        // 取消后重发：替换上一条未响应的用户消息
        attachments.forEach((attachment) => revokeDraftAttachment(attachment))
        setDraft('')
        setAttachments([])
        injectionBlockedRunIdRef.current = null
        invalidateMessageSync()
        const originalMsg = messages.find((m) => m.id === replaceMessageId)
        const nonTextParts = originalMsg?.content_json?.parts?.filter((p) => p.type !== 'text') ?? []
        const replacedContentJson: MessageContent | undefined = originalMsg?.content_json
          ? { parts: [{ type: 'text', text }, ...nonTextParts] }
          : undefined
        setMessages((prev) => {
          const idx = prev.findIndex((m) => m.id === replaceMessageId)
          if (idx === -1) return prev
          return prev.slice(0, idx + 1).map((m, i) =>
            i === idx ? { ...m, content: text, content_json: replacedContentJson ?? m.content_json } : m,
          )
        })
        const run = await editMessage(accessToken, threadId, replaceMessageId, text, replacedContentJson)
        if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(threadId)
        noResponseMsgIdRef.current = replaceMessageId
        resetSearchSteps()
        setActiveRunId(run.run_id)
        onRunStarted(threadId)
        scrollToBottom()
      } else {
        const uploaded = await uploadAttachments(threadId)
        const message = await createMessage(accessToken, threadId, buildMessageRequest(text, uploaded))
        invalidateMessageSync()
        setUserEnterMessageId(message.id)
        setMessages((prev) => [...prev, message])
        attachments.forEach((attachment) => revokeDraftAttachment(attachment))
        setDraft('')
        setAttachments([])
        injectionBlockedRunIdRef.current = null
        noResponseMsgIdRef.current = message.id

        const run = await createRun(accessToken, threadId, personaKey, modelOverride, readThreadClawFolder(threadId) ?? undefined)
        if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(threadId)
        resetSearchSteps()
        setActiveRunId(run.run_id)
        onRunStarted(threadId)
        scrollToBottom()
      }
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    } finally {
      setSending(false)
    }
  }

  const handleEditMessage = useCallback(async (original: MessageResponse, newContent: string) => {
    if (isStreaming || sending || !threadId) return
    setSending(true)
    setError(null)
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null
    try {
      const nonTextParts = original.content_json?.parts?.filter((p) => p.type !== 'text') ?? []
      const newContentJson: MessageContent | undefined = original.content_json
        ? { parts: [{ type: 'text', text: newContent }, ...nonTextParts] }
        : undefined
      const run = await editMessage(accessToken, threadId, original.id, newContent, newContentJson)
      // 乐观更新：同步更新 content 和 content_json，保留附件 parts
      invalidateMessageSync()
      setMessages((prev) => {
        const idx = prev.findIndex((m) => m.id === original.id)
        if (idx === -1) return prev
        return prev.slice(0, idx + 1).map((m, i) =>
          i === idx ? { ...m, content: newContent, content_json: newContentJson ?? m.content_json } : m,
        )
      })
      resetSearchSteps()
      setActiveRunId(run.run_id)
      onRunStarted(threadId)
      scrollToBottom()
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    } finally {
      setSending(false)
    }
  }, [accessToken, threadId, isStreaming, sending, onRunStarted, onLoggedOut, scrollToBottom, invalidateMessageSync, resetSearchSteps])

  const handleRetry = useCallback(async () => {
    if (isStreaming || sending || !threadId) return
    setSending(true)
    setError(null)
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null
    try {
      const run = await retryThread(accessToken, threadId)
      // 乐观地移除最后一条 assistant 消息（后端已标记 hidden）
      invalidateMessageSync()
      setMessages((prev) => {
        const lastAssistantIdx = prev.map((m) => m.role).lastIndexOf('assistant')
        if (lastAssistantIdx === -1) return prev
        return prev.filter((_, i) => i !== lastAssistantIdx)
      })
      resetSearchSteps()
      setActiveRunId(run.run_id)
      onRunStarted(threadId)
      scrollToBottom()
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    } finally {
      setSending(false)
    }
  }, [accessToken, threadId, isStreaming, sending, onRunStarted, onLoggedOut, scrollToBottom, invalidateMessageSync, resetSearchSteps])

  const handleAsrError = useCallback((err: unknown) => {
    if (isApiError(err) && err.status === 401) {
      onLoggedOut()
      return
    }
    setError(normalizeError(err))
  }, [onLoggedOut])

  const handleFork = useCallback(async (messageId: string) => {
    if (!threadId || isStreaming || sending) return
    setError(null)
    setInjectionBlocked(null)
    try {
      const forked = await forkThread(accessToken, threadId, messageId)
      if (forked.id_mapping) migrateMessageMetadata(forked.id_mapping)
      onThreadCreated(forked)
      navigate(`/t/${forked.id}`)
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    }
  }, [accessToken, threadId, isStreaming, sending, onLoggedOut, onThreadCreated, navigate])

  const handleCheckInSubmit = useCallback(async () => {
    if (!activeRunId || checkInSubmitting) return
    const text = checkInDraft.trim()
    if (!text) return

    setCheckInSubmitting(true)
    setError(null)
    setInjectionBlocked(null)
    try {
      await provideInput(accessToken, activeRunId, text)
      setCheckInDraft('')
      setAwaitingInput(false)
      setPendingUserInput(null)
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    } finally {
      setCheckInSubmitting(false)
    }
  }, [activeRunId, accessToken, checkInDraft, checkInSubmitting, onLoggedOut])

  const handleUserInputSubmit = useCallback(async (response: UserInputResponse) => {
    if (!activeRunId) return
    setError(null)
    setInjectionBlocked(null)
    try {
      await provideInput(accessToken, activeRunId, JSON.stringify(response.answers))
      setPendingUserInput(null)
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    }
  }, [accessToken, activeRunId, onLoggedOut])

  const handleUserInputDismiss = useCallback(async () => {
    if (!activeRunId) return
    const req = pendingUserInput
    if (!req) return
    setError(null)
    setInjectionBlocked(null)
    try {
      await provideInput(accessToken, activeRunId, JSON.stringify({}))
      setPendingUserInput(null)
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    }
  }, [accessToken, activeRunId, pendingUserInput, onLoggedOut])

  const handleCancel = useCallback(() => {
    if (!activeRunId || cancelSubmitting) return
    const runId = activeRunId
    freezeCutoffRef.current = sse.lastSeq

    // 若模型还未响应，记录该消息 ID 供下次发送时替换
    if (noResponseMsgIdRef.current) {
      replaceOnCancelRef.current = noResponseMsgIdRef.current
      noResponseMsgIdRef.current = null
    }

    setCancelSubmitting(true)
    setError(null)
    setInjectionBlocked(null)

    void cancelRun(accessToken, runId, sse.lastSeq).catch((err: unknown) => {
      setError(normalizeError(err))
      freezeCutoffRef.current = null
      setCancelSubmitting(false)
    })
  }, [activeRunId, cancelSubmitting, accessToken, sse.lastSeq])

  const terminalSseError = useMemo(() => {
    if (!sse.error) return null
    return normalizeError(sse.error)
  }, [sse.error])

  const lastUserMsgIdx = useMemo(() => {
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].role === 'user') return i
    }
    return -1
  }, [messages])

  const clearUserEnterAnimation = useCallback(() => {
    setUserEnterMessageId(null)
  }, [])

  const resolvedMessageSources = useMemo(() => {
    return resolveMessageSourcesForRender(messages, messageSourcesMap)
  }, [messages, messageSourcesMap])

  const sourcePanelSources = sourcePanelMessageId ? resolvedMessageSources.get(sourcePanelMessageId) : undefined
  const sourcePanelUserQuery = useMemo(() => {
    if (!sourcePanelMessageId) return undefined
    const idx = messages.findIndex((m) => m.id === sourcePanelMessageId)
    for (let i = idx - 1; i >= 0; i--) {
      if (messages[i].role === 'user') return messages[i].content
    }
    return undefined
  }, [sourcePanelMessageId, messages])

  // 保留最近一次数据，使关闭时面板内容在过渡动画期间仍可见
  if (sourcePanelSources) lastPanelSourcesRef.current = sourcePanelSources
  if (sourcePanelUserQuery !== undefined) lastPanelQueryRef.current = sourcePanelUserQuery
  if (codePanelExecution) lastCodePanelRef.current = codePanelExecution
  if (documentPanelArtifact) lastDocumentPanelRef.current = documentPanelArtifact
  const panelDisplaySources = sourcePanelSources ?? lastPanelSourcesRef.current
  const panelDisplayQuery = sourcePanelUserQuery ?? lastPanelQueryRef.current
  const codePanelDisplay = codePanelExecution ?? lastCodePanelRef.current
  const documentPanelDisplay = documentPanelArtifact ?? lastDocumentPanelRef.current
  const isSourcePanelOpen = !!(sourcePanelSources && sourcePanelSources.length > 0)
  const isCodePanelOpen = !!codePanelExecution
  const isDocumentPanelOpen = !!documentPanelArtifact
  const isPanelOpen = isSourcePanelOpen || isCodePanelOpen || isDocumentPanelOpen

  const allReadFiles = useMemo(() => {
    const seen = new Set<string>()
    const result: string[] = []
    const addOps = (ops: FileOpRef[]) => {
      for (const op of ops) {
        if (op.toolName === 'read_file' && op.status === 'success' && op.label && op.label !== 'read file') {
          if (!seen.has(op.label)) { seen.add(op.label); result.push(op.label) }
        }
      }
    }
    for (const ops of messageFileOpsMap.values()) addOps(ops)
    addOps(topLevelFileOps)
    return result
  }, [messageFileOpsMap, topLevelFileOps])

  const openCodePanel = useCallback((ce: CodeExecution) => {
    setCodePanelExecution((prev) => {
      if (prev?.id === ce.id) {
        onRightPanelChange?.(false)
        return null
      }
      setSourcePanelMessageId(null)
      setDocumentPanelArtifact(null)
      onRightPanelChange?.(true)
      return ce
    })
  }, [onRightPanelChange])

  const openDocumentPanel = useCallback((artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => {
    stabilizeDocumentPanelScroll(options?.trigger)
    setDocumentPanelArtifact((prev) => {
      if (prev?.artifact.key === artifact.key) {
        onRightPanelChange?.(false)
        return null
      }
      setSourcePanelMessageId(null)
      setCodePanelExecution(null)
      onRightPanelChange?.(true)
      return {
        artifact,
        artifacts: options?.artifacts ?? [],
        runId: options?.runId,
      }
    })
  }, [onRightPanelChange, stabilizeDocumentPanelScroll])

  // COP step 计数：timeline 中所有非 finished 的点
  const dedupedTopLevelCodeExecutions = useMemo(() => {
    const lastIdxById = new Map<string, number>()
    topLevelCodeExecutions.forEach((ce, i) => lastIdxById.set(ce.id, i))
    return topLevelCodeExecutions.filter((ce, i) => lastIdxById.get(ce.id) === i)
  }, [topLevelCodeExecutions])

  const allStreamItems = useMemo(() => [
    ...dedupedTopLevelCodeExecutions.map(ce => ({ kind: 'code' as const, id: ce.id, seq: ce.seq ?? 0, item: ce })),
    ...topLevelSubAgents.map(a => ({ kind: 'agent' as const, id: a.id, seq: a.seq ?? 0, item: a })),
    ...topLevelFileOps.map(op => ({ kind: 'fileop' as const, id: op.id, seq: op.seq ?? 0, item: op })),
    ...topLevelWebFetches.map(wf => ({ kind: 'fetch' as const, id: wf.id, seq: wf.seq ?? 0, item: wf })),
  ].sort((a, b) => a.seq - b.seq), [dedupedTopLevelCodeExecutions, topLevelSubAgents, topLevelFileOps, topLevelWebFetches])

  const livePlacedShowWidgetCallIds = useMemo(() => liveCopShowWidgetCallIds(liveAssistantTurn), [liveAssistantTurn])
  const livePlacedCreateArtifactCallIds = useMemo(() => liveCopCreateArtifactCallIds(liveAssistantTurn), [liveAssistantTurn])

  const copTimelineStreamHiddenIds = useMemo(() => {
    if (!liveAssistantTurn || liveAssistantTurn.segments.length === 0) return new Set<string>()
    return toolCallIdsInCopTimelines(liveAssistantTurn, {
      codeExecutions: dedupedTopLevelCodeExecutions,
      fileOps: topLevelFileOps,
      webFetches: topLevelWebFetches,
      subAgents: topLevelSubAgents,
      searchSteps,
      sources: currentRunSourcesRef.current,
    })
  }, [liveAssistantTurn, dedupedTopLevelCodeExecutions, topLevelFileOps, topLevelWebFetches, topLevelSubAgents, searchSteps])

  const allStreamItemsForUi = useMemo(() => {
    if (copTimelineStreamHiddenIds.size === 0) return allStreamItems
    return allStreamItems.filter((e) => !copTimelineStreamHiddenIds.has(e.id))
  }, [allStreamItems, copTimelineStreamHiddenIds])

  return (
    <div className="relative flex min-w-0 flex-1 flex-col overflow-hidden bg-[var(--c-bg-page)]">
      {/* 顶部 header */}
      <div className="flex min-h-[60px] items-center justify-between gap-2 px-[15px] py-[8px]">
        {/* 左侧：对话标题 */}
        <div className="flex min-w-0 flex-1 items-center pl-[5px]">
          {threadId && currentTitle && (
            editingTitle !== null ? (
              <input
                ref={editTitleInputRef}
                value={editingTitle}
                onChange={(e) => setEditingTitle(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    renameCancelledRef.current = false
                    void commitRename(editingTitle)
                  } else if (e.key === 'Escape') {
                    renameCancelledRef.current = true
                    setEditingTitle(null)
                  }
                }}
                onBlur={() => {
                  if (!renameCancelledRef.current) {
                    void commitRename(editingTitle)
                  }
                  renameCancelledRef.current = false
                }}
                style={{
                  fontSize: '14px',
                  fontWeight: 450,
                  color: 'var(--c-text-primary)',
                  background: 'var(--c-bg-deep)',
                  border: '0.5px solid var(--c-border-subtle)',
                  borderRadius: '8px',
                  padding: '5px 10px',
                  outline: 'none',
                  minWidth: 0,
                  maxWidth: '320px',
                  width: '100%',
                }}
              />
            ) : (
              <div
                ref={titleContainerRef}
                className="title-group flex items-stretch gap-[3px]"
                style={{ transform: 'translateY(-3px)' }}
              >
                {/* 标题文字 */}
                <button
                  onClick={openTitleMenu}
                  className="title-part"
                  style={{
                    borderRadius: '7px 0 0 7px',
                    padding: '5px 10px',
                    fontSize: '14px',
                    fontWeight: 350,
                    maxWidth: '280px',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {currentTitle}
                </button>
                {/* 展开箭头 */}
                <button
                  ref={titleChevronRef}
                  onClick={openTitleMenu}
                  className="title-part"
                  style={{
                    borderRadius: '0 7px 7px 0',
                    padding: '5px 8px',
                    display: 'flex',
                    alignItems: 'center',
                  }}
                >
                  <ChevronDown size={14} style={{ flexShrink: 0 }} />
                </button>
              </div>
            )
          )}
        </div>

        {/* 右侧：操作按钮 */}
        <div className="flex items-center gap-2">
          {!isDesktop() && (
            <ModeSwitch
              mode={appMode}
              onChange={onSetAppMode}
              labels={{ chat: t.modeChat, claw: t.modeClaw }}
              availableModes={availableAppModes}
            />
          )}
          {threadId && privateThreadIds.has(threadId) && (
            <span className="text-xs font-medium text-[var(--c-text-muted)]">{t.incognitoLabel}</span>
          )}
          {!isDesktop() && (
            <NotificationBell accessToken={accessToken} onClick={onOpenNotifications} refreshKey={notificationVersion} title={t.notificationsTitle} />
          )}
          {!isDesktop() && threadId && !privateThreadIds.has(threadId) && (
            <button
              onClick={() => setShareModalOpen(true)}
              title={t.shareTitle}
              className="flex h-8 w-8 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
            >
              <Share2 size={18} />
            </button>
          )}
          {!isDesktop() && (
            <button
              onClick={
                threadId && privateThreadIds.has(threadId)
                  ? undefined
                  : pendingIncognito
                    ? () => setPendingIncognito(false)
                    : messages.length > 0
                      ? () => setPendingIncognito(true)
                      : onTogglePrivateMode
              }
              title={threadId && privateThreadIds.has(threadId) ? t.thisThreadIsIncognito : t.toggleIncognito}
              className={[
                'flex h-8 w-8 items-center justify-center rounded-lg transition-colors',
                threadId && privateThreadIds.has(threadId) || pendingIncognito
                  ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]'
                  : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]',
                threadId && privateThreadIds.has(threadId) ? 'cursor-default' : 'cursor-pointer',
              ].join(' ')}
            >
              <Glasses size={18} />
            </button>
          )}
        </div>
      </div>

      {/* 主体区域：消息 + 输入 + 可选的 sources 侧边面板 */}
      <div className="relative flex flex-1 min-h-0">
        <div className="relative flex flex-1 min-w-0 flex-col">
          {/* 消息列表 */}
          <div
            ref={scrollContainerRef}
            onScroll={handleScrollContainerScroll}
            className="chat-scroll-hidden relative flex-1 min-h-0 overflow-y-auto bg-[var(--c-bg-page)] [scrollbar-gutter:stable]"
          >
        <div
          style={{ maxWidth: 800, margin: '0 auto', padding: `50px ${isPanelOpen ? '32px' : '60px'} 200px`, transition: 'padding 280ms cubic-bezier(0.16,1,0.3,1)' }}
          className="flex w-full flex-col gap-6"
        >
          {messagesLoading ? (
            <div className="py-20 text-center text-sm text-[var(--c-text-muted)]">{t.loading}</div>
          ) : (
            <>
              {contextCompactBar && (
                <ContextCompactBar
                  variant={contextCompactBar}
                  runningLabel={t.desktopSettings.chatCompactBannerRunning}
                  doneLabel={t.desktopSettings.chatCompactBannerDone}
                  trimLabel={t.desktopSettings.chatCompactBannerTrim}
                  llmFailedLabel={t.desktopSettings.chatCompactBannerLlmFailed}
                />
              )}
              {messages.map((msg, idx) => {
                const resolvedSources = msg.role === 'assistant' ? resolvedMessageSources.get(msg.id) : undefined
                const canShowSources = !!(resolvedSources && resolvedSources.length > 0)
                const historicalTurn = msg.role === 'assistant' ? messageAssistantTurnMap.get(msg.id) : undefined
                const hasAssistantTurn = !!(historicalTurn && historicalTurn.segments.length > 0)
                const msgWidgetsRaw =
                  msg.role === 'assistant' ? (messageWidgetsMap.get(msg.id) ?? readMessageWidgets(msg.id) ?? undefined) : undefined
                const bubbleWidgets =
                  msg.role === 'assistant' && historicalTurn && historicalTurn.segments.length > 0
                    ? msgWidgetsRaw?.filter((w) => !widgetToolCallIdsPlacedInTurn(historicalTurn, msgWidgetsRaw).has(w.id))
                    : msgWidgetsRaw

                const messageCodeExecutions = msg.role === 'assistant' ? messageCodeExecutionsMap.get(msg.id) : undefined
                const hasMessageCodeExecutions = !!(messageCodeExecutions && messageCodeExecutions.length > 0)
                const messageSubAgents = msg.role === 'assistant' ? messageSubAgentsMap.get(msg.id) : undefined
                const messageSearchSteps = msg.role === 'assistant' ? messageSearchStepsMap.get(msg.id) : undefined
                const timelineSteps = messageSearchSteps ?? []
                const messageFileOps = msg.role === 'assistant' ? messageFileOpsMap.get(msg.id) : undefined
                const messageWebFetches = msg.role === 'assistant' ? messageWebFetchesMap.get(msg.id) : undefined
                const msgThinking = msg.role === 'assistant' ? messageThinkingMap.get(msg.id) : undefined
                return (
                  <div key={msg.id} ref={idx === lastUserMsgIdx ? lastUserMsgRef : undefined}>
                  {msg.role === 'assistant' && hasAssistantTurn && (
                    <div style={{ marginBottom: '6px', display: 'flex', flexDirection: 'column', gap: 0, maxWidth: '663px' }}>
                      {!isSearchThread &&
                        msgThinking != null &&
                        msgThinking.thinkingText.trim() !== '' &&
                        !turnHasCopThinkingItems(historicalTurn!) && (
                        <CopTimeline
                          key={`${msg.id}-legacy-thinking`}
                          steps={[]}
                          sources={[]}
                          isComplete
                          assistantThinking={{ markdown: msgThinking.thinkingText, live: false }}
                          accessToken={accessToken}
                          baseUrl={baseUrl}
                        />
                      )}
                      {historicalTurn!.segments.map((seg, si) =>
                        seg.type === 'text' ? (
                          <MarkdownRenderer
                            key={`${msg.id}-at-${si}`}
                            content={seg.content}
                            webSources={resolvedSources}
                            artifacts={messageArtifactsMap.get(msg.id)}
                            accessToken={accessToken}
                            runId={msg.run_id ?? undefined}
                            onOpenDocument={openDocumentPanel}
                            trimTrailingMargin={
                              historicalTurn!.segments[si + 1] == null ||
                              historicalTurn!.segments[si + 1]?.type === 'cop'
                            }
                          />
                        ) : (
                          (() => {
                            const payload = copTimelinePayloadForSegment(seg, {
                              codeExecutions: messageCodeExecutions,
                              fileOps: messageFileOps,
                              webFetches: messageWebFetches,
                              subAgents: messageSubAgents,
                              searchSteps: messageSearchSteps ?? [],
                              sources: resolvedSources ?? [],
                            })
                            const histWidgets = historicWidgetsForCop(seg, msgWidgetsRaw)
                            const thinkingRowsHist = !isSearchThread
                              ? thinkingRowsForCop(seg, {
                                  live: false,
                                  segmentIndex: si,
                                  lastSegmentIndex: historicalTurn!.segments.length - 1,
                                })
                              : []
                            const copInlineHist = !isSearchThread
                              ? copInlineTextRowsForCop(seg, {
                                  live: false,
                                  segmentIndex: si,
                                  lastSegmentIndex: historicalTurn!.segments.length - 1,
                                })
                              : []
                            if (
                              copSegmentCalls(seg).length === 0 &&
                              thinkingRowsHist.length === 0 &&
                              copInlineHist.length === 0 &&
                              histWidgets.length === 0
                            ) {
                              return null
                            }
                            const timelineTitleOverride = seg.title?.trim() || undefined
                            const histTrail = historicalTurn!.segments[si + 1]
                            const histTrailingText =
                              histTrail?.type === 'text' && histTrail.content.length > 0
                            return (
                              <Fragment key={`${msg.id}-acw-${si}`}>
                                <CopTimeline
                                  steps={payload.steps}
                                  sources={payload.sources}
                                  isComplete
                                  codeExecutions={payload.codeExecutions}
                                  onOpenCodeExecution={openCodePanel}
                                  activeCodeExecutionId={codePanelExecution?.id}
                                  subAgents={payload.subAgents}
                                  fileOps={payload.fileOps}
                                  webFetches={payload.webFetches}
                                  headerOverride={timelineTitleOverride}
                                  thinkingRows={thinkingRowsHist.length > 0 ? thinkingRowsHist : undefined}
                                  copInlineTextRows={copInlineHist.length > 0 ? copInlineHist : undefined}
                                  trailingAssistantTextPresent={histTrailingText}
                                  accessToken={accessToken}
                                  baseUrl={baseUrl}
                                />
                                {histWidgets.map((w) => (
                                  <WidgetBlock
                                    key={w.id}
                                    html={w.html}
                                    title={w.title}
                                    complete
                                    onAction={handleArtifactAction}
                                  />
                                ))}
                              </Fragment>
                            )
                          })()
                        ),
                      )}
                    </div>
                  )}
                  {msg.role === 'assistant' && !hasAssistantTurn && (timelineSteps.length > 0 || hasMessageCodeExecutions || (messageSubAgents && messageSubAgents.length > 0) || (messageFileOps && messageFileOps.length > 0) || (messageWebFetches && messageWebFetches.length > 0)) && (
                    <div style={{ marginBottom: '12px' }}>
                      {timelineSteps.length > 0 && (
                        <CopTimeline
                          steps={timelineSteps}
                          sources={resolvedSources ?? []}
                          isComplete
                          codeExecutions={messageCodeExecutions}
                          onOpenCodeExecution={openCodePanel}
                          activeCodeExecutionId={codePanelExecution?.id}
                          subAgents={messageSubAgents}
                          fileOps={messageFileOps}
                          webFetches={messageWebFetches}
                          accessToken={accessToken}
                          baseUrl={baseUrl}
                        />
                      )}
                      {timelineSteps.length === 0 && (hasMessageCodeExecutions || (messageSubAgents && messageSubAgents.length > 0) || (messageFileOps && messageFileOps.length > 0) || (messageWebFetches && messageWebFetches.length > 0)) && (
                        <CopTimeline
                          steps={[]}
                          sources={[]}
                          isComplete
                          codeExecutions={messageCodeExecutions}
                          onOpenCodeExecution={openCodePanel}
                          activeCodeExecutionId={codePanelExecution?.id}
                          subAgents={messageSubAgents}
                          fileOps={messageFileOps}
                          webFetches={messageWebFetches}
                          accessToken={accessToken}
                          baseUrl={baseUrl}
                        />
                      )}
                    </div>
                  )}
                  <MessageBubble
                    message={msg}
                    streamAssistantMarkdown={
                      isStreaming && msg.role === 'assistant' && idx === messages.length - 1
                    }
                    animateUserEnter={msg.role === 'user' && msg.id === userEnterMessageId}
                    onUserEnterAnimationEnd={msg.role === 'user' && msg.id === userEnterMessageId ? clearUserEnterAnimation : undefined}
                    onRetry={
                      msg.role === 'assistant' && idx === messages.length - 1 && !isStreaming && !sending
                        ? handleRetry
                        : undefined
                    }
                    onEdit={
                      msg.role === 'user' && !isStreaming && !sending
                        ? (newContent) => handleEditMessage(msg, newContent)
                        : undefined
                    }
                    onFork={
                      msg.role === 'assistant' && !isStreaming && !sending
                        ? () => void handleFork(msg.id)
                        : undefined
                    }
                    onShare={
                      msg.role === 'assistant' && !isStreaming && !sending && threadId && !privateThreadIds.has(threadId)
                        ? () => {
                            if (sharingMessageId) return
                            setSharingMessageId(msg.id)
                            createThreadShare(accessToken, threadId, 'public')
                              .then((share) => {
                                const url = `${window.location.origin}/s/${share.token}`
                                void navigator.clipboard.writeText(url)
                                setSharingMessageId(null)
                                setSharedMessageId(msg.id)
                                setTimeout(() => setSharedMessageId(null), 1500)
                              })
                              .catch(() => {
                                setSharingMessageId(null)
                              })
                          }
                        : undefined
                    }
                    shareState={
                      sharingMessageId === msg.id ? 'sharing' : sharedMessageId === msg.id ? 'shared' : 'idle'
                    }
                    webSources={resolvedSources}
                    artifacts={msg.role === 'assistant' ? messageArtifactsMap.get(msg.id) : undefined}
                    browserActions={msg.role === 'assistant' ? messageBrowserActionsMap.get(msg.id) : undefined}
                    widgets={bubbleWidgets}
                    accessToken={accessToken}
                    onWidgetAction={msg.role === 'assistant' ? handleArtifactAction : undefined}
                    onShowSources={
                      msg.role === 'assistant' && canShowSources
                        ? () => {
                            setCodePanelExecution(null)
                            setDocumentPanelArtifact(null)
                            setSourcePanelMessageId((prev) => {
                              const next = prev === msg.id ? null : msg.id
                              onRightPanelChange?.(next !== null)
                              return next
                            })
                          }
                        : undefined
                    }
                    onOpenDocument={msg.role === 'assistant' ? openDocumentPanel : undefined}
                    activePanelArtifactKey={documentPanelArtifact?.artifact.key ?? null}
                    onViewRunDetail={
                      showRunEvents && msg.role === 'assistant' && msg.run_id
                        ? () => setRunDetailPanelRunId(msg.run_id!)
                        : undefined
                    }
                    contentOverride={msg.role === 'assistant' && hasAssistantTurn ? '' : undefined}
                    plainTextForCopy={msg.role === 'assistant' && hasAssistantTurn ? assistantTurnPlainText(historicalTurn!) : undefined}
                  />
                  {/* 无痕分割线：固定在 fork 基点之后 */}
                  {locationState?.isIncognitoFork && locationState.forkBaseCount != null && idx === locationState.forkBaseCount - 1 && (
                    <IncognitoDivider text={t.incognitoForkDivider} />
                  )}
                  </div>
                )
              })}

              {/* 流式：正文 Markdown + COP 用 CopTimeline 点线 */}
              {liveAssistantTurn && liveAssistantTurn.segments.length > 0 && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 0, maxWidth: '663px' }}>
                  {/* pending thinking shimmer: Enter 后 thinking 内容到达前显示 */}
                  {pendingThinking && !liveTurnHasThinkingSegment(liveAssistantTurn) && (
                    <CopTimeline
                      key="pending-thinking"
                      steps={[]}
                      sources={[]}
                      isComplete={false}
                      live
                      shimmer
                      assistantThinking={{ markdown: '', live: true }}
                      thinkingStartedAt={copThinkingStartedAtMs}
                      accessToken={accessToken}
                      baseUrl={baseUrl}
                    />
                  )}
                  {liveAssistantTurn.segments.map((seg, si) => {
                    const lastSegIdx = liveAssistantTurn.segments.length - 1
                    const lastTurnSeg = liveAssistantTurn.segments[lastSegIdx]
                    const mdTypewriterDone =
                      !isStreaming ||
                      lastTurnSeg?.type !== 'text' ||
                      si !== lastSegIdx
                    const copClosedByFollowingSeg = si < lastSegIdx
                    const copTimelineComplete = !isStreaming || copClosedByFollowingSeg
                    const copTimelineLive = isStreaming && !copClosedByFollowingSeg

                    return seg.type === 'text' ? (
                      <LiveTurnMarkdown
                        key={`live-at-${si}`}
                        content={seg.content}
                        typewriterDone={mdTypewriterDone}
                        webSources={currentRunSourcesRef.current.length > 0 ? currentRunSourcesRef.current : undefined}
                        artifacts={currentRunArtifactsRef.current.length > 0 ? currentRunArtifactsRef.current : undefined}
                        accessToken={accessToken}
                        runId={activeRunId ?? undefined}
                        onOpenDocument={openDocumentPanel}
                        trimTrailingMargin={
                          liveAssistantTurn.segments[si + 1] == null ||
                          liveAssistantTurn.segments[si + 1]?.type === 'cop'
                        }
                      />
                    ) : (
                      (() => {
                        const payload = copTimelinePayloadForSegment(seg, {
                          codeExecutions: dedupedTopLevelCodeExecutions,
                          fileOps: topLevelFileOps,
                          webFetches: topLevelWebFetches,
                          subAgents: topLevelSubAgents,
                          searchSteps,
                          sources: currentRunSourcesRef.current,
                        })
                        const liveWidgets = liveStreamingWidgetEntriesForCop(seg, streamingArtifacts)
                        const liveArts = liveInlineArtifactEntriesForCop(seg, streamingArtifacts)
                        const thinkingRowsLive = !isSearchThread
                          ? thinkingRowsForCop(seg, {
                              live: isStreaming,
                              segmentIndex: si,
                              lastSegmentIndex: lastSegIdx,
                            })
                          : []
                        const copInlineLive = !isSearchThread
                          ? copInlineTextRowsForCop(seg, {
                              live: isStreaming,
                              segmentIndex: si,
                              lastSegmentIndex: lastSegIdx,
                            })
                          : []
                        if (
                          copSegmentCalls(seg).length === 0 &&
                          thinkingRowsLive.length === 0 &&
                          copInlineLive.length === 0 &&
                          liveWidgets.length === 0 &&
                          liveArts.length === 0
                        ) {
                          return null
                        }
                        const timelineTitleOverride = seg.title?.trim() || undefined
                        const trailSeg = si + 1 <= lastSegIdx ? liveAssistantTurn.segments[si + 1] : undefined
                        const trailingAssistantTextPresent =
                          trailSeg?.type === 'text' && trailSeg.content.length > 0
                        return (
                          <Fragment key={`live-acw-${si}`}>
                            <CopTimeline
                              steps={payload.steps}
                              sources={payload.sources}
                              isComplete={copTimelineComplete}
                              codeExecutions={payload.codeExecutions}
                              onOpenCodeExecution={openCodePanel}
                              activeCodeExecutionId={codePanelExecution?.id}
                              subAgents={payload.subAgents}
                              fileOps={payload.fileOps}
                              webFetches={payload.webFetches}
                              headerOverride={timelineTitleOverride}
                                  thinkingRows={thinkingRowsLive.length > 0 ? thinkingRowsLive : undefined}
                                  copInlineTextRows={copInlineLive.length > 0 ? copInlineLive : undefined}
                                  shimmer={copTimelineLive}
                                  live={copTimelineLive}
                                  thinkingStartedAt={copThinkingStartedAtMs}
                                  trailingAssistantTextPresent={trailingAssistantTextPresent}
                                  accessToken={accessToken}
                                  baseUrl={baseUrl}
                            />
                            {liveWidgets.map((entry) => (
                              <WidgetBlock
                                key={`live-w-${entry.toolCallId ?? entry.toolCallIndex}`}
                                html={entry.content ?? ''}
                                title={entry.title ?? 'Widget'}
                                complete={entry.complete}
                                loadingMessages={entry.loadingMessages}
                                onAction={handleArtifactAction}
                              />
                            ))}
                            {liveArts.map((entry) => (
                              <ArtifactStreamBlock
                                key={`live-art-${entry.toolCallId ?? entry.toolCallIndex}`}
                                entry={entry}
                                accessToken={accessToken}
                                onAction={handleArtifactAction}
                              />
                            ))}
                          </Fragment>
                        )
                      })()
                    )
                  })}
                </div>
              )}

              {allStreamItemsForUi.length > 0 && (
                <motion.div
                  className="cop-timeline-root"
                  initial={{ opacity: 0, y: 6 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ duration: 0.3, ease: 'easeOut' }}
                  style={{ maxWidth: '663px' }}
                >
                  <div
                    ref={copCodeExecScrollRef}
                    style={{
                      paddingLeft: COP_TIMELINE_CONTENT_PADDING_LEFT_PX,
                      paddingTop: '6px',
                      display: 'flex',
                      flexDirection: 'column',
                    }}
                  >
                    {allStreamItemsForUi.map((entry, idx) => {
                      const total = allStreamItemsForUi.length
                      const isFirst = idx === 0
                      const isLast = idx === total - 1
                      const multiItems = total >= 2
                      const dotTop =
                        entry.kind === 'code' && entry.item.language !== 'shell'
                          ? COP_TIMELINE_PYTHON_DOT_TOP
                          : COP_TIMELINE_DOT_TOP
                      const dotColor = entry.kind === 'code'
                        ? codeExecutionAccentColor(entry.item.status)
                        : entry.kind === 'agent'
                          ? entry.item.status === 'completed' ? 'var(--c-text-muted)' : entry.item.status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : 'var(--c-text-secondary)'
                          : entry.kind === 'fileop'
                            ? entry.item.status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : entry.item.status === 'running' ? 'var(--c-text-secondary)' : 'var(--c-text-muted)'
                            : entry.item.status === 'failed' ? 'var(--c-status-error-text, #ef4444)' : entry.item.status === 'fetching' ? 'var(--c-text-secondary)' : 'var(--c-text-muted)'
                      const isShell = entry.kind === 'code' && entry.item.language === 'shell'
                      return (
                        <motion.div
                          key={entry.id}
                          initial={{ opacity: 0, y: 6 }}
                          animate={{ opacity: 1, y: 0 }}
                          transition={{ duration: 0.25, ease: 'easeOut' }}
                        >
                          <CopTimelineUnifiedRow
                            isFirst={isFirst}
                            isLast={isLast}
                            multiItems={multiItems}
                            dotTop={dotTop}
                            dotColor={dotColor}
                          >
                            {entry.kind === 'code' && (isShell
                              ? <ExecutionCard variant="shell" code={entry.item.code} output={entry.item.output} status={entry.item.status} errorMessage={entry.item.errorMessage} smooth />
                              : <CodeExecutionCard language={entry.item.language} code={entry.item.code} output={entry.item.output} errorMessage={entry.item.errorMessage} status={entry.item.status} onOpen={() => openCodePanel(entry.item as CodeExecution)} isActive={codePanelExecution?.id === entry.item.id} />
                            )}
                            {entry.kind === 'agent' && (
                              <SubAgentBlock sourceTool={entry.item.sourceTool} nickname={entry.item.nickname} personaId={entry.item.personaId} input={entry.item.input} output={entry.item.output} status={entry.item.status} error={entry.item.error} live={isStreaming} currentRunId={entry.item.currentRunId} accessToken={accessToken} baseUrl={baseUrl} />
                            )}
                            {entry.kind === 'fileop' && (
                              <ExecutionCard variant="fileop" toolName={entry.item.toolName} label={entry.item.label} output={entry.item.output} status={entry.item.status} errorMessage={entry.item.errorMessage} smooth />
                            )}
                            {entry.kind === 'fetch' && <WebFetchItem fetch={entry.item} live />}
                          </CopTimelineUnifiedRow>
                        </motion.div>
                      )
                    })}
                  </div>
                </motion.div>
              )}

              {/* 无 live 助手块且无序列化顶层条时，用 CopTimeline 收束（与 allStreamItemsForUi 互斥） */}
              {!isStreaming &&
                liveAssistantTurn == null &&
                allStreamItemsForUi.length === 0 &&
                (dedupedTopLevelCodeExecutions.length > 0 || topLevelSubAgents.length > 0 || topLevelFileOps.length > 0 || topLevelWebFetches.length > 0) && (
                <div style={{ maxWidth: '663px' }}>
                  <CopTimeline
                    steps={[]}
                    sources={[]}
                    isComplete
                    codeExecutions={dedupedTopLevelCodeExecutions.length > 0 ? dedupedTopLevelCodeExecutions : undefined}
                    onOpenCodeExecution={openCodePanel}
                    activeCodeExecutionId={codePanelExecution?.id}
                    subAgents={topLevelSubAgents.length > 0 ? topLevelSubAgents : undefined}
                    fileOps={topLevelFileOps.length > 0 ? topLevelFileOps : undefined}
                    webFetches={topLevelWebFetches.length > 0 ? topLevelWebFetches : undefined}
                    accessToken={accessToken}
                    baseUrl={baseUrl}
                  />
                </div>
              )}

              {streamingArtifacts.filter((e) => e.toolName === 'show_widget' && (
                (e.content != null && e.content.length > 0) ||
                (e.loadingMessages != null && e.loadingMessages.length > 0)
              ) && (!e.toolCallId || !livePlacedShowWidgetCallIds.has(e.toolCallId))).map((entry) => (
                <WidgetBlock
                  key={`streaming-widget-${entry.toolCallIndex}`}
                  html={entry.content ?? ''}
                  title={entry.title ?? 'Widget'}
                  complete={entry.complete}
                  loadingMessages={entry.loadingMessages}
                  onAction={handleArtifactAction}
                />
              ))}

              {streamingArtifacts.filter((e) => e.toolName === 'create_artifact' && e.content && e.display !== 'panel' && (!e.toolCallId || !livePlacedCreateArtifactCallIds.has(e.toolCallId))).map((entry) => (
                <ArtifactStreamBlock
                  key={`streaming-artifact-${entry.toolCallIndex}`}
                  entry={entry}
                  accessToken={accessToken}
                  onAction={handleArtifactAction}
                />
              ))}

              {injectionBlocked && (
                <div className="max-w-[720px] rounded-xl border-[0.5px] border-[var(--c-error-border)] bg-[var(--c-error-bg)] px-4 py-3 text-sm text-[var(--c-error-text)]">
                  {injectionBlocked}
                </div>
              )}

              {awaitingInput && (
                <div
                  className="flex flex-col gap-2 rounded-xl px-4 py-3"
                  style={{ background: 'var(--c-bg-sub)', border: '0.5px solid var(--c-border-subtle)' }}
                >
                  <textarea
                    autoFocus
                    rows={3}
                    value={checkInDraft}
                    onChange={(e) => setCheckInDraft(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && !e.shiftKey) {
                        e.preventDefault()
                        void handleCheckInSubmit()
                      }
                    }}
                    disabled={checkInSubmitting}
                    className="w-full resize-none rounded-lg bg-transparent px-1 py-0.5 text-sm outline-none"
                    style={{ color: 'var(--c-text-primary)', caretColor: 'var(--c-text-primary)' }}
                    placeholder={t.checkInPlaceholder}
                  />
                  <div className="flex justify-end">
                    <button
                      type="button"
                      onClick={() => void handleCheckInSubmit()}
                      disabled={checkInSubmitting || !checkInDraft.trim()}
                      className="rounded-lg px-3 py-1 text-xs font-medium transition-opacity disabled:opacity-40"
                      style={{ background: 'var(--c-brand)', color: '#fff' }}
                    >
                      {checkInSubmitting ? '...' : 'Send'}
                    </button>
                  </div>
                </div>
              )}

              {terminalSseError && <ErrorCallout error={terminalSseError} />}

              {/* pendingIncognito：末尾展示分隔线，等待用户发送第一条消息 */}
              {pendingIncognito && (
                <IncognitoDivider
                  text={t.incognitoForkDivider}
                  onComplete={() => {
                    if (isAtBottomRef.current) {
                      bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
                    }
                  }}
                />
              )}

              <div ref={bottomRef} />
            </>
          )}
        </div>
      </div>

      {/* 输入区域 */}
      <div
        style={{ maxWidth: 1200, margin: '0 auto', padding: `12px ${appMode === 'claw' ? '14px' : isPanelOpen ? '32px' : '60px'} ${appMode === 'claw' ? '22px' : '8px'}`, position: 'absolute', bottom: 0, left: 0, right: 0, zIndex: 10, background: 'linear-gradient(to bottom, transparent 0%, var(--c-bg-page) 24px)' }}
        className="flex w-full flex-col items-center gap-2"
      >
        {/* 滚动到底部按钮：始终锚定在输入框顶边正上方 */}
        <button
          onClick={scrollToBottom}
          style={{
            position: 'absolute',
            top: 0,
            left: '50%',
            transform: 'translate(-50%, calc(-100% - 8px))',
            zIndex: 1,
            opacity: isAtBottom ? 0 : 1,
            pointerEvents: isAtBottom ? 'none' : 'auto',
            transition: 'opacity 200ms ease',
            width: 36,
            height: 36,
            borderRadius: '50%',
            border: '0.5px solid var(--c-border)',
            background: 'var(--c-bg-sidebar)',
            color: 'var(--c-text-secondary)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            cursor: 'pointer',
          }}
        >
          <ArrowDown size={16} className={isStreaming && !isAtBottom ? 'arrow-breathe' : ''} />
        </button>
        {queuedDraft && (
          <div
            className="flex w-full max-w-[756px] items-center gap-2 rounded-xl px-3 py-2"
            style={{ background: 'var(--c-bg-sub)', border: '0.5px solid var(--c-border-subtle)' }}
          >
            <span
              className="flex-1 truncate text-sm"
              style={{ color: 'var(--c-text-secondary)' }}
            >
              {queuedDraft}
            </span>
            <button
              type="button"
              onClick={() => { pendingMessageRef.current = null; setQueuedDraft(null) }}
              className="flex items-center justify-center rounded opacity-70 transition-opacity hover:opacity-100"
              style={{ color: 'var(--c-text-muted)' }}
            >
              <X size={12} />
            </button>
          </div>
        )}
        {pendingUserInput ? (
          <motion.div
            key="user-input-card"
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 8 }}
            transition={{ duration: 0.25, ease: 'easeOut' }}
            className="w-full max-w-[840px] px-4"
          >
            <UserInputCard
              key={pendingUserInput.request_id}
              request={pendingUserInput}
              onSubmit={handleUserInputSubmit}
              onDismiss={handleUserInputDismiss}
              disabled={!activeRunId}
            />
          </motion.div>
        ) : (
          <ChatInput
            value={draft}
            onChange={setDraft}
            onSubmit={handleSend}
            onCancel={handleCancel}
            placeholder={t.replyPlaceholder}
            disabled={sending}
            isStreaming={isStreaming}
            canCancel={canCancel}
            cancelSubmitting={cancelSubmitting}
            attachments={attachments}
            onAttachFiles={handleAttachFiles}
            onPasteContent={handlePasteContent}
            onRemoveAttachment={handleRemoveAttachment}
            accessToken={accessToken}
            onAsrError={handleAsrError}
            searchMode={isSearchThread}
            onPersonaChange={(personaKey) => setIsSearchThread(personaKey === SEARCH_PERSONA_KEY)}
            onOpenSettings={onOpenSettings}
            appMode={appMode}
            hasMessages={messages.length > 0}
            clawThreadId={threadId}
          />
        )}
        <p style={{ color: 'var(--c-text-muted)', fontSize: '11px', letterSpacing: '-0.3px', textAlign: 'center', marginBottom: 0, marginTop: '-2px' }}>
          Arkloop is AI and can make mistakes. Please double-check responses.
        </p>

        {error && (
          <div className="w-full max-w-[756px]">
            <ErrorCallout error={error} />
          </div>
        )}
      </div>

        </div>
        {/* 右侧面板：flex 兄弟节点；chat 模式下用 motion 驱动 width，避免嵌套 flex + CSS transition 偶发不插值 */}
        {appMode === 'claw' ? (
          <div
            style={{
              width: '300px',
              flexShrink: 0,
              overflow: 'hidden',
            }}
          >
            <ClawRightPanel
              accessToken={accessToken}
              projectId={currentThread?.project_id || undefined}
              steps={clawTodos.map((td) => ({
                id: td.id,
                label: td.content,
                status: td.status === 'completed' ? 'done' : td.status === 'in_progress' ? 'active' : 'pending',
              }))}
              onForbidden={() => onSetAppMode('chat')}
              readFiles={allReadFiles}
              threadId={threadId}
            />
          </div>
        ) : (
          <motion.div
            className="flex-shrink-0 overflow-hidden"
            initial={false}
            animate={{
              width: isDocumentPanelOpen
                ? documentPanelWidth
                : (isSourcePanelOpen || isCodePanelOpen)
                  ? sidePanelWidth
                  : 0,
            }}
            transition={{ duration: 0.28, ease: [0.16, 1, 0.3, 1] }}
            style={{
              borderLeft: (panelDisplaySources || codePanelDisplay || documentPanelDisplay)
                ? '0.5px solid var(--c-border-subtle)'
                : 'none',
            }}
          >
            {isSourcePanelOpen && panelDisplaySources && panelDisplaySources.length > 0 && (
              <div style={{ width: `${sidePanelWidth}px`, height: '100%', contain: 'layout style' }}>
                <SourcesPanel
                  sources={panelDisplaySources}
                  userQuery={panelDisplayQuery}
                  onClose={() => { setSourcePanelMessageId(null); onRightPanelChange?.(false) }}
                />
              </div>
            )}
            {isCodePanelOpen && codePanelDisplay && (
              <div style={{ width: `${sidePanelWidth}px`, height: '100%', contain: 'layout style' }}>
                <CodeExecutionPanel
                  execution={codePanelDisplay}
                  onClose={() => { setCodePanelExecution(null); onRightPanelChange?.(false) }}
                />
              </div>
            )}
            {isDocumentPanelOpen && documentPanelDisplay && (
              <div style={{ width: `${documentPanelWidth}px`, height: '100%', contain: 'layout style' }}>
                <DocumentPanel
                  artifact={documentPanelDisplay.artifact}
                  artifacts={documentPanelDisplay.artifacts}
                  accessToken={accessToken}
                  runId={documentPanelDisplay.runId}
                  onClose={() => {
                    stabilizeDocumentPanelScroll()
                    setDocumentPanelArtifact(null)
                    onRightPanelChange?.(false)
                  }}
                />
              </div>
            )}
          </motion.div>
        )}
      </div>

      {threadId && (
        <ShareModal
          accessToken={accessToken}
          threadId={threadId}
          open={shareModalOpen}
          onClose={() => setShareModalOpen(false)}
        />
      )}

      {/* 标题下拉菜单 */}
      {titleMenuOpen && threadId && createPortal(
        <div
          ref={titleMenuRef}
          className="dropdown-menu"
          style={{
            position: 'fixed',
            right: `calc(100vw - ${titleMenuPos.x}px)`,
            top: titleMenuPos.y,
            zIndex: 9999,
            border: '0.5px solid var(--c-border-subtle)',
            borderRadius: '10px',
            padding: '4px',
            background: 'var(--c-bg-menu)',
            minWidth: '140px',
            boxShadow: 'var(--c-dropdown-shadow)',
          }}
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
            <button
              onClick={startRename}
              className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
            >
              <Pencil size={13} style={{ flexShrink: 0 }} />
              {t.renameThread}
            </button>
            <button
              onClick={toggleStar}
              className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
            >
              <Star
                size={13}
                style={{
                  flexShrink: 0,
                  fill: starredIds.includes(threadId) ? 'var(--c-text-secondary)' : 'none',
                }}
              />
              {starredIds.includes(threadId) ? t.unstarThread : t.starThread}
            </button>
            {!isDesktop() && !privateThreadIds.has(threadId) && (
              <button
                onClick={handleShareFromMenu}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
              >
                <Share2 size={13} style={{ flexShrink: 0 }} />
                {t.shareThread}
              </button>
            )}
            <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 0' }} />
            <button
              onClick={confirmDelete}
              className="flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-[13px] text-[#ef4444] hover:bg-[rgba(239,68,68,0.08)] hover:text-[#f87171]"
            >
              <Trash2 size={13} style={{ flexShrink: 0 }} />
              {t.deleteThread}
            </button>
          </div>
        </div>,
        document.body,
      )}

      {/* 删除确认弹窗 */}
      {deleteConfirmOpen && createPortal(
        <div
          className="overlay-fade-in fixed inset-0 flex items-center justify-center"
          style={{ zIndex: 10000, background: 'rgba(0,0,0,0.12)', backdropFilter: 'blur(2px)', WebkitBackdropFilter: 'blur(2px)' }}
          onClick={(e) => { if (e.target === e.currentTarget) setDeleteConfirmOpen(false) }}
        >
          <div
            className="modal-enter"
            style={{
              background: 'var(--c-bg-page)',
              border: '0.5px solid var(--c-border-subtle)',
              borderRadius: '16px',
              padding: '24px',
              width: '320px',
              boxShadow: 'var(--c-dropdown-shadow)',
            }}
          >
            <p style={{ fontSize: '15px', fontWeight: 600, color: 'var(--c-text-primary)', marginBottom: '8px' }}>
              {t.deleteThreadConfirmTitle}
            </p>
            <p style={{ fontSize: '13px', color: 'var(--c-text-secondary)', lineHeight: 1.55, marginBottom: '20px' }}>
              {t.deleteThreadConfirmBody}
            </p>
            <div style={{ display: 'flex', gap: '8px', justifyContent: 'flex-end' }}>
              <button
                onClick={() => setDeleteConfirmOpen(false)}
                className="hover:bg-[var(--c-bg-deep)]"
                style={{
                  padding: '7px 16px',
                  borderRadius: '8px',
                  fontSize: '13px',
                  fontWeight: 500,
                  color: 'var(--c-text-secondary)',
                  background: 'transparent',
                  border: '0.5px solid var(--c-border-subtle)',
                  cursor: 'pointer',
                }}
              >
                {t.deleteThreadCancel}
              </button>
              <button
                onClick={handleDeleteThread}
                className="hover:opacity-85 active:opacity-70"
                style={{
                  padding: '7px 16px',
                  borderRadius: '8px',
                  fontSize: '13px',
                  fontWeight: 500,
                  color: '#fff',
                  background: '#ef4444',
                  border: 'none',
                  cursor: 'pointer',
                }}
              >
                {t.deleteThreadConfirm}
              </button>
            </div>
          </div>
        </div>,
        document.body,
      )}

      {runDetailPanelRunId && (
        <RunDetailPanel
          runId={runDetailPanelRunId}
          accessToken={accessToken}
          onClose={() => setRunDetailPanelRunId(null)}
        />
      )}
    </div>
  )
}

function ContextCompactBar({
  variant,
  runningLabel,
  doneLabel,
  trimLabel,
  llmFailedLabel,
}: {
  variant: { type: 'persist'; status: 'running' | 'done' | 'llm_failed' } | { type: 'trim'; status: 'done'; dropped: number }
  runningLabel: string
  doneLabel: string
  trimLabel: string
  llmFailedLabel: string
}) {
  return (
    <motion.div
      initial={{ opacity: 0, height: 0 }}
      animate={{ opacity: 1, height: 'auto' }}
      transition={{ duration: 0.28, ease: 'easeOut' }}
      style={{ overflow: 'hidden' }}
    >
      <div className="flex items-center gap-3 py-1">
        <div className="h-px flex-1 bg-[var(--c-border-subtle)]" />
        <span className="flex items-center gap-1.5 text-xs text-[var(--c-text-muted)]">
          {variant.type === 'persist' && variant.status === 'running' ? (
            <Loader2 size={12} strokeWidth={1.5} className="shrink-0 animate-spin opacity-80" />
          ) : variant.type === 'persist' && variant.status === 'llm_failed' ? (
            <AlertCircle size={12} strokeWidth={1.5} className="shrink-0 opacity-80 text-[var(--c-status-warning)]" />
          ) : (
            <Check size={12} strokeWidth={1.5} className="shrink-0 opacity-80" />
          )}
          {variant.type === 'persist'
            ? variant.status === 'running'
              ? runningLabel
              : variant.status === 'llm_failed'
                ? llmFailedLabel
                : doneLabel
            : trimLabel.replace('{n}', String(variant.dropped))}
        </span>
        <div className="h-px flex-1 bg-[var(--c-border-subtle)]" />
      </div>
    </motion.div>
  )
}

function IncognitoDivider({ text, onComplete }: { text: string; onComplete?: () => void }) {
  return (
    <motion.div
      initial={{ opacity: 0, height: 0 }}
      animate={{ opacity: 1, height: 'auto' }}
      transition={{ duration: 0.3, ease: 'easeOut' }}
      style={{ overflow: 'hidden' }}
      onAnimationComplete={onComplete}
    >
      <div className="flex items-center gap-3 py-1 mt-6">
        <div className="h-px flex-1" style={{ background: 'var(--c-border-subtle)' }} />
        <span className="flex items-center gap-1.5 text-xs" style={{ color: 'var(--c-text-muted)' }}>
          <Glasses size={12} strokeWidth={1.5} style={{ opacity: 0.7 }} />
          {text}
        </span>
        <div className="h-px flex-1" style={{ background: 'var(--c-border-subtle)' }} />
      </div>
    </motion.div>
  )
}
