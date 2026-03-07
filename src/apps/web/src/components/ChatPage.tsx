import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { useParams, useLocation, useOutletContext, useNavigate } from 'react-router-dom'
import { motion } from 'framer-motion'
import { Glasses, Loader2, Paperclip, Share2, X, Zap } from 'lucide-react'
import { ChatInput, type Attachment, formatFileSize } from './ChatInput'
import { MessageBubble, StreamingBubble } from './MessageBubble'
import { ThinkingBlock, CodeExecutionCard, type CodeExecution } from './ThinkingBlock'
import { ShellExecutionBlock } from './ShellExecutionBlock'
import { SearchTimeline, type SearchStep } from './SearchTimeline'
import { resolveMessageSourcesForRender } from './chatSourceResolver'
import { ErrorCallout, type AppError } from './ErrorCallout'
import { ShareModal } from './ShareModal'
import { ReportModal } from './ReportModal'
import { NotificationBell } from './NotificationBell'
import { SourcesPanel } from './SourcesPanel'
import { CodeExecutionPanel } from './CodeExecutionPanel'
import { DocumentPanel } from './DocumentPanel'
import { useSSE } from '../hooks/useSSE'
import { SSEApiError } from '../sse'
import {
  applyCodeExecutionToolCall,
  applyCodeExecutionToolResult,
  buildMessageCodeExecutionsFromRunEvents,
  patchCodeExecutionList,
  buildMessageThinkingFromRunEvents,
  selectFreshRunEvents,
  shouldReplayMessageCodeExecutions,
} from '../runEventProcessing'
import { useLocale } from '../contexts/LocaleContext'
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
  isApiError,
  type MessageResponse,
  type ThreadResponse,
} from '../api'
import {
  addSearchThreadId,
  SEARCH_PERSONA_KEY,
  isSearchThreadId,
  readMessageSources,
  writeMessageSources,
  readMessageArtifacts,
  writeMessageArtifacts,
  readMessageCodeExecutions,
  writeMessageCodeExecutions,
  readMessageThinking,
  writeMessageThinking,
  readMessageSearchSteps,
  writeMessageSearchSteps,
  type WebSource,
  type ArtifactRef,
  type CodeExecutionRef,
  type MessageThinkingRef,
  type MessageSearchStepRef,
  migrateMessageMetadata,
} from '../storage'

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
}

type LocationState = { initialRunId?: string; isSearch?: boolean; isIncognitoFork?: boolean; forkBaseCount?: number } | null

const SHOW_EXPLICIT_THINKING = false

const DEFAULT_SEARCH_PLANNING_LABEL = 'Planning'
const SEARCH_PLANNING_LABEL_MAX_LEN = 60
const SEARCH_PLANNING_TOOL_NAME = 'timeline_title'

function compactSingleLine(raw: string | undefined, maxLen = SEARCH_PLANNING_LABEL_MAX_LEN): string {
  const withoutFiles = (raw ?? '').replace(/<file[\s\S]*?<\/file>/g, ' ')
  const text = withoutFiles.replace(/\s+/g, ' ').trim()
  if (!text) return ''
  if (text.length <= maxLen) return text
  if (maxLen <= 3) return text.slice(0, maxLen)
  return text.slice(0, maxLen - 3).trimEnd() + '...'
}

function patchLegacySearchSteps(
  steps: MessageSearchStepRef[],
): { steps: MessageSearchStepRef[]; changed: boolean } {
  const idx = steps.findIndex((s) => s.kind === 'planning')
  if (idx < 0) return { steps, changed: false }
  const planning = steps[idx]
  // 旧版本的 planning 步骤 id 形如 `plan-${callId}`，且 label 是前端占位符。
  // 这里不做内容匹配，只按结构做一次性迁移。
  if (!planning.id.startsWith('plan-')) return { steps, changed: false }

  const firstSearchingQuery =
    steps.find((s) => s.kind === 'searching' && Array.isArray(s.queries) && s.queries.length > 0)?.queries?.[0]

  const nextLabel = compactSingleLine(firstSearchingQuery) || DEFAULT_SEARCH_PLANNING_LABEL
  if (planning.label === nextLabel) return { steps, changed: false }
  const patched = steps.map((s, i) => (i === idx ? { ...s, label: nextLabel } : s))
  return { steps: patched, changed: true }
}

function buildHistoricalSearchSteps(userQuery?: string): SearchStep[] {
  const query = compactSingleLine(userQuery)
  return [
    { id: 'history-plan', kind: 'planning', label: DEFAULT_SEARCH_PLANNING_LABEL, status: 'done' },
    {
      id: 'history-search',
      kind: 'searching',
      label: 'Searching',
      status: 'done',
      queries: query ? [query] : undefined,
    },
    { id: 'history-reviewing', kind: 'reviewing', label: 'Reviewing', status: 'done' },
    { id: 'history-finished', kind: 'finished', label: 'Finished', status: 'done' },
  ]
}

function finalizeSearchSteps(steps: SearchStep[]): MessageSearchStepRef[] {
  if (steps.length === 0) return []
  const normalized: MessageSearchStepRef[] = steps.map((step) => ({
    id: step.id,
    kind: step.kind,
    label: step.label,
    status: 'done',
    queries: step.queries ? [...step.queries] : undefined,
  }))
  const hasSearch = normalized.some((step) => step.kind === 'searching')
  if (hasSearch && !normalized.some((step) => step.kind === 'reviewing')) {
    normalized.push({ id: 'reviewing', kind: 'reviewing', label: 'Reviewing', status: 'done' })
  }
  if (!normalized.some((step) => step.kind === 'finished')) {
    normalized.push({ id: 'finished', kind: 'finished', label: 'Finished', status: 'done' })
  }
  return normalized
}

