import { useState, useCallback, useMemo, useRef, useEffect, type FormEvent } from 'react'
import { useNavigate, useOutletContext } from 'react-router-dom'
import { Glasses } from 'lucide-react'
import { ChatInput, type Attachment } from './ChatInput'
import { ErrorCallout, type AppError } from './ErrorCallout'
import { NotificationBell } from './NotificationBell'
import { createThread, createMessage, createRun, uploadThreadAttachment, isApiError, type ThreadResponse, type MeResponse } from '../api'
import { writeActiveThreadIdToStorage, addSearchThreadId, SEARCH_PERSONA_KEY } from '../storage'
import { useLocale } from '../contexts/LocaleContext'
import { buildMessageRequest } from '../messageContent'

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
  refreshCredits: () => void
  onOpenNotifications: () => void
  notificationVersion: number
  creditsBalance: number
  me: MeResponse | null
  isPrivateMode: boolean
  onTogglePrivateMode: () => void
  privateThreadIds: Set<string>
  isSearchMode: boolean
  onEnterSearchMode: () => void
  onExitSearchMode: () => void
  pendingSkillPrompt?: string | null
  onConsumeSkillPrompt?: () => void
  onOpenSettings?: (tab: string) => void
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



export function WelcomePage() {
  const { accessToken, onLoggedOut, onThreadCreated, refreshCredits, onOpenNotifications, notificationVersion, creditsBalance: _creditsBalance, me, isPrivateMode, onTogglePrivateMode, isSearchMode, onEnterSearchMode, onExitSearchMode, pendingSkillPrompt, onConsumeSkillPrompt, onOpenSettings } = useOutletContext<OutletContext>()
  const [draft, setDraft] = useState('')
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const attachmentsRef = useRef<Attachment[]>([])
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<AppError | null>(null)
  const navigate = useNavigate()
  const { t } = useLocale()
  const draftThreadRef = useRef<ThreadResponse | null>(null)
  const draftThreadPromiseRef = useRef<Promise<ThreadResponse> | null>(null)

  const greeting = useMemo(() => buildGreeting(me?.username ?? null, new Date()), [me?.username])

  useEffect(() => {
    if (pendingSkillPrompt) {
      setDraft(pendingSkillPrompt)
      onConsumeSkillPrompt?.()
    }
  }, [pendingSkillPrompt, onConsumeSkillPrompt])

  const [typedGreeting, setTypedGreeting] = useState('')
  useEffect(() => {
    setTypedGreeting('')
    let i = 0
    const id = setInterval(() => {
      i++
      if (i > greeting.length) { clearInterval(id); return }
      setTypedGreeting(greeting.slice(0, i))
    }, 45)
    return () => clearInterval(id)
  }, [greeting])

  const revokeDraftAttachment = useCallback((attachment: Attachment) => {
    if (attachment.preview_url) URL.revokeObjectURL(attachment.preview_url)
  }, [])

  const ensureDraftThread = useCallback((): Promise<ThreadResponse> => {
    if (draftThreadRef.current) return Promise.resolve(draftThreadRef.current)
    if (draftThreadPromiseRef.current) return draftThreadPromiseRef.current
    const promise = createThread(accessToken, { title: t.newChatTitle, is_private: isPrivateMode })
      .then((thread) => { draftThreadRef.current = thread; return thread })
    draftThreadPromiseRef.current = promise
    return promise
  }, [accessToken, isPrivateMode, t.newChatTitle])

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
    for (const att of newAttachments) {
      ensureDraftThread()
        .then((thread) => uploadThreadAttachment(accessToken, thread.id, att.file))
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
  }, [accessToken, ensureDraftThread])

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
    ensureDraftThread()
      .then((thread) => uploadThreadAttachment(accessToken, thread.id, file))
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
  }, [accessToken, ensureDraftThread])

  const handleRemoveAttachment = useCallback((id: string) => {
    setAttachments((prev) => {
      const target = prev.find((item) => item.id === id)
      if (target) revokeDraftAttachment(target)
      return prev.filter((item) => item.id !== id)
    })
  }, [revokeDraftAttachment])

  const handleAsrError = useCallback((err: unknown) => {
    if (isApiError(err) && err.status === 401) {
      onLoggedOut()
      return
    }
    setError(normalizeError(err, t.requestFailed))
  }, [onLoggedOut, t.requestFailed])

  const handleSubmit = async (e: FormEvent<HTMLFormElement>, personaKey: string, modelOverride?: string) => {
    e.preventDefault()
    const text = draft.trim()
    if ((!text && attachments.length === 0) || sending) return

    setSending(true)
    setError(null)

    try {
      const title = deriveTitle(text, t.newChatTitle)
      const thread = draftThreadRef.current
        ? draftThreadRef.current
        : await createThread(accessToken, { title, is_private: isPrivateMode })
      const uploaded = await Promise.all(
        attachments.map(async (attachment) => {
          if (attachment.uploaded) return attachment.uploaded
          return await uploadThreadAttachment(accessToken, thread.id, attachment.file)
        }),
      )
      await createMessage(accessToken, thread.id, buildMessageRequest(text, uploaded))
      const run = await createRun(accessToken, thread.id, personaKey, modelOverride)

      if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(thread.id)
      attachments.forEach((attachment) => revokeDraftAttachment(attachment))
      setDraft('')
      setAttachments([])
      refreshCredits()
      writeActiveThreadIdToStorage(thread.id)
      onThreadCreated(thread)
      navigate(`/t/${thread.id}`, { state: { initialRunId: run.run_id, isSearch: personaKey === SEARCH_PERSONA_KEY } })
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err, t.requestFailed))
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="flex h-full flex-col">
      {/* 顶部 header */}
      <div className="relative z-10 flex min-h-[51px] items-center justify-end gap-2 px-[15px] py-[15px]">
        <NotificationBell accessToken={accessToken} onClick={onOpenNotifications} refreshKey={notificationVersion} title={t.notificationsTitle} />
        <button
          onClick={onTogglePrivateMode}
          title={isPrivateMode ? t.disableIncognito : t.enableIncognito}
          className={[
            'flex h-8 w-8 items-center justify-center rounded-lg transition-colors',
            isPrivateMode
              ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]'
              : 'text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]',
          ].join(' ')}
        >
          <Glasses size={18} />
        </button>
      </div>

      {/* 居中内容 */}
      <div
        className="flex flex-1 flex-col items-center px-5 pt-[27vh]"
      >
        {/* 标题：两层绝对定位交叉淡出，容器高度由下层撑开 */}
        <div className="mb-[40px]" style={{ position: 'relative', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          {/* 常规问候 / 无痕文本 */}
          <h2
            className="text-[40px] font-normal tracking-[-0.5px] text-[var(--c-text-heading)]"
            style={{
              opacity: isSearchMode ? 0 : 1,
              transform: isSearchMode ? 'translateY(-6px)' : 'translateY(0)',
              transition: 'opacity 0.2s ease, transform 0.22s ease',
              pointerEvents: isSearchMode ? 'none' : 'auto',
            }}
          >
            {isPrivateMode ? t.youAreIncognito : typedGreeting}
          </h2>
          {/* Search for everything — 绝对覆盖，不撑开高度 */}
          <h2
            className="absolute text-[40px] font-normal tracking-[-0.5px] text-[var(--c-text-heading)]"
            style={{
              opacity: isSearchMode ? 1 : 0,
              transform: isSearchMode ? 'translateY(0)' : 'translateY(6px)',
              transition: 'opacity 0.2s ease, transform 0.22s ease',
              pointerEvents: isSearchMode ? 'auto' : 'none',
              whiteSpace: 'nowrap',
            }}
          >
            Search for everything
          </h2>
        </div>

        <div className="w-full max-w-[675px]">
          <ChatInput
            value={draft}
            onChange={setDraft}
            onSubmit={handleSubmit}
            placeholder={isSearchMode ? '今天有什么想搜索的吗？' : t.chatPlaceholder}
            disabled={sending}
            isStreaming={false}
            variant="welcome"
            searchMode={isSearchMode}
            attachments={attachments}
            onAttachFiles={handleAttachFiles}
            onPasteContent={handlePasteContent}
            onRemoveAttachment={handleRemoveAttachment}
            accessToken={accessToken}
            onAsrError={handleAsrError}
            onPersonaChange={(personaKey) => {
              if (personaKey === SEARCH_PERSONA_KEY && !isSearchMode) onEnterSearchMode()
              else if (personaKey !== SEARCH_PERSONA_KEY && isSearchMode) onExitSearchMode()
            }}
            onOpenSettings={onOpenSettings}
          />
          {/* incognito note: 平滑展开/收起 */}
          <div
            style={{
              display: 'grid',
              gridTemplateRows: isPrivateMode ? '1fr' : '0fr',
              opacity: isPrivateMode ? 1 : 0,
              transition: 'grid-template-rows 0.2s ease, opacity 0.15s ease',
              overflow: 'hidden',
            }}
          >
            <div style={{ minHeight: 0 }}>
              <p className="mt-2 text-center text-xs" style={{ color: 'var(--c-text-muted)' }}>
                {t.incognitoThreadNote}
              </p>
            </div>
          </div>
          {error && <ErrorCallout error={error} />}
        </div>
      </div>
    </div>
  )
}
