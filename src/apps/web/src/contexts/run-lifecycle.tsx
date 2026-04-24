import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useSSE, type UseSSEResult } from '../hooks/useSSE'
import { type AppError } from '@arkloop/shared'
import { apiBaseUrl } from '@arkloop/shared/api'
import type { UserInputRequest } from '../userInputTypes'
import { clearThreadRunHandoff } from '../storage'
import { useAuth } from './auth'
import { useChatSession } from './chat-session'

// TODO: extract to shared types
type ContextCompactBarState =
  | { type: 'persist'; status: 'running' | 'done' | 'llm_failed' }
  | { type: 'trim'; status: 'done'; dropped: number }

type TerminalRunHandoffStatus = 'completed' | 'cancelled' | 'interrupted' | 'failed' | null

const COMPLETED_RUN_SSE_TAIL_MS = 30000

interface RunLifecycleContextValue {
  activeRunId: string | null
  sending: boolean
  cancelSubmitting: boolean
  error: AppError | null
  injectionBlocked: string | null
  queuedDraft: string | null
  awaitingInput: boolean
  pendingUserInput: UserInputRequest | null
  checkInDraft: string
  checkInSubmitting: boolean
  contextCompactBar: ContextCompactBarState | null
  terminalRunDisplayId: string | null
  terminalRunHandoffStatus: TerminalRunHandoffStatus
  terminalRunAssistantMessageId: string | null
  terminalRunHistoryExpanded: boolean
  completedTitleTailRunId: string | null

  isStreaming: boolean
  sseRunId: string

  sse: UseSSEResult

  setActiveRunId: (id: string | null) => void
  setSending: (v: boolean) => void
  setCancelSubmitting: (v: boolean) => void
  setError: (err: AppError | null) => void
  setInjectionBlocked: (v: string | null) => void
  setQueuedDraft: (v: string | null) => void
  setAwaitingInput: (v: boolean) => void
  setPendingUserInput: (v: UserInputRequest | null) => void
  setCheckInDraft: (v: string) => void
  setCheckInSubmitting: (v: boolean) => void
  setContextCompactBar: (v: ContextCompactBarState | null) => void
  setTerminalRunDisplayId: (v: string | null) => void
  setTerminalRunHandoffStatus: (v: TerminalRunHandoffStatus) => void
  markTerminalRunHistory: (msgId: string | null, expanded?: boolean) => void
  armCompletedTitleTail: (runId: string) => void
  clearCompletedTitleTail: () => void
  clearLiveRunState: () => void

  injectionBlockedRunIdRef: React.RefObject<string | null>
  processedEventCountRef: React.RefObject<number>
  freezeCutoffRef: React.RefObject<number | null>
  lastVisibleNonTerminalSeqRef: React.RefObject<number>
  sseTerminalFallbackRunIdRef: React.RefObject<string | null>
  sseTerminalFallbackArmedRef: React.RefObject<boolean>
  noResponseMsgIdRef: React.RefObject<string | null>
  replaceOnCancelRef: React.RefObject<string | null>
  pendingMessageRef: React.RefObject<string | null>
  seenFirstToolCallInRunRef: React.RefObject<boolean>
}

const Ctx = createContext<RunLifecycleContextValue | null>(null)

