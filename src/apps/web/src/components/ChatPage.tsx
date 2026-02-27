import { useState, useEffect, useRef, useCallback, useMemo, type FormEvent } from 'react'
import { useParams, useLocation, useOutletContext } from 'react-router-dom'
import { Glasses, Paperclip, Share2, X, Zap } from 'lucide-react'
import { ChatInput, type Attachment, formatFileSize } from './ChatInput'
import { MessageBubble, StreamingBubble } from './MessageBubble'
import { ThinkingBlock } from './ThinkingBlock'
import { ErrorCallout, type AppError } from './ErrorCallout'
import { DebugFloatingPanel } from './DebugFloatingPanel'
import { ShareModal } from './ShareModal'
import { NotificationBell } from './NotificationBell'
import { useSSE } from '../hooks/useSSE'
import { SSEApiError } from '../sse'
import { selectFreshRunEvents } from '../runEventProcessing'
import { useLocale } from '../contexts/LocaleContext'
import {
  createMessage,
  createRun,
  cancelRun,
  provideInput,
  retryThread,
  editMessage,
  listMessages,
  listThreadRuns,
  isApiError,
  type MessageResponse,
} from '../api'
import { type SelectedTier } from '../storage'

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
  refreshCredits: () => void
  onOpenNotifications: () => void
  notificationVersion: number
  creditsBalance: number
  isPrivateMode: boolean
  onTogglePrivateMode: () => void
  privateThreadIds: Set<string>
}

type LocationState = { initialRunId?: string; isSearch?: boolean } | null

