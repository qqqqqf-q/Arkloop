import { useState, useCallback, useMemo, type FormEvent } from 'react'
import { useNavigate, useOutletContext } from 'react-router-dom'
import { Glasses, Paperclip, X, Zap } from 'lucide-react'
import { ChatInput, type Attachment, formatFileSize } from './ChatInput'
import { ErrorCallout, type AppError } from './ErrorCallout'
import { NotificationBell } from './NotificationBell'
import { createThread, createMessage, createRun, isApiError, type ThreadResponse, type MeResponse } from '../api'
import { writeActiveThreadIdToStorage } from '../storage'
import { useLocale } from '../contexts/LocaleContext'

function normalizeError(error: unknown, fallback: string): AppError {
  if (isApiError(error)) {
    return { message: error.message, traceId: error.traceId, code: error.code }
  }
  if (error instanceof Error) {
    return { message: error.message }
  }
  return { message: fallback }
}

function deriveTitle(content: string, defaultTitle: string): string {
  const cleaned = content.trim().replace(/\s+/g, ' ')
  if (!cleaned) return defaultTitle
  return cleaned.length > 40 ? `${cleaned.slice(0, 40)}…` : cleaned
}

type OutletContext = {
  accessToken: string
  onLoggedOut: () => void
  onThreadCreated: (thread: ThreadResponse) => void
  onOpenNotifications: () => void
  notificationVersion: number
  creditsBalance: number
  me: MeResponse | null
}

// 按时段、星期、节日生成问候语，全部基于浏览器本地时间。
function buildGreeting(name: string | null, now: Date): string {
  const hour = now.getHours()
  const month = now.getMonth()   // 0-based
  const day = now.getDate()
  const weekday = now.getDay()   // 0=Sun

  const first = name ? name.split(/[\s_]+/)[0] : null
  const hi = first ? `，${first}` : ''

  // 节日优先
  if (month === 11 && day >= 24 && day <= 26) return `圣诞快乐${hi}`
  if (month === 0 && day === 1) return `新年快乐${hi}`
  if (month === 1 && day >= 9 && day <= 15) return `新春快乐${hi}`

  // 周一激励
  if (weekday === 1 && hour >= 8 && hour < 12) {
    return first ? `新的一周，${first}，冲` : '新的一周，冲'
  }

  // 周五
  if (weekday === 5 && hour >= 15) {
    return `周五了${hi}，收工前还有什么要做的？`
  }

  // 深夜
  if (hour >= 0 && hour < 5) {
    return first ? `还没睡，${first}？` : '还没睡？'
  }

  // 时段问候池，每个时段多条随机，避免每次一样
  const pools: Record<string, string[]> = {
    morning: [
      `早上好${hi}`,
      `早${hi}，今天有什么计划？`,
      first ? `${first}，早，喝咖啡了吗？` : '早，喝咖啡了吗？',
    ],
    afternoon: [
      `下午好${hi}`,
      `下午了，有什么需要帮忙的？`,
      first ? `${first}，下午好，进展顺利吗？` : '下午好，进展顺利吗？',
    ],
    evening: [
      `晚上好${hi}`,
      `晚上了，还在忙？`,
      first ? `${first}，晚上好，有什么我能做的？` : '晚上好，有什么我能做的？',
    ],
    generic: [
      first ? `你好，${first}，有什么我能做的？` : '有什么我能做的？',
      `欢迎回来${hi}`,
      first ? `${first}，在这里，说吧` : '在这里，说吧',
      `今天想做什么${hi ? hi.slice(1) + '？' : '？'}`,
    ],
  }

  let pool: string[]
  if (hour >= 5 && hour < 12) pool = pools.morning
  else if (hour >= 12 && hour < 18) pool = pools.afternoon
  else if (hour >= 18 && hour < 24) pool = pools.evening
  else pool = pools.generic

  // 用分钟做伪随机 seed，同一分钟内刷新不跳
  const seed = now.getMinutes() + now.getHours() * 60
  return pool[seed % pool.length]
}