export function RunLifecycleProvider({ children }: { children: ReactNode }) {
  const { accessToken } = useAuth()
  const { threadId } = useChatSession()

  const [activeRunId, setActiveRunId] = useState<string | null>(null)
  const [sending, setSending] = useState(false)
  const [cancelSubmitting, setCancelSubmitting] = useState(false)
  const [error, setError] = useState<AppError | null>(null)
  const [injectionBlocked, setInjectionBlocked] = useState<string | null>(null)
  const [queuedDraft, setQueuedDraft] = useState<string | null>(null)
  const [awaitingInput, setAwaitingInput] = useState(false)
  const [pendingUserInput, setPendingUserInput] = useState<UserInputRequest | null>(null)
  const [checkInDraft, setCheckInDraft] = useState('')
  const [checkInSubmitting, setCheckInSubmitting] = useState(false)
  const [contextCompactBar, setContextCompactBar] = useState<ContextCompactBarState | null>(null)
  const [terminalRunDisplayId, setTerminalRunDisplayId] = useState<string | null>(null)
  const [terminalRunHandoffStatus, setTerminalRunHandoffStatus] = useState<TerminalRunHandoffStatus>(null)
  const [terminalRunAssistantMessageId, setTerminalRunAssistantMessageId] = useState<string | null>(null)
  const [terminalRunHistoryExpanded, setTerminalRunHistoryExpanded] = useState(false)
  const [completedTitleTailRunId, setCompletedTitleTailRunId] = useState<string | null>(null)

  const completedTitleTailTimerRef = useRef<number | null>(null)
  const contextCompactHideTimerRef = useRef<number | null>(null)

  // SSE dispatch 所需 refs
  const injectionBlockedRunIdRef = useRef<string | null>(null)
  const processedEventCountRef = useRef(0)
  const freezeCutoffRef = useRef<number | null>(null)
  const lastVisibleNonTerminalSeqRef = useRef(0)
  const sseTerminalFallbackRunIdRef = useRef<string | null>(null)
  const sseTerminalFallbackArmedRef = useRef(false)
  const noResponseMsgIdRef = useRef<string | null>(null)
  const replaceOnCancelRef = useRef<string | null>(null)
  const pendingMessageRef = useRef<string | null>(null)
  const seenFirstToolCallInRunRef = useRef(false)
  const hasMountedRef = useRef(false)

  // derived
  const isStreaming = activeRunId != null
  const sseRunId = activeRunId ?? completedTitleTailRunId ?? ''

  // SSE
  const baseUrl = apiBaseUrl()
  const sse = useSSE({ runId: sseRunId, accessToken, baseUrl })

  // --- actions ---

  const markTerminalRunHistory = useCallback((msgId: string | null, expanded = true) => {
    if (msgId) {
      setTerminalRunAssistantMessageId(msgId)
      setTerminalRunHistoryExpanded(expanded)
    } else {
      setTerminalRunAssistantMessageId(null)
      setTerminalRunHistoryExpanded(false)
    }
  }, [])

  const clearCompletedTitleTail = useCallback(() => {
    if (completedTitleTailTimerRef.current !== null) {
      window.clearTimeout(completedTitleTailTimerRef.current)
      completedTitleTailTimerRef.current = null
    }
    setCompletedTitleTailRunId(null)
  }, [])

  const armCompletedTitleTail = useCallback((runId: string) => {
    if (!runId) return
    if (completedTitleTailTimerRef.current !== null) {
      window.clearTimeout(completedTitleTailTimerRef.current)
    }
    setCompletedTitleTailRunId(runId)
    completedTitleTailTimerRef.current = window.setTimeout(() => {
      completedTitleTailTimerRef.current = null
      setCompletedTitleTailRunId((current) => (current === runId ? null : current))
    }, COMPLETED_RUN_SSE_TAIL_MS)
  }, [])

  const clearContextCompactHideTimer = useCallback(() => {
    if (contextCompactHideTimerRef.current != null) {
      clearTimeout(contextCompactHideTimerRef.current)
      contextCompactHideTimerRef.current = null
    }
  }, [])

  const clearLiveRunState = useCallback(() => {
    setActiveRunId(null)
    setSending(false)
    setCancelSubmitting(false)
    setError(null)
    setInjectionBlocked(null)
    setQueuedDraft(null)
    setAwaitingInput(false)
    setPendingUserInput(null)
    setCheckInDraft('')
    setCheckInSubmitting(false)
    setContextCompactBar(null)
    clearContextCompactHideTimer()
  }, [clearContextCompactHideTimer])

  // activeRunId 变化时清理 contextCompactBar
  useEffect(() => {
    if (!activeRunId) {
      clearContextCompactHideTimer()
      setContextCompactBar(null)
    }
  }, [activeRunId, clearContextCompactHideTimer])

  // 组件卸载时清理定时器
  useEffect(() => {
    return () => {
      clearContextCompactHideTimer()
      if (completedTitleTailTimerRef.current !== null) {
        window.clearTimeout(completedTitleTailTimerRef.current)
      }
    }
  }, [clearContextCompactHideTimer])

  // activeRunId 变化 -> 连接 SSE、重置 refs
  useEffect(() => {
    if (!activeRunId) return
    if (threadId) clearThreadRunHandoff(threadId)
    clearCompletedTitleTail()
    freezeCutoffRef.current = null
    injectionBlockedRunIdRef.current = null
    sseTerminalFallbackRunIdRef.current = activeRunId
    sseTerminalFallbackArmedRef.current = false
    seenFirstToolCallInRunRef.current = false
    sse.reset()
    sse.connect()
    processedEventCountRef.current = 0
    lastVisibleNonTerminalSeqRef.current = 0
    setCancelSubmitting(false)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeRunId, clearCompletedTitleTail, threadId])

  // sseRunId 变化 -> 连接/断开 SSE
  useEffect(() => {
    if (!sseRunId) return
    sse.connect()
    return () => { sse.disconnect() }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sseRunId])

  // activeRunId 变化 -> 重置 terminal run 状态
  useEffect(() => {
    if (!activeRunId) {
      lastVisibleNonTerminalSeqRef.current = 0
      return
    }
    setTerminalRunDisplayId(null)
    setTerminalRunHandoffStatus(null)
  }, [activeRunId])

  // activeRunId 变化 -> 清除 terminal run history
  useEffect(() => {
    if (!activeRunId) return
    markTerminalRunHistory(null)
  }, [activeRunId, markTerminalRunHistory])

  // SSE terminal fallback arming
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
      if (!sseRunId) return
      const s = sse.state
      if (s === 'closed' || s === 'error' || s === 'idle') {
        sse.reconnect()
      }
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => document.removeEventListener('visibilitychange', onVisibilityChange)
  }, [sse, sseRunId, sse.state, sse.reconnect])

  // 切换 thread 时重置 run 状态
  useEffect(() => {
    if (!hasMountedRef.current) {
      hasMountedRef.current = true
      return
    }
    setActiveRunId(null)
    clearCompletedTitleTail()
    setInjectionBlocked(null)
    setAwaitingInput(false)
    setPendingUserInput(null)
    setCheckInDraft('')
    setCheckInSubmitting(false)
    setQueuedDraft(null)
    setTerminalRunDisplayId(null)
    setTerminalRunHandoffStatus(null)
    markTerminalRunHistory(null)
    clearContextCompactHideTimer()
    injectionBlockedRunIdRef.current = null
    seenFirstToolCallInRunRef.current = false
    pendingMessageRef.current = null
    replaceOnCancelRef.current = null
    sse.disconnect()
    sse.clearEvents()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [threadId, clearCompletedTitleTail, markTerminalRunHistory, clearContextCompactHideTimer])

  const value = useMemo<RunLifecycleContextValue>(() => ({
    activeRunId,
    sending,
    cancelSubmitting,
    error,
    injectionBlocked,
    queuedDraft,
    awaitingInput,
    pendingUserInput,
    checkInDraft,
    checkInSubmitting,
    contextCompactBar,
    terminalRunDisplayId,
    terminalRunHandoffStatus,
    terminalRunAssistantMessageId,
    terminalRunHistoryExpanded,
    completedTitleTailRunId,
    isStreaming,
    sseRunId,
    sse,
    setActiveRunId,
    setSending,
    setCancelSubmitting,
    setError,
    setInjectionBlocked,
    setQueuedDraft,
    setAwaitingInput,
    setPendingUserInput,
    setCheckInDraft,
    setCheckInSubmitting,
    setContextCompactBar,
    setTerminalRunDisplayId,
    setTerminalRunHandoffStatus,
    markTerminalRunHistory,
    armCompletedTitleTail,
    clearCompletedTitleTail,
    clearLiveRunState,
    injectionBlockedRunIdRef,
    processedEventCountRef,
    freezeCutoffRef,
    lastVisibleNonTerminalSeqRef,
    sseTerminalFallbackRunIdRef,
    sseTerminalFallbackArmedRef,
    noResponseMsgIdRef,
    replaceOnCancelRef,
    pendingMessageRef,
    seenFirstToolCallInRunRef,
  }), [
    activeRunId,
    sending,
    cancelSubmitting,
    error,
    injectionBlocked,
    queuedDraft,
    awaitingInput,
    pendingUserInput,
    checkInDraft,
    checkInSubmitting,
    contextCompactBar,
    terminalRunDisplayId,
    terminalRunHandoffStatus,
    terminalRunAssistantMessageId,
    terminalRunHistoryExpanded,
    completedTitleTailRunId,
    isStreaming,
    sseRunId,
    sse,
    markTerminalRunHistory,
    armCompletedTitleTail,
    clearCompletedTitleTail,
    clearLiveRunState,
  ])

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useRunLifecycle(): RunLifecycleContextValue {
  const ctx = useContext(Ctx)
  if (!ctx) throw new Error('useRunLifecycle must be used within RunLifecycleProvider')
  return ctx
}

export type { ContextCompactBarState, TerminalRunHandoffStatus }