export function ChatPage() {
  const { accessToken, onLoggedOut, onRunStarted, onRunEnded, refreshCredits, onOpenNotifications, notificationVersion, creditsBalance, onTogglePrivateMode, privateThreadIds } = useOutletContext<OutletContext>()
  const { threadId } = useParams<{ threadId: string }>()
  const location = useLocation()
  const locationState = location.state as LocationState
  const { t } = useLocale()

  const baseUrl = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? ''

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

  // segment 状态：用于渲染 Agent 规划轮折叠块
  type Segment = { segmentId: string; kind: string; mode: string; label: string; content: string; isStreaming: boolean }
  const [segments, setSegments] = useState<Segment[]>([])
  const activeSegmentIdRef = useRef<string | null>(null)
  // Pro 路径的 LLM 原生 thinking 内容（channel: "thinking"）
  const [thinkingDraft, setThinkingDraft] = useState('')

  const bottomRef = useRef<HTMLDivElement>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const processedEventCountRef = useRef(0)
  const pendingMessageRef = useRef<string | null>(null)
  // 用户是否停留在底部区域（距底部 80px 以内视为"在底部"）
  const isAtBottomRef = useRef(true)

  const handleScrollContainerScroll = useCallback(() => {
    const el = scrollContainerRef.current
    if (!el) return
    isAtBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight <= 80
  }, [])

  const sse = useSSE({ runId: activeRunId ?? '', accessToken, baseUrl })

  const isStreaming = activeRunId != null
  const canCancel =
    activeRunId != null &&
    (sse.state === 'connecting' || sse.state === 'connected' || sse.state === 'reconnecting')

  const refreshMessages = useCallback(async () => {
    if (!threadId) return
    try {
      const items = await listMessages(accessToken, threadId)
      setMessages(items)
    } catch (err) {
      setError(normalizeError(err))
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
        // 若 location state 已提供 initialRunId，优先使用（来自 WelcomePage 新建后导航）
        if (locationState?.initialRunId) {
          if (threadId) onRunStarted(threadId)
        } else {
          const latest = runs[0]
          const isRunning = latest?.status === 'running'
          setActiveRunId(isRunning ? latest.run_id : null)
          if (isRunning && threadId) onRunStarted(threadId)
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

  // 切换 thread 时清理 SSE 和排队消息
  useEffect(() => {
    setAssistantDraft('')
    setSegments([])
    activeSegmentIdRef.current = null
    setThinkingDraft('')
    setCancelSubmitting(false)
    pendingMessageRef.current = null
    setQueuedDraft(null)
    sse.disconnect()
    sse.clearEvents()
    processedEventCountRef.current = 0
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [threadId])

  // 连接 SSE
  useEffect(() => {
    if (!activeRunId) return
    sse.reset()
    sse.connect()
    processedEventCountRef.current = 0
    setAssistantDraft('')
    setSegments([])
    activeSegmentIdRef.current = null
    setThinkingDraft('')
    setCancelSubmitting(false)
    return () => { sse.disconnect() }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeRunId])

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
        const display = (obj.display ?? {}) as { mode?: unknown; label?: unknown }
        const mode = typeof display.mode === 'string' ? display.mode : 'collapsed'
        const label = typeof display.label === 'string' ? display.label : ''
        if (!segmentId) continue
        activeSegmentIdRef.current = segmentId
        setSegments((prev) => [...prev, { segmentId, kind, mode, label, content: '', isStreaming: true }])
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

      if (event.type === 'run.input_requested') {
        setAwaitingInput(true)
        continue
      }

      if (event.type === 'run.completed') {
        sse.disconnect()
        setActiveRunId(null)
        setAssistantDraft('')
        setThinkingDraft('')
        setSegments([])
        activeSegmentIdRef.current = null
        setQueuedDraft(null)
        setAwaitingInput(false)
        setCheckInDraft('')
        if (threadId) onRunEnded(threadId)
        refreshCredits()
        void refreshMessages().then(() => {
          const pending = pendingMessageRef.current
          if (pending) {
            pendingMessageRef.current = null
            void sendMessageRef.current(pending)
          }
        })
        continue
      }

      if (event.type === 'run.cancelled') {
        sse.disconnect()
        setActiveRunId(null)
        setThinkingDraft('')
        setSegments([])
        activeSegmentIdRef.current = null
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
        setSegments([])
        activeSegmentIdRef.current = null
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

  // 新消息/流式内容时，仅在用户停留在底部时自动滚动
  useEffect(() => {
    if (!isAtBottomRef.current) return
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, assistantDraft, thinkingDraft, segments])

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

  const handleSend = async (e: FormEvent<HTMLFormElement>, tier: SelectedTier) => {
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

      const message = await createMessage(accessToken, threadId, { content })
      setMessages((prev) => [...prev, message])
      setDraft('')
      setAttachments([])
      setAssistantDraft('')

      const tierToSkillId: Record<SelectedTier, string> = {
        Auto: 'auto',
        Lite: 'lite',
        Pro: 'pro',
        Ultra: 'ultra',
      }
      const run = await createRun(accessToken, threadId, tierToSkillId[tier])
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
          onClick={threadId && privateThreadIds.has(threadId) ? undefined : onTogglePrivateMode}
          title={threadId && privateThreadIds.has(threadId) ? t.thisThreadIsIncognito : t.toggleIncognito}
          className={[
            'flex h-8 w-8 items-center justify-center rounded-lg transition-colors',
            threadId && privateThreadIds.has(threadId)
              ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)] cursor-default'
              : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]',
          ].join(' ')}
        >
          <Glasses size={18} />
        </button>
      </div>

      {/* 消息列表 */}
      <div
        ref={scrollContainerRef}
        onScroll={handleScrollContainerScroll}
        className="relative flex-1 min-h-0 overflow-y-auto bg-[var(--c-bg-page)]"
      >
        <div
          style={{ maxWidth: 800, margin: '0 auto', padding: '50px 60px' }}
          className="flex w-full flex-col gap-6"
        >
          {messagesLoading ? (
            <div className="py-20 text-center text-sm text-[var(--c-text-muted)]">加载中...</div>
          ) : (
            <>
              {messages.map((msg, idx) => (
                <MessageBubble
                  key={msg.id}
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
                />
              ))}

              {segments.map((seg) => (
                <ThinkingBlock
                  key={seg.segmentId}
                  kind={seg.kind}
                  label={seg.label}
                  mode={seg.mode as 'visible' | 'collapsed' | 'hidden'}
                  content={seg.content}
                  isStreaming={seg.isStreaming}
                />
              ))}

              {thinkingDraft && (
                <ThinkingBlock
                  kind="thinking"
                  label="思考过程"
                  mode="collapsed"
                  content={thinkingDraft}
                  isStreaming={!!activeRunId}
                />
              )}

              {assistantDraft && <StreamingBubble content={assistantDraft} />}

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

              <div ref={bottomRef} />
            </>
          )}
        </div>
      </div>

      {/* 输入区域 */}
      <div
        style={{ maxWidth: 1200, margin: '0 auto', padding: '20px 60px 24px', flexShrink: 0 }}
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
          searchMode={locationState?.isSearch === true}
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

      {/* 调试悬浮面板 */}
      <DebugFloatingPanel
        events={sse.events}
        state={sse.state}
        lastSeq={sse.lastSeq}
        error={sse.error}
        activeRunId={activeRunId}
        onReconnect={sse.reconnect}
        onClear={sse.clearEvents}
      />

      {threadId && (
        <ShareModal
          accessToken={accessToken}
          threadId={threadId}
          open={shareModalOpen}
          onClose={() => setShareModalOpen(false)}
        />
      )}
    </div>
  )
}