export function ChatPage() {
  const { accessToken, onLoggedOut, onRunStarted, onRunEnded, onThreadCreated, onThreadTitleUpdated, refreshCredits, onOpenNotifications, notificationVersion, creditsBalance, onTogglePrivateMode, privateThreadIds, onSetPendingIncognito, onRightPanelChange } = useOutletContext<OutletContext>()
  const { threadId } = useParams<{ threadId: string }>()
  const location = useLocation()
  const locationState = location.state as LocationState
  const navigate = useNavigate()
  const { t } = useLocale()

  const baseUrl = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? ''

  const [isSearchThread, setIsSearchThread] = useState(
    () => locationState?.isSearch === true || isSearchThreadId(threadId ?? ''),
  )

  const [messages, setMessages] = useState<MessageResponse[]>([])
  const [messagesLoading, setMessagesLoading] = useState(false)
  const [draft, setDraft] = useState('')
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const [assistantDraft, setAssistantDraft] = useState('')
  const [activeRunId, setActiveRunId] = useState<string | null>(
    locationState?.initialRunId ?? null,
  )
  const [sending, setSending] = useState(false)
  const [cancelSubmitting, setCancelSubmitting] = useState(false)
  const [error, setError] = useState<AppError | null>(null)
  const [queuedDraft, setQueuedDraft] = useState<string | null>(null)
  const [awaitingInput, setAwaitingInput] = useState(false)
  const [checkInDraft, setCheckInDraft] = useState('')
  const [checkInSubmitting, setCheckInSubmitting] = useState(false)
  const [shareModalOpen, setShareModalOpen] = useState(false)
  const [reportModalOpen, setReportModalOpen] = useState(false)
  const [sharingMessageId, setSharingMessageId] = useState<string | null>(null)
  const [sharedMessageId, setSharedMessageId] = useState<string | null>(null)
  const [pendingIncognito, setPendingIncognito] = useState(false)

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
  // 思考过程缓存：messageId -> thinking snapshot
  const [, setMessageThinkingMap] = useState<Map<string, MessageThinkingRef>>(new Map())
  // Search 时间轴缓存：messageId -> steps
  const [messageSearchStepsMap, setMessageSearchStepsMap] = useState<Map<string, MessageSearchStepRef[]>>(new Map())
  // sources 侧边面板：显示哪条消息的来源
  const [sourcePanelMessageId, setSourcePanelMessageId] = useState<string | null>(null)
  // 代码执行侧边面板
  const [codePanelExecution, setCodePanelExecution] = useState<CodeExecution | null>(null)
  const lastCodePanelRef = useRef<CodeExecution | null>(null)
  // 文档预览侧边面板
  const [documentPanelArtifact, setDocumentPanelArtifact] = useState<ArtifactRef | null>(null)
  const lastDocumentPanelRef = useRef<ArtifactRef | null>(null)
  // 关闭动画期间保留上一次的数据
  const lastPanelSourcesRef = useRef<WebSource[] | undefined>(undefined)
  const lastPanelQueryRef = useRef<string | undefined>(undefined)
  // segment 状态：用于渲染 Agent 规划轮折叠块
  type Segment = { segmentId: string; kind: string; mode: string; label: string; content: string; isStreaming: boolean; codeExecutions: CodeExecution[] }
  const [segments, setSegments] = useState<Segment[]>([])
  const activeSegmentIdRef = useRef<string | null>(null)
  const segmentsRef = useRef<Segment[]>([])
  // Pro 路径的 LLM 原生 thinking 内容（channel: "thinking"）
  const [thinkingDraft, setThinkingDraft] = useState('')
  const thinkingDraftRef = useRef('')
  // segment 外的顶层代码执行（Ultra/Pro 模式，无 segment 包裹）
  const [topLevelCodeExecutions, setTopLevelCodeExecutions] = useState<CodeExecution[]>([])
  // Search 模式时间轴步骤（run 结束后保持，下次 run 开始时清除）
  const [searchSteps, setSearchSteps] = useState<SearchStep[]>([])
  const searchStepsRef = useRef<SearchStep[]>([])
  const [liveTimelineExiting, setLiveTimelineExiting] = useState(false)
  const liveTimelineExitTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const applySearchSteps = useCallback((updater: (prev: SearchStep[]) => SearchStep[]) => {
    setSearchSteps((prev) => {
      const next = updater(prev)
      searchStepsRef.current = next
      return next
    })
  }, [])
  const resetSearchSteps = useCallback(() => {
    searchStepsRef.current = []
    setSearchSteps([])
  }, [])

  const bottomRef = useRef<HTMLDivElement>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const copCodeExecScrollRef = useRef<HTMLDivElement>(null)
  const lastUserMsgRef = useRef<HTMLDivElement>(null)
  const wasLoadingRef = useRef(false)
  const processedEventCountRef = useRef(0)
  const pendingMessageRef = useRef<string | null>(null)
  // 仅在当前 run 的 SSE 确认进入过连接态后，才允许触发终端兜底。
  const sseTerminalFallbackRunIdRef = useRef<string | null>(null)
  const sseTerminalFallbackArmedRef = useRef(false)
  // 用户是否停留在底部区域（距底部 80px 以内视为"在底部"）
  const isAtBottomRef = useRef(true)

  useEffect(() => {
    segmentsRef.current = segments
  }, [segments])

  useEffect(() => {
    thinkingDraftRef.current = thinkingDraft
  }, [thinkingDraft])

  const handleScrollContainerScroll = useCallback(() => {
    const el = scrollContainerRef.current
    if (!el) return
    isAtBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight <= 80
  }, [])

  const buildLiveThinkingSnapshot = useCallback((): MessageThinkingRef | null => {
    const liveSegments = segmentsRef.current
      .filter((s) => s.mode !== 'hidden' && s.content.trim() !== '')
      .map((s) => ({
        segmentId: s.segmentId,
        kind: s.kind,
        mode: s.mode,
        label: s.label,
        content: s.content,
      }))
    const liveThinking = thinkingDraftRef.current
    if (liveSegments.length === 0 && liveThinking.trim() === '') {
      return null
    }
    return {
      thinkingText: liveThinking,
      segments: liveSegments,
    }
  }, [])

  const sse = useSSE({ runId: activeRunId ?? '', accessToken, baseUrl })

  const isStreaming = activeRunId != null
  const canCancel =
    activeRunId != null &&
    (sse.state === 'connecting' || sse.state === 'connected' || sse.state === 'reconnecting')

  const refreshMessages = useCallback(async (): Promise<MessageResponse[]> => {
    if (!threadId) return []
    try {
      const items = await listMessages(accessToken, threadId)
      setMessages(items)
      return items
    } catch (err) {
      setError(normalizeError(err))
      return []
    }
  }, [accessToken, threadId])

  // 仅用于 streaming 结束后自动发送排队消息（无附件）
  const sendMessage = useCallback(async (text: string) => {
    if (!threadId) return
    setSending(true)
    setError(null)
    try {
      const message = await createMessage(accessToken, threadId, { content: text })
      setMessages((prev) => [...prev, message])
      setAssistantDraft('')
      const run = await createRun(accessToken, threadId)
      setActiveRunId(run.run_id)
      onRunStarted(threadId)
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    } finally {
      setSending(false)
    }
  }, [accessToken, threadId, onLoggedOut, onRunStarted])

  // 用 ref 持有最新的 sendMessage，避免 SSE 事件闭包中捕获旧引用
  const sendMessageRef = useRef(sendMessage)
  useEffect(() => { sendMessageRef.current = sendMessage }, [sendMessage])

  // 加载 thread 数据
  useEffect(() => {
    if (!threadId) return

    setMessagesLoading(true)
    setError(null)
    setAssistantDraft('')

    void (async () => {
      try {
        const [items, runs] = await Promise.all([
          listMessages(accessToken, threadId),
          listThreadRuns(accessToken, threadId, 1),
        ])
        setMessages(items)

        // 加载各消息缓存的 web 来源
        const sourcesMap = new Map<string, WebSource[]>()
        const artifactsMap = new Map<string, ArtifactRef[]>()
        const codeExecMap = new Map<string, CodeExecutionRef[]>()
        const thinkingMap = new Map<string, MessageThinkingRef>()
        const searchStepsMap = new Map<string, MessageSearchStepRef[]>()
        for (const msg of items) {
          if (msg.role !== 'assistant') continue

          const cached = readMessageSources(msg.id)
          if (cached) sourcesMap.set(msg.id, cached)
          const cachedArt = readMessageArtifacts(msg.id)
          if (cachedArt) artifactsMap.set(msg.id, cachedArt)
          const cachedExec = readMessageCodeExecutions(msg.id)
          if (cachedExec) codeExecMap.set(msg.id, cachedExec)
          const cachedThinking = readMessageThinking(msg.id)
          if (cachedThinking) thinkingMap.set(msg.id, cachedThinking)
          const cachedSearchSteps = readMessageSearchSteps(msg.id)
          if (cachedSearchSteps) {
            const patched = patchLegacySearchSteps(cachedSearchSteps)
            if (patched.changed) writeMessageSearchSteps(msg.id, patched.steps)
            searchStepsMap.set(msg.id, patched.steps)
          }
        }

        // 服务端回放：补齐最新一轮的 thinking / 代码执行缓存
        const latest = runs[0]
        const lastAssistant = [...items].reverse().find((m) => m.role === 'assistant')
        const replayThinkingNeeded = !!(lastAssistant && !thinkingMap.has(lastAssistant.id))
        const replayCodeExecNeeded = !!(lastAssistant && shouldReplayMessageCodeExecutions(codeExecMap.get(lastAssistant.id)))
        if (latest && latest.status !== 'running' && lastAssistant && (replayThinkingNeeded || replayCodeExecNeeded)) {
          try {
            const replayEvents = await listRunEvents(accessToken, latest.run_id, { follow: false })
            if (replayThinkingNeeded) {
              const thinking = buildMessageThinkingFromRunEvents(replayEvents)
              if (thinking) {
                thinkingMap.set(lastAssistant.id, thinking)
                writeMessageThinking(lastAssistant.id, thinking)
              }
            }
            if (replayCodeExecNeeded) {
              const replayExecs = buildMessageCodeExecutionsFromRunEvents(replayEvents)
              codeExecMap.set(lastAssistant.id, replayExecs)
              writeMessageCodeExecutions(lastAssistant.id, replayExecs)
            }
          } catch {
            // 回放失败不影响主流程
          }
        }

        setMessageSourcesMap(sourcesMap)
        setMessageArtifactsMap(artifactsMap)
        setMessageCodeExecutionsMap(codeExecMap)
        setMessageThinkingMap(thinkingMap)
        setMessageSearchStepsMap(searchStepsMap)

        // 若 location state 已提供 initialRunId，优先使用（来自 WelcomePage 新建后导航）
        // 必须显式调用 setActiveRunId，因为 React Router 复用组件实例，useState 初始值不会重新求值
        if (locationState?.initialRunId) {
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
        setMessagesLoading(false)
      }
    })()
  // 只在 threadId 变化时重新加载，避免依赖 locationState 导致重复触发
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accessToken, threadId])

  // 切换 thread 时清理 SSE 和排队消息，并重置 pendingIncognito
  useEffect(() => {
    setActiveRunId(null)
    setAssistantDraft('')
    setSegments([])
    activeSegmentIdRef.current = null
    setThinkingDraft('')
    setTopLevelCodeExecutions([])
    resetSearchSteps()
    setCancelSubmitting(false)
    setAwaitingInput(false)
    setCheckInDraft('')
    pendingMessageRef.current = null
    setQueuedDraft(null)
    currentRunSourcesRef.current = []
    currentRunArtifactsRef.current = []
    currentRunCodeExecutionsRef.current = []
    setMessageSourcesMap(new Map())
    setMessageArtifactsMap(new Map())
    setMessageCodeExecutionsMap(new Map())
    setMessageThinkingMap(new Map())
    setMessageSearchStepsMap(new Map())
    setSourcePanelMessageId(null)
    sse.disconnect()
    sse.clearEvents()
    // 不重置 processedEventCountRef: clearEvents 是异步的，若此处归零，
    // 同一 effects 阶段内事件处理 effect 会重放旧事件导致 thinkingDraft 串线。
    // activeRunId effect 在新 run 启动时负责归零。
    setPendingIncognito(false)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [threadId])

  // 同步 pendingIncognito 到 AppLayout（用于 Sidebar 无痕 UI）
  useEffect(() => {
    onSetPendingIncognito(pendingIncognito)
    return () => { onSetPendingIncognito(false) }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pendingIncognito])

  // 连接 SSE
  useEffect(() => {
    if (!activeRunId) return
    sseTerminalFallbackRunIdRef.current = activeRunId
    sseTerminalFallbackArmedRef.current = false
    sse.reset()
    sse.connect()
    processedEventCountRef.current = 0
    currentRunSourcesRef.current = []
    currentRunArtifactsRef.current = []
    currentRunCodeExecutionsRef.current = []
    setAssistantDraft('')
    setSegments([])
    activeSegmentIdRef.current = null
    setThinkingDraft('')
    setTopLevelCodeExecutions([])
    resetSearchSteps()
    setCancelSubmitting(false)
    return () => { sse.disconnect() }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeRunId])

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
    const { fresh, nextProcessedCount } = selectFreshRunEvents({
      events: sse.events,
      activeRunId,
      processedCount: processedEventCountRef.current,
    })
    processedEventCountRef.current = nextProcessedCount

    for (const event of fresh) {
      if (event.type === 'run.segment.start') {
        const obj = event.data as { segment_id?: unknown; kind?: unknown; display?: unknown }
        const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
        const kind = typeof obj.kind === 'string' ? obj.kind : 'planning_round'
        const display = (obj.display ?? {}) as { mode?: unknown; label?: unknown; queries?: unknown }
        const mode = typeof display.mode === 'string' ? display.mode : 'collapsed'
        const label = typeof display.label === 'string' ? display.label : ''
        if (!segmentId) continue
        activeSegmentIdRef.current = segmentId

        // search_* kind 路由到 searchSteps（所有模式均支持）
        if (kind.startsWith('search_')) {
          const searchKind = kind === 'search_planning' ? 'planning'
            : kind === 'search_queries' ? 'searching'
            : kind === 'search_reviewing' ? 'reviewing'
            : 'planning'
          const queries = Array.isArray(display.queries)
            ? (display.queries as unknown[]).filter((q): q is string => typeof q === 'string')
            : undefined
          applySearchSteps((prev) => [...prev, {
            id: segmentId,
            kind: searchKind as SearchStep['kind'],
            label,
            status: 'active',
            queries,
          }])
        } else {
          setSegments((prev) => [...prev, { segmentId, kind, mode, label, content: '', isStreaming: true, codeExecutions: [] }])
        }
        continue
      }

      if (event.type === 'run.segment.end') {
        const obj = event.data as { segment_id?: unknown }
        const segmentId = typeof obj.segment_id === 'string' ? obj.segment_id : ''
        if (segmentId && activeSegmentIdRef.current === segmentId) {
          activeSegmentIdRef.current = null
        }
        setSegments((prev) =>
          prev.map((s) => (s.segmentId === segmentId ? { ...s, isStreaming: false } : s)),
        )
        applySearchSteps((prev) =>
          prev.map((s) => (s.id === segmentId ? { ...s, status: 'done' as const } : s)),
        )
        continue
      }

      if (event.type === 'message.delta') {
        const obj = event.data as { content_delta?: unknown; role?: unknown; channel?: unknown }
        if (obj.role != null && obj.role !== 'assistant') continue
        if (typeof obj.content_delta !== 'string' || !obj.content_delta) continue
        const delta = obj.content_delta
        const isThinking = obj.channel === 'thinking'
        const activeSeg = activeSegmentIdRef.current
        if (activeSeg) {
          // segment 内：主内容和 thinking 都属于该规划轮，追加到 segment buffer
          setSegments((prev) =>
            prev.map((s) =>
              s.segmentId === activeSeg && s.mode !== 'hidden'
                ? { ...s, content: s.content + delta }
                : s,
            ),
          )
        } else if (isThinking) {
          setThinkingDraft((prev) => prev + delta)
        } else {
          setAssistantDraft((prev) => prev + delta)
        }
        continue
      }

      if (event.type === 'tool.call') {
        const obj = event.data as { tool_name?: unknown; llm_name?: unknown; tool_call_id?: unknown; arguments?: unknown }
        const toolName = typeof obj.tool_name === 'string' ? obj.tool_name : event.tool_name
        const llmName = typeof obj.llm_name === 'string' ? obj.llm_name : undefined
        const codeExecutionCall = applyCodeExecutionToolCall(currentRunCodeExecutionsRef.current, event)
        if (codeExecutionCall.appended) {
          const entry: CodeExecution = codeExecutionCall.appended
          currentRunCodeExecutionsRef.current = codeExecutionCall.nextExecutions
          const activeSeg = activeSegmentIdRef.current
          // 搜索段内的代码执行路由到 topLevel，由 SearchTimeline 统一渲染
          const isSearchSeg = activeSeg && searchStepsRef.current.some((s) => s.id === activeSeg)
          if (SHOW_EXPLICIT_THINKING && activeSeg && !isSearchSeg) {
            setSegments((prev) =>
              prev.map((s) =>
                s.segmentId === activeSeg
                  ? { ...s, codeExecutions: [...s.codeExecutions, entry] }
                  : s,
              ),
            )
          } else {
            setTopLevelCodeExecutions((prev) => [...prev, entry])
          }
        }
        // 搜索模式：模型输出的 planning 小标题
        if (toolName === SEARCH_PLANNING_TOOL_NAME) {
          const args = obj.arguments as Record<string, unknown> | undefined
          const rawLabel = typeof args?.label === 'string' ? args.label : undefined
          const label = compactSingleLine(rawLabel) || DEFAULT_SEARCH_PLANNING_LABEL
          applySearchSteps((prev) => {
            const idx = prev.findIndex((s) => s.kind === 'planning')
            if (idx >= 0) {
              const next = [...prev]
              next[idx] = { ...next[idx], label, status: 'done' }
              return next
            }
            return [{ id: 'planning', kind: 'planning', label, status: 'done' }, ...prev]
          })
          continue
        }
        // web_search tool.call 驱动 SearchTimeline（所有模式均支持）
        if (toolName === 'web_search' || llmName === 'web_search') {
          const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : event.event_id
          const args = obj.arguments as Record<string, unknown> | undefined
          const query = typeof args?.query === 'string' ? args.query : undefined
          const queries = Array.isArray(args?.queries)
            ? (args.queries as unknown[]).filter((q): q is string => typeof q === 'string' && q.trim().length > 0)
            : undefined
          const displayQueries = queries && queries.length > 0
            ? queries
            : query
              ? [query]
              : undefined
          // 不插入兜底 planning，直接添加 searching 步骤
          applySearchSteps((prev) => {
            return [...prev, {
              id: callId,
              kind: 'searching' as const,
              label: 'Searching',
              status: 'active' as const,
              queries: displayQueries,
            }]
          })
        }
        continue
      }

      if (event.type === 'tool.result') {
        const obj = event.data as { tool_name?: unknown; tool_call_id?: unknown; result?: unknown }
        const resultToolName = typeof obj.tool_name === 'string' ? obj.tool_name : ''
        if (resultToolName === 'web_search' || resultToolName.startsWith('web_search.')) {
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
          // 标记 searching 步骤完成
          const callId = typeof obj.tool_call_id === 'string' ? obj.tool_call_id : undefined
          if (callId) {
            applySearchSteps((prev) =>
              prev.map((s) => s.id === callId ? { ...s, status: 'done' as const } : s),
            )
          }
        }
        // 检测 sandbox 执行产物 + document_write 产物
        if (obj.tool_name === 'python_execute' || obj.tool_name === 'exec_command' || obj.tool_name === 'write_stdin' || obj.tool_name === 'document_write') {
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
              }))
            if (newArtifacts.length > 0) {
              currentRunArtifactsRef.current = [...currentRunArtifactsRef.current, ...newArtifacts]
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
        setAwaitingInput(true)
        continue
      }

      if (event.type === 'run.completed') {
        const runThinking = buildLiveThinkingSnapshot()
        sse.disconnect()
        setActiveRunId(null)
        setAssistantDraft('')
        setThinkingDraft('')
        setTopLevelCodeExecutions([])
        setSegments([])
        activeSegmentIdRef.current = null
        const runSearchSteps = finalizeSearchSteps(searchStepsRef.current)
        if (runSearchSteps.length > 0) applySearchSteps(() => runSearchSteps)
        // 让 live SearchTimeline 平滑收起而非瞬间消失
        if (searchStepsRef.current.length > 0) {
          setLiveTimelineExiting(true)
          clearTimeout(liveTimelineExitTimerRef.current)
          liveTimelineExitTimerRef.current = setTimeout(() => setLiveTimelineExiting(false), 500)
        }
        setQueuedDraft(null)
        setAwaitingInput(false)
        setCheckInDraft('')
        if (threadId) onRunEnded(threadId)
        refreshCredits()
        const runSources = [...currentRunSourcesRef.current]
        // 不清除 currentRunSourcesRef —— SearchTimeline 完成后仍需读取
        // 下次 run 开始时 SSE connect effect 自动清除
        const runArtifacts = [...currentRunArtifactsRef.current]
        currentRunArtifactsRef.current = []
        const runCodeExecs = [...currentRunCodeExecutionsRef.current]
        currentRunCodeExecutionsRef.current = []
        void refreshMessages().then((items) => {
          const lastAssistant = [...items].reverse().find((m) => m.role === 'assistant')
          if (lastAssistant) {
            if (runSources.length > 0) {
              writeMessageSources(lastAssistant.id, runSources)
              setMessageSourcesMap((prev) => new Map(prev).set(lastAssistant.id, runSources))
            }
            if (runSearchSteps.length > 0) {
              writeMessageSearchSteps(lastAssistant.id, runSearchSteps)
              setMessageSearchStepsMap((prev) => new Map(prev).set(lastAssistant.id, runSearchSteps))
            }
            if (runArtifacts.length > 0) {
              writeMessageArtifacts(lastAssistant.id, runArtifacts)
              setMessageArtifactsMap((prev) => new Map(prev).set(lastAssistant.id, runArtifacts))
            }
            writeMessageCodeExecutions(lastAssistant.id, runCodeExecs)
            setMessageCodeExecutionsMap((prev) => new Map(prev).set(lastAssistant.id, runCodeExecs))
            if (runThinking) {
              writeMessageThinking(lastAssistant.id, runThinking)
              setMessageThinkingMap((prev) => new Map(prev).set(lastAssistant.id, runThinking))
            }
          }
          const pending = pendingMessageRef.current
          if (pending) {
            pendingMessageRef.current = null
            void sendMessageRef.current(pending)
          }
        })
        // 标题生成在后端异步执行，run.completed 后 SSE 已断开，轮询补偿
        if (threadId) {
          const tid = threadId
          const pollTitle = (remaining: number) => {
            if (remaining <= 0) return
            setTimeout(() => {
              void getThread(accessToken, tid).then((resp) => {
                if (resp.title) onThreadTitleUpdated(tid, resp.title)
                else if (remaining > 1) pollTitle(remaining - 1)
              }).catch(() => {})
            }, 3000)
          }
          pollTitle(3)
        }
        continue
      }

      if (event.type === 'run.cancelled') {
        sse.disconnect()
        setActiveRunId(null)
        setThinkingDraft('')
        setTopLevelCodeExecutions([])
        setSegments([])
        resetSearchSteps()
        activeSegmentIdRef.current = null
        currentRunCodeExecutionsRef.current = []
        setAwaitingInput(false)
        setCheckInDraft('')
        if (threadId) onRunEnded(threadId)
        const data = event.data as { trace_id?: unknown }
        const traceId = typeof data?.trace_id === 'string' ? data.trace_id : undefined
        setError({ message: '已停止生成', traceId })
        continue
      }

      if (event.type === 'run.failed') {
        sse.disconnect()
        setActiveRunId(null)
        setThinkingDraft('')
        setTopLevelCodeExecutions([])
        setSegments([])
        resetSearchSteps()
        activeSegmentIdRef.current = null
        currentRunCodeExecutionsRef.current = []
        setAwaitingInput(false)
        setCheckInDraft('')
        if (threadId) onRunEnded(threadId)
        const obj = event.data as { message?: unknown; error_class?: unknown }
        setError({
          message: typeof obj?.message === 'string' ? obj.message : '运行失败',
          code: typeof obj?.error_class === 'string' ? obj.error_class : undefined,
        })
      }
    }
  }, [activeRunId, refreshMessages, refreshCredits, sse.events]) // eslint-disable-line react-hooks/exhaustive-deps

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

    // run.completed 等终端事件处理中会同步 setActiveRunId(null)，
    // React 批量更新后 activeRunId 已经为 null，不会到达此处。
    // 到达此处说明 SSE 关闭时确实没有处理过终端事件。
    sseTerminalFallbackArmedRef.current = false
    sseTerminalFallbackRunIdRef.current = null
    const runThinking = buildLiveThinkingSnapshot()
    const runSources = [...currentRunSourcesRef.current]
    const runArtifacts = [...currentRunArtifactsRef.current]
    const runCodeExecs = [...currentRunCodeExecutionsRef.current]
    currentRunArtifactsRef.current = []
    currentRunCodeExecutionsRef.current = []

    setActiveRunId(null)
    setAssistantDraft('')
    setThinkingDraft('')
    setTopLevelCodeExecutions([])
    setSegments([])
    activeSegmentIdRef.current = null
    const runSearchSteps = finalizeSearchSteps(searchStepsRef.current)
    if (runSearchSteps.length > 0) applySearchSteps(() => runSearchSteps)
    setQueuedDraft(null)
    setAwaitingInput(false)
    setCheckInDraft('')
    if (threadId) onRunEnded(threadId)
    refreshCredits()

    void refreshMessages().then((items) => {
      const lastAssistant = [...items].reverse().find((m) => m.role === 'assistant')
      if (lastAssistant) {
        if (runSources.length > 0) {
          writeMessageSources(lastAssistant.id, runSources)
          setMessageSourcesMap((prev) => new Map(prev).set(lastAssistant.id, runSources))
        }
        if (runSearchSteps.length > 0) {
          writeMessageSearchSteps(lastAssistant.id, runSearchSteps)
          setMessageSearchStepsMap((prev) => new Map(prev).set(lastAssistant.id, runSearchSteps))
        }
        if (runArtifacts.length > 0) {
          writeMessageArtifacts(lastAssistant.id, runArtifacts)
          setMessageArtifactsMap((prev) => new Map(prev).set(lastAssistant.id, runArtifacts))
        }
        writeMessageCodeExecutions(lastAssistant.id, runCodeExecs)
        setMessageCodeExecutionsMap((prev) => new Map(prev).set(lastAssistant.id, runCodeExecs))
        if (runThinking) {
          writeMessageThinking(lastAssistant.id, runThinking)
          setMessageThinkingMap((prev) => new Map(prev).set(lastAssistant.id, runThinking))
        }
      }
    })
  }, [activeRunId, sse.state, buildLiveThinkingSnapshot]) // eslint-disable-line react-hooks/exhaustive-deps

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
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, assistantDraft, segments])

  // COP 代码执行列表：新 item 添加时自动滚动到底部
  useEffect(() => {
    const el = copCodeExecScrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [topLevelCodeExecutions.length])

  // 发送新消息时强制滚动到底部（用户主动操作，应该跟上）
  const scrollToBottom = useCallback(() => {
    isAtBottomRef.current = true
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [])

  const handleAttachFiles = useCallback((files: File[]) => {
    const readers = files.map((file) => {
      return new Promise<Attachment>((resolve, reject) => {
        const isText = file.type.startsWith('text/') || file.type === ''
        const reader = new FileReader()
        reader.onload = () => {
          resolve({
            id: `${file.name}-${file.size}-${Date.now()}`,
            name: file.name,
            size: file.size,
            content: reader.result as string,
            encoding: isText ? 'text' : 'base64',
          })
        }
        reader.onerror = () => reject(reader.error ?? new Error(`读取失败: ${file.name}`))
        if (isText) {
          reader.readAsText(file)
        } else {
          reader.readAsDataURL(file)
        }
      })
    })
    void Promise.allSettled(readers).then((results) => {
      const newAttachments = results
        .filter((r): r is PromiseFulfilledResult<Attachment> => r.status === 'fulfilled')
        .map((r) => r.value)
      if (newAttachments.length === 0) return
      setAttachments((prev) => {
        const existingNames = new Set(prev.map((a) => a.name))
        const deduped = newAttachments.filter((a) => !existingNames.has(a.name))
        return [...prev, ...deduped]
      })
    })
  }, [])

  const handleRemoveAttachment = useCallback((id: string) => {
    setAttachments((prev) => prev.filter((a) => a.id !== id))
  }, [])

  const handleSend = async (e: React.FormEvent<HTMLFormElement>, personaKey: string) => {
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
    setError(null)

    try {
      const fileParts = attachments.map(
        (a) => `<file name="${a.name}" encoding="${a.encoding}">\n${a.content}\n</file>`,
      )
      const content = fileParts.length > 0
        ? `${fileParts.join('\n\n')}${text ? `\n\n${text}` : ''}`
        : text

      // 首次在无痕模式下发送：先 fork 出一个 private thread，再在其中发送
      if (pendingIncognito && messages.length > 0) {
        const lastMessageId = messages[messages.length - 1].id
        const forked = await forkThread(accessToken, threadId, lastMessageId, true)
        if (forked.id_mapping) migrateMessageMetadata(forked.id_mapping)
        onThreadCreated(forked)
        await createMessage(accessToken, forked.id, { content })
        const run = await createRun(accessToken, forked.id, personaKey)
        if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(forked.id)
        setDraft('')
        setAttachments([])
        navigate(`/t/${forked.id}`, {
          state: { isIncognitoFork: true, initialRunId: run.run_id, forkBaseCount: messages.length },
          replace: false,
        })
        onRunStarted(forked.id)
        return
      }

      const message = await createMessage(accessToken, threadId, { content })
      setMessages((prev) => [...prev, message])
      setDraft('')
      setAttachments([])
      setAssistantDraft('')

      const run = await createRun(accessToken, threadId, personaKey)
      if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(threadId)
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
  }

  const handleEditMessage = useCallback(async (messageId: string, newContent: string) => {
    if (isStreaming || sending || !threadId) return
    setSending(true)
    setError(null)
    setAssistantDraft('')
    try {
      const run = await editMessage(accessToken, threadId, messageId, newContent)
      // 乐观更新：替换消息内容，移除其后所有消息
      setMessages((prev) => {
        const idx = prev.findIndex((m) => m.id === messageId)
        if (idx === -1) return prev
        return prev.slice(0, idx + 1).map((m, i) =>
          i === idx ? { ...m, content: newContent } : m,
        )
      })
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
  }, [accessToken, threadId, isStreaming, sending, onRunStarted, onLoggedOut, scrollToBottom])

  const handleRetry = useCallback(async () => {
    if (isStreaming || sending || !threadId) return
    setSending(true)
    setError(null)
    setAssistantDraft('')
    try {
      const run = await retryThread(accessToken, threadId)
      // 乐观地移除最后一条 assistant 消息（后端已标记 hidden）
      setMessages((prev) => {
        const lastAssistantIdx = prev.map((m) => m.role).lastIndexOf('assistant')
        if (lastAssistantIdx === -1) return prev
        return prev.filter((_, i) => i !== lastAssistantIdx)
      })
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
  }, [accessToken, threadId, isStreaming, sending, onRunStarted, onLoggedOut, scrollToBottom])

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
    try {
      await provideInput(accessToken, activeRunId, text)
      setCheckInDraft('')
      setAwaitingInput(false)
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

  const handleCancel = useCallback(() => {
    if (!activeRunId || cancelSubmitting) return
    const runId = activeRunId

    sse.disconnect()
    setActiveRunId(null)
    setAssistantDraft('')
    setAwaitingInput(false)
    setCheckInDraft('')
    setCancelSubmitting(true)
    setError(null)
    pendingMessageRef.current = null
    setQueuedDraft(null)
    if (threadId) onRunEnded(threadId)

    void cancelRun(accessToken, runId).catch((err: unknown) => {
      setError(normalizeError(err))
    })
  }, [activeRunId, cancelSubmitting, sse.disconnect, accessToken, threadId, onRunEnded])

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

  const resolvedMessageSources = useMemo(() => {
    return resolveMessageSourcesForRender(messages, messageSourcesMap)
  }, [messages, messageSourcesMap])

  const historicalTimelineMap = useMemo(() => {
    const timelineMap = new Map<string, { steps: MessageSearchStepRef[]; sources: WebSource[] }>()

    messages.forEach((msg, idx) => {
      if (msg.role !== 'assistant') return
      const sources = resolvedMessageSources.get(msg.id) ?? []
      const cachedSteps = messageSearchStepsMap.get(msg.id)
      if (cachedSteps && cachedSteps.length > 0) {
        timelineMap.set(msg.id, { steps: cachedSteps, sources })
        return
      }

      // 无缓存步骤时：需有 sources 才有兜底意义
      if (sources.length === 0) return
      // 仅 Search 模式线程用通用兜底，其他模式需有缓存步骤才渲染
      if (!isSearchThread) return

      let userQuery: string | undefined
      for (let j = idx - 1; j >= 0; j--) {
        if (messages[j].role === 'user') {
          userQuery = messages[j].content
          break
        }
      }
      timelineMap.set(msg.id, { steps: buildHistoricalSearchSteps(userQuery), sources })
    })

    return timelineMap
  }, [isSearchThread, messages, resolvedMessageSources, messageSearchStepsMap])

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

  const openDocumentPanel = useCallback((artifact: ArtifactRef) => {
    setDocumentPanelArtifact((prev) => {
      if (prev?.key === artifact.key) {
        onRightPanelChange?.(false)
        return null
      }
      setSourcePanelMessageId(null)
      setCodePanelExecution(null)
      onRightPanelChange?.(true)
      return artifact
    })
  }, [onRightPanelChange])

  // COP step 计数：timeline 中所有非 finished 的点
  const dedupedTopLevelCodeExecutions = useMemo(() => {
    const seen = new Set<string>()
    return topLevelCodeExecutions.filter((ce) => {
      if (seen.has(ce.id)) return false
      seen.add(ce.id)
      return true
    })
  }, [topLevelCodeExecutions])

  const copStepCount = useMemo(() => {
    const timelineSteps = searchSteps.filter(s => s.kind !== 'finished').length
    const segmentSteps = searchSteps.length === 0
      ? segments.filter(s => s.mode !== 'hidden').length
      : 0
    const codeExecSteps = timelineSteps === 0 && segmentSteps === 0
      ? dedupedTopLevelCodeExecutions.length
      : 0
    return timelineSteps + segmentSteps + codeExecSteps
  }, [searchSteps, segments, dedupedTopLevelCodeExecutions])

  const copHeaderLabel = !assistantDraft
    ? 'Thinking'
    : copStepCount > 0
      ? `${copStepCount} steps completed`
      : 'Completed'

  return (
    <div className="relative flex min-w-0 flex-1 flex-col overflow-hidden bg-[var(--c-bg-page)]">
      {/* 顶部 header */}
      <div className="flex min-h-[51px] items-center justify-end gap-2 px-[15px] py-[15px]">
        {threadId && privateThreadIds.has(threadId) && (
          <span className="text-xs font-medium text-[var(--c-text-muted)]">{t.incognitoLabel}</span>
        )}
        <div className="flex items-center gap-1 text-[var(--c-text-secondary)]" style={{ opacity: 0.8 }}>
          <Zap size={13} strokeWidth={2.2} />
          <span className="text-sm font-medium tabular-nums">{creditsBalance.toLocaleString()}</span>
        </div>
        <NotificationBell accessToken={accessToken} onClick={onOpenNotifications} refreshKey={notificationVersion} title={t.notificationsTitle} />
        {threadId && !privateThreadIds.has(threadId) && (
          <button
            onClick={() => setShareModalOpen(true)}
            title={t.shareTitle}
            className="flex h-8 w-8 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
          >
            <Share2 size={18} />
          </button>
        )}
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
      </div>

      {/* 主体区域：消息 + 输入 + 可选的 sources 侧边面板 */}
      <div className="flex flex-1 min-h-0">
        <div className="relative flex flex-1 min-w-0 flex-col">
          {/* 消息列表 */}
          <div
            ref={scrollContainerRef}
            onScroll={handleScrollContainerScroll}
            className="relative flex-1 min-h-0 overflow-y-auto bg-[var(--c-bg-page)]"
          >
        <div
          style={{ maxWidth: 800, margin: '0 auto', padding: `50px ${isPanelOpen ? '32px' : '60px'} 200px` }}
          className="flex w-full flex-col gap-6"
        >
          {messagesLoading ? (
            <div className="py-20 text-center text-sm text-[var(--c-text-muted)]">加载中...</div>
          ) : (
            <>
              {messages.map((msg, idx) => {
                const resolvedSources = msg.role === 'assistant' ? resolvedMessageSources.get(msg.id) : undefined
                const canShowSources = !!(resolvedSources && resolvedSources.length > 0)
                const timeline = msg.role === 'assistant'
                  ? historicalTimelineMap.get(msg.id)
                  : undefined
                const timelineSteps = timeline?.steps ?? []
                const timelineSources = timeline?.sources ?? (resolvedSources ?? [])
                const messageCodeExecutions = msg.role === 'assistant' ? messageCodeExecutionsMap.get(msg.id) : undefined
                const hasMessageCodeExecutions = !!(messageCodeExecutions && messageCodeExecutions.length > 0)

                return (
                  <div key={msg.id} ref={idx === lastUserMsgIdx ? lastUserMsgRef : undefined}>
                  {/* 完成后的搜索时间轴：最后一条 assistant 消息上方 */}
                  {(timelineSteps.length > 0 || hasMessageCodeExecutions) && (
                    <div style={{ marginBottom: '12px' }}>
                      <SearchTimeline
                        steps={timelineSteps}
                        sources={timelineSources}
                        isComplete
                        codeExecutions={messageCodeExecutions}
                        onOpenCodeExecution={openCodePanel}
                      />
                    </div>
                  )}
                  <MessageBubble
                    message={msg}
                    onRetry={
                      msg.role === 'assistant' && idx === messages.length - 1 && !isStreaming && !sending
                        ? handleRetry
                        : undefined
                    }
                    onEdit={
                      msg.role === 'user' && !isStreaming && !sending
                        ? (newContent) => handleEditMessage(msg.id, newContent)
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
                    onReport={
                      msg.role === 'assistant' && !isStreaming && !sending && threadId
                        ? () => setReportModalOpen(true)
                        : undefined
                    }
                    webSources={resolvedSources}
                    artifacts={msg.role === 'assistant' ? messageArtifactsMap.get(msg.id) : undefined}
                    accessToken={accessToken}
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
                    activePanelArtifactKey={documentPanelArtifact?.key ?? null}
                  />
                  {/* 无痕分割线：固定在 fork 基点之后 */}
                  {locationState?.isIncognitoFork && locationState.forkBaseCount != null && idx === locationState.forkBaseCount - 1 && (
                    <IncognitoDivider text={t.incognitoForkDivider} />
                  )}
                  </div>
                )
              })}

              {/* 流式 COP 状态指示：Thinking / XX steps completed */}
              {isStreaming && searchSteps.length === 0 && (segments.length > 0 || dedupedTopLevelCodeExecutions.length > 0 || !assistantDraft) && (
                <motion.div
                  initial={{ opacity: 0, y: 6 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ duration: 0.3, ease: 'easeOut' }}
                  style={{ maxWidth: '663px' }}
                >
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '6px',
                      padding: '6px 0',
                      color: 'var(--c-text-secondary)',
                      fontSize: '13px',
                      fontWeight: 500,
                    }}
                  >
                    {!assistantDraft && (
                      <Loader2 size={13} className="animate-spin" style={{ flexShrink: 0, color: 'var(--c-text-secondary)' }} />
                    )}
                    {copHeaderLabel && (
                      <span className={!assistantDraft ? 'thinking-shimmer' : undefined}>{copHeaderLabel}</span>
                    )}
                  </div>
                  {!assistantDraft && segments.length > 0 && (
                    <div style={{ paddingLeft: '24px', paddingTop: '2px' }}>
                      {segments.filter(s => s.label && s.mode !== 'hidden').map(seg => (
                        <div
                          key={seg.segmentId}
                          style={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: '6px',
                            fontSize: '13px',
                            color: 'var(--c-text-muted)',
                            padding: '4px 0',
                          }}
                        >
                          {seg.isStreaming && (
                            <Loader2 size={12} className="animate-spin" style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
                          )}
                          <span>{seg.label}</span>
                        </div>
                      ))}
                    </div>
                  )}
                  {dedupedTopLevelCodeExecutions.length > 0 && (
                    <div ref={copCodeExecScrollRef} style={{ paddingLeft: '24px', paddingTop: '6px', maxHeight: '60vh', overflowY: 'auto' }}>
                      {dedupedTopLevelCodeExecutions.map((ce, idx) => {
                        const isLast = idx === dedupedTopLevelCodeExecutions.length - 1
                        const showDot = dedupedTopLevelCodeExecutions.length > 0
                        const showLine = dedupedTopLevelCodeExecutions.length >= 2
                        return (
                          <motion.div
                            key={ce.id}
                            initial={{ opacity: 0, x: -6 }}
                            animate={{ opacity: 1, x: 0 }}
                            transition={{ duration: 0.25, ease: 'easeOut' }}
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
                                  width: '8px',
                                  height: '8px',
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
                              ? <ShellExecutionBlock code={ce.code} output={ce.output} exitCode={ce.exitCode} isStreaming={isStreaming} />
                              : <CodeExecutionCard language={ce.language} code={ce.code} output={ce.output} exitCode={ce.exitCode} onOpen={() => openCodePanel(ce)} />
                            }
                          </motion.div>
                        )
                      })}
                    </div>
                  )}
                </motion.div>
              )}

              {/* 流式期间的 live 时间轴 */}
              {(isStreaming || liveTimelineExiting) && searchSteps.length > 0 && (
                <SearchTimeline
                  steps={searchSteps}
                  sources={currentRunSourcesRef.current}
                  isComplete={liveTimelineExiting && !isStreaming}
                  codeExecutions={dedupedTopLevelCodeExecutions.length > 0 ? dedupedTopLevelCodeExecutions : undefined}
                  onOpenCodeExecution={openCodePanel}
                  headerOverride={!liveTimelineExiting ? copHeaderLabel : undefined}
                  shimmer={!liveTimelineExiting && !assistantDraft}
                />
              )}

              {/* 非搜索模式：常规 segment 渲染 */}
              {SHOW_EXPLICIT_THINKING && !isSearchThread && segments.map((seg) => (
                <ThinkingBlock
                  key={seg.segmentId}
                  kind={seg.kind}
                  label={seg.label}
                  mode={seg.mode as 'visible' | 'collapsed' | 'hidden'}
                  content={seg.content}
                  isStreaming={seg.isStreaming}
                  codeExecutions={seg.codeExecutions}
                  onOpenCodeExecution={openCodePanel}
                />
              ))}

              {SHOW_EXPLICIT_THINKING && thinkingDraft && (
                <ThinkingBlock
                  kind="thinking"
                  label="thinking"
                  mode="collapsed"
                  content={thinkingDraft}
                  isStreaming={!!activeRunId}
                />
              )}

              {/* 无 COP 时，顶层代码执行卡片独立渲染（仅流式结束后、run.completed 前的短暂窗口） */}
              {!isStreaming && dedupedTopLevelCodeExecutions.length > 0 && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                  {dedupedTopLevelCodeExecutions.map((ce) =>
                    ce.language === 'shell'
                      ? <ShellExecutionBlock key={ce.id} code={ce.code} output={ce.output} exitCode={ce.exitCode} />
                      : <CodeExecutionCard key={ce.id} language={ce.language} code={ce.code} output={ce.output} exitCode={ce.exitCode} onOpen={() => openCodePanel(ce)} />
                  )}
                </div>
              )}

              {assistantDraft && (
                <StreamingBubble
                  content={assistantDraft}
                  webSources={currentRunSourcesRef.current.length > 0 ? currentRunSourcesRef.current : undefined}
                />
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
                    placeholder="Type your response..."
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
        style={{ maxWidth: 1200, margin: '0 auto', padding: `12px ${isPanelOpen ? '32px' : '60px'} 16px`, position: 'absolute', bottom: 0, left: 0, right: 0, zIndex: 10, background: 'linear-gradient(to bottom, transparent 0%, var(--c-bg-page) 24px)' }}
        className="flex w-full flex-col items-center gap-2"
      >
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
        {attachments.length > 0 && (
          <div className="flex w-full max-w-[756px] flex-wrap gap-2">
            {attachments.map((att) => (
              <div
                key={att.id}
                className="flex items-center gap-1.5 rounded-lg px-2.5 py-1.5"
                style={{ background: 'var(--c-bg-sub)', border: '0.5px solid var(--c-border-subtle)' }}
              >
                <Paperclip size={12} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
                <span
                  className="text-xs"
                  style={{ color: 'var(--c-text-secondary)', maxWidth: '160px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                >
                  {att.name}
                </span>
                <span className="text-xs" style={{ color: 'var(--c-text-muted)', flexShrink: 0 }}>
                  {formatFileSize(att.size)}
                </span>
                <button
                  type="button"
                  onClick={() => handleRemoveAttachment(att.id)}
                  className="flex items-center justify-center rounded transition-opacity duration-100 hover:opacity-100"
                  style={{ color: 'var(--c-text-muted)', opacity: 0.7, marginLeft: '2px' }}
                >
                  <X size={12} />
                </button>
              </div>
            ))}
          </div>
        )}
        <ChatInput
          value={draft}
          onChange={setDraft}
          onSubmit={handleSend}
          onCancel={handleCancel}
          placeholder="Reply..."
          disabled={sending}
          isStreaming={isStreaming}
          canCancel={canCancel}
          cancelSubmitting={cancelSubmitting}
          attachments={attachments}
          onAttachFiles={handleAttachFiles}
          accessToken={accessToken}
          onAsrError={handleAsrError}
          searchMode={isSearchThread}
          onPersonaChange={(personaKey) => setIsSearchThread(personaKey === SEARCH_PERSONA_KEY)}
        />
        <p style={{ color: 'var(--c-text-muted)', fontSize: '13px', letterSpacing: '-0.52px', textAlign: 'center' }}>
          Arkloop is AI and can make mistakes. Please double-check responses.
        </p>

        {error && (
          <div className="w-full max-w-[756px]">
            <ErrorCallout error={error} />
          </div>
        )}
      </div>

        </div>
        {/* 右侧面板 - width 过渡驱动整体布局动画 */}
        <div
          style={{
            width: isDocumentPanelOpen ? '540px' : (isSourcePanelOpen || isCodePanelOpen) ? '420px' : '0px',
            overflow: 'hidden',
            flexShrink: 0,
            transition: 'width 280ms cubic-bezier(0.16,1,0.3,1)',
            borderLeft: (panelDisplaySources || codePanelDisplay || documentPanelDisplay) ? '0.5px solid var(--c-border-subtle)' : 'none',
          }}
        >
          {isSourcePanelOpen && panelDisplaySources && panelDisplaySources.length > 0 && (
            <div style={{ width: '420px', height: '100%' }}>
              <SourcesPanel
                sources={panelDisplaySources}
                userQuery={panelDisplayQuery}
                onClose={() => { setSourcePanelMessageId(null); onRightPanelChange?.(false) }}
              />
            </div>
          )}
          {isCodePanelOpen && codePanelDisplay && (
            <div style={{ width: '420px', height: '100%' }}>
              <CodeExecutionPanel
                execution={codePanelDisplay}
                onClose={() => { setCodePanelExecution(null); onRightPanelChange?.(false) }}
              />
            </div>
          )}
          {isDocumentPanelOpen && documentPanelDisplay && (
            <div style={{ width: '540px', height: '100%' }}>
              <DocumentPanel
                artifact={documentPanelDisplay}
                accessToken={accessToken}
                onClose={() => { setDocumentPanelArtifact(null); onRightPanelChange?.(false) }}
              />
            </div>
          )}
        </div>
      </div>

      {threadId && (
        <ShareModal
          accessToken={accessToken}
          threadId={threadId}
          open={shareModalOpen}
          onClose={() => setShareModalOpen(false)}
        />
      )}

      {threadId && (
        <ReportModal
          accessToken={accessToken}
          threadId={threadId}
          open={reportModalOpen}
          onClose={() => setReportModalOpen(false)}
        />
      )}
    </div>
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