function FreePlanBadge() {
  const [expanded, setExpanded] = useState(false)

  return (
    <div
      className="overflow-hidden rounded-2xl transition-all duration-300 ease-in-out"
      style={{
        background: 'var(--c-bg-deep)',
        border: '0.5px solid var(--c-border-subtle)',
        width: expanded ? '300px' : '200px',
        maxHeight: expanded ? '120px' : '38px',
      }}
    >
      {/* 顶部按钮行 */}
      <div className="flex w-fit items-center" style={{ height: '38px' }}>
        <span
          className="px-4 text-sm"
          style={{ color: 'var(--c-text-muted)', whiteSpace: 'nowrap' }}
        >
          免费计划
        </span>
        <div style={{ width: '0.5px', height: '16px', background: 'var(--c-border)', flexShrink: 0 }} />
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="flex-none pl-4 pr-3 text-sm font-medium transition-opacity duration-150 hover:opacity-80"
          style={{ color: '#4691F6', whiteSpace: 'nowrap' }}
        >
          开始免费试用
        </button>
      </div>

      {/* 展开内容 */}
      <div
        className="px-4 pb-4 text-sm leading-relaxed"
        style={{ color: 'var(--c-text-secondary)', width: '300px' }}
      >
        哈哈，我们目前暂时不收费哦，如果不够用了可以找我来换一点积分。
      </div>
    </div>
  )
}

export function WelcomePage() {
  const { accessToken, onLoggedOut, onThreadCreated, onOpenNotifications, notificationVersion, creditsBalance, me } = useOutletContext<OutletContext>()
  const [draft, setDraft] = useState('')
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<AppError | null>(null)
  const navigate = useNavigate()
  const { t } = useLocale()

  const greeting = useMemo(() => buildGreeting(me?.display_name ?? null, new Date()), [me?.display_name])

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

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const text = draft.trim()
    if ((!text && attachments.length === 0) || sending) return

    setSending(true)
    setError(null)

    try {
      const title = deriveTitle(text, t.newChatTitle)
      const thread = await createThread(accessToken, { title })

      const fileParts = attachments.map(
        (a) => `<file name="${a.name}" encoding="${a.encoding}">\n${a.content}\n</file>`,
      )
      const content = fileParts.length > 0
        ? `${fileParts.join('\n\n')}${text ? `\n\n${text}` : ''}`
        : text

      await createMessage(accessToken, thread.id, { content })
      const run = await createRun(accessToken, thread.id)

      writeActiveThreadIdToStorage(thread.id)
      onThreadCreated(thread)
      navigate(`/t/${thread.id}`, { state: { initialRunId: run.run_id } })
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err, t.requestFailed))
      setSending(false)
    }
  }

  return (
    <div className="flex h-full flex-col">
      {/* 顶部 header */}
      <div className="flex min-h-[51px] items-center justify-end gap-2 px-[15px] py-[15px]">
        <div className="flex items-center gap-1 text-[var(--c-text-secondary)]" style={{ opacity: 0.8 }}>
          <Zap size={13} strokeWidth={2.2} />
          <span className="text-sm font-medium tabular-nums">{creditsBalance.toLocaleString()}</span>
        </div>
        <NotificationBell accessToken={accessToken} onClick={onOpenNotifications} refreshKey={notificationVersion} />
        <button className="flex h-8 w-8 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]">
          <Glasses size={18} />
        </button>
      </div>

      {/* 居中内容 */}
      <div className="flex flex-1 flex-col items-center justify-center px-5" style={{ marginTop: '-120px' }}>
        <FreePlanBadge />

        <h2 className="mt-[40px] mb-[40px] text-[40px] font-normal tracking-[-0.5px] text-[var(--c-text-heading)]">
          {greeting}
        </h2>

        <div className="w-full max-w-[750px]">
          {attachments.length > 0 && (
            <div className="mb-2 flex flex-wrap gap-2">
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
            onSubmit={handleSubmit}
            placeholder={t.chatPlaceholder}
            disabled={sending}
            isStreaming={false}
            variant="welcome"
            attachments={attachments}
            onAttachFiles={handleAttachFiles}
            accessToken={accessToken}
          />

          {error && <ErrorCallout error={error} />}
        </div>
      </div>
    </div>
  )
}
