import { useState, useEffect, useRef, useCallback, useMemo, type FormEvent } from 'react'
import { useParams, useLocation, useOutletContext } from 'react-router-dom'
import { Glasses, Paperclip, X } from 'lucide-react'
import { ChatInput, type Attachment, formatFileSize } from './ChatInput'
import { MessageBubble, StreamingBubble } from './MessageBubble'
import { ErrorCallout, type AppError } from './ErrorCallout'
import { DebugFloatingPanel } from './DebugFloatingPanel'
import { useSSE } from '../hooks/useSSE'
import { SSEApiError } from '../sse'
import { selectFreshRunEvents } from '../runEventProcessing'
import {
  createMessage,
  createRun,
  cancelRun,
  listMessages,
  listThreadRuns,
  isApiError,
  type MessageResponse,
} from '../api'

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
}

type LocationState = { initialRunId?: string } | null

export function ChatPage() {
  const { accessToken, onLoggedOut } = useOutletContext<OutletContext>()
  const { threadId } = useParams<{ threadId: string }>()
  const location = useLocation()
  const locationState = location.state as LocationState

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

  const bottomRef = useRef<HTMLDivElement>(null)
  const processedEventCountRef = useRef(0)

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
        // 若 location state 已提供 initialRunId，优先使用
        if (!locationState?.initialRunId) {
          const latest = runs[0]
          setActiveRunId(latest?.status === 'running' ? latest.run_id : null)
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

  // 切换 thread 时清理 SSE
  useEffect(() => {
    setAssistantDraft('')
    setCancelSubmitting(false)
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
      if (event.type === 'message.delta') {
        const obj = event.data as { content_delta?: unknown; role?: unknown }
        if (obj.role != null && obj.role !== 'assistant') continue
        if (typeof obj.content_delta !== 'string' || !obj.content_delta) continue
        setAssistantDraft((prev) => prev + obj.content_delta)
        continue
      }

      if (event.type === 'run.completed') {
        sse.disconnect()
        setActiveRunId(null)
        setAssistantDraft('')
        void refreshMessages()
        continue
      }

      if (event.type === 'run.cancelled') {
        sse.disconnect()
        setActiveRunId(null)
        const data = event.data as { trace_id?: unknown }
        const traceId = typeof data?.trace_id === 'string' ? data.trace_id : undefined
        setError({ message: '已停止生成', traceId })
        continue
      }

      if (event.type === 'run.failed') {
        sse.disconnect()
        setActiveRunId(null)
        const obj = event.data as { message?: unknown; error_class?: unknown }
        setError({
          message: typeof obj?.message === 'string' ? obj.message : '运行失败',
          code: typeof obj?.error_class === 'string' ? obj.error_class : undefined,
        })
      }
    }
  }, [activeRunId, refreshMessages, sse.events]) // eslint-disable-line react-hooks/exhaustive-deps

  // 401 SSE 错误时登出
  useEffect(() => {
    if (sse.error instanceof SSEApiError && sse.error.status === 401) {
      onLoggedOut()
    }
  }, [sse.error, onLoggedOut])

  // 自动滚动到底部
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, assistantDraft])

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

  const handleSend = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (sending || isStreaming || !threadId) return
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

      const run = await createRun(accessToken, threadId)
      setActiveRunId(run.run_id)
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

  const handleCancel = async () => {
    if (!activeRunId || cancelSubmitting) return
    setCancelSubmitting(true)
    setError(null)
    try {
      await cancelRun(accessToken, activeRunId)
    } catch (err) {
      setError(normalizeError(err))
      setCancelSubmitting(false)
    }
  }

  const terminalSseError = useMemo(() => {
    if (!sse.error) return null
    return normalizeError(sse.error)
  }, [sse.error])

  return (
    <div className="relative flex min-w-0 flex-1 flex-col overflow-hidden bg-[#262624]">
      {/* 顶部 header */}
      <div className="flex min-h-[51px] items-center justify-end px-[15px] py-[15px]">
        <button className="flex h-5 w-5 items-center justify-center text-[#c2c0b6] opacity-80 transition-opacity hover:opacity-100">
          <Glasses size={20} />
        </button>
      </div>

      {/* 消息列表 */}
      <div className="flex-1 overflow-y-auto bg-[#262624]">
        <div
          style={{ maxWidth: 800, margin: '0 auto', padding: '50px 60px' }}
          className="flex w-full flex-col gap-6"
        >
          {messagesLoading ? (
            <div className="py-20 text-center text-sm text-[#6b6b68]">加载中...</div>
          ) : (
            <>
              {messages.map((msg) => (
                <MessageBubble key={msg.id} message={msg} />
              ))}

              {assistantDraft && <StreamingBubble content={assistantDraft} />}

              {terminalSseError && <ErrorCallout error={terminalSseError} />}

              <div ref={bottomRef} />
            </>
          )}
        </div>
      </div>

      {/* 输入区域 */}
      <div
        style={{ maxWidth: 1200, margin: '0 auto', padding: '20px 60px 24px' }}
        className="flex w-full flex-col items-center gap-2"
      >
        {attachments.length > 0 && (
          <div className="flex w-full max-w-[756px] flex-wrap gap-2">
            {attachments.map((att) => (
              <div
                key={att.id}
                className="flex items-center gap-1.5 rounded-lg px-2.5 py-1.5"
                style={{ background: '#1e1e1c', border: '0.5px solid #3a3a38' }}
              >
                <Paperclip size={12} style={{ color: '#7b7970', flexShrink: 0 }} />
                <span
                  className="text-xs"
                  style={{ color: '#c2c0b6', maxWidth: '160px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                >
                  {att.name}
                </span>
                <span className="text-xs" style={{ color: '#6b6b68', flexShrink: 0 }}>
                  {formatFileSize(att.size)}
                </span>
                <button
                  type="button"
                  onClick={() => handleRemoveAttachment(att.id)}
                  className="flex items-center justify-center rounded transition-opacity duration-100 hover:opacity-100"
                  style={{ color: '#6b6b68', opacity: 0.7, marginLeft: '2px' }}
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
        />
        <p style={{ color: '#96948d', fontSize: '13px', letterSpacing: '-0.52px', textAlign: 'center' }}>
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
    </div>
  )
}
