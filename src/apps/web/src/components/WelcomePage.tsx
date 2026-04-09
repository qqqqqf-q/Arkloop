import { useState, useCallback, useMemo, useRef, useEffect, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Glasses } from 'lucide-react'
import { ChatInput, type Attachment, type ChatInputHandle } from './ChatInput'
import { ErrorCallout, type AppError } from './ErrorCallout'
import { NotificationBell } from './NotificationBell'
import { isDesktop } from '@arkloop/shared/desktop'
import { DebugTrigger, useTimeZone } from '@arkloop/shared'
import { createThread, createMessage, createRun, uploadStagingAttachment, isApiError } from '../api'
import {
  writeActiveThreadIdToStorage,
  addSearchThreadId,
  SEARCH_PERSONA_KEY,
  transferGlobalWorkFolderToThread,
  transferGlobalThinkingToThread,
  readSelectedThinkingEnabled,
  readWorkFolder,
  readDeveloperShowDebugPanel,
} from '../storage'
import { useLocale } from '../contexts/LocaleContext'
import { buildMessageRequest } from '../messageContent'
import { useAuth } from '../contexts/auth'
import { useThreadList } from '../contexts/thread-list'
import {
  useAppModeUI,
  useNotificationsUI,
  useSearchUI,
  useSettingsUI,
  useSkillPromptUI,
} from '../contexts/app-ui'
import { useCredits } from '../contexts/credits'

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

type GreetingParts = {
  hour: number
  month: number
  day: number
  weekday: number
  minute: number
}

function getGreetingParts(now: Date, timeZone: string): GreetingParts {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone,
    hour: '2-digit',
    minute: '2-digit',
    month: 'numeric',
    day: 'numeric',
    weekday: 'short',
    hour12: false,
  }).formatToParts(now)
  const getPart = (type: string) => parts.find((part) => part.type === type)?.value ?? '0'
  const weekdayLabel = getPart('weekday')
  const weekdayMap: Record<string, number> = {
    Sun: 0,
    Mon: 1,
    Tue: 2,
    Wed: 3,
    Thu: 4,
    Fri: 5,
    Sat: 6,
  }
  return {
    hour: Number(getPart('hour')),
    minute: Number(getPart('minute')),
    month: Number(getPart('month')) - 1,
    day: Number(getPart('day')),
    weekday: weekdayMap[weekdayLabel] ?? 0,
  }
}

function buildGreeting(name: string | null, now: GreetingParts): string {
  const { hour, month, day, weekday, minute } = now

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
  const seed = minute + hour * 60
  return pool[seed % pool.length]
}



export function WelcomePage() {
  const { accessToken, logout: onLoggedOut, me } = useAuth()
  const { timeZone } = useTimeZone()
  const { addThread: onThreadCreated, isPrivateMode, togglePrivateMode: onTogglePrivateMode } = useThreadList()
  const { isSearchMode, enterSearchMode: onEnterSearchMode, exitSearchMode: onExitSearchMode } = useSearchUI()
  const { openNotifications: onOpenNotifications, notificationVersion } = useNotificationsUI()
  const { openSettings: onOpenSettings } = useSettingsUI()
  const { appMode } = useAppModeUI()
  const { pendingSkillPrompt, consumeSkillPrompt } = useSkillPromptUI()
  const { refreshCredits } = useCredits()
  const [showDebugPanel, setShowDebugPanel] = useState(() => readDeveloperShowDebugPanel())
  const chatInputRef = useRef<ChatInputHandle>(null)
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const attachmentsRef = useRef<Attachment[]>([])
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<AppError | null>(null)
  const navigate = useNavigate()
  const { t } = useLocale()

  const greeting = useMemo(
    () => buildGreeting(me?.username ?? null, getGreetingParts(new Date(), timeZone)),
    [me?.username, timeZone],
  )

  useEffect(() => {
    const handleChange = (e: Event) => {
      setShowDebugPanel((e as CustomEvent<boolean>).detail)
    }
    window.addEventListener('arkloop:developer_show_debug_panel', handleChange)
    return () => window.removeEventListener('arkloop:developer_show_debug_panel', handleChange)
  }, [])

  useEffect(() => {
    if (pendingSkillPrompt) {
      chatInputRef.current?.setValue(pendingSkillPrompt)
      consumeSkillPrompt()
    }
  }, [pendingSkillPrompt, consumeSkillPrompt])

  const [typedGreeting, setTypedGreeting] = useState('')
  useEffect(() => {
    setTypedGreeting('')
    if (isPrivateMode) return
    let i = 0
    const id = setInterval(() => {
      i++
      if (i > greeting.length) { clearInterval(id); return }
      setTypedGreeting(greeting.slice(0, i))
    }, 45)
    return () => clearInterval(id)
  }, [greeting, isPrivateMode])

  const [typedIncognito, setTypedIncognito] = useState('')
  useEffect(() => {
    setTypedIncognito('')
    if (!isPrivateMode) return
    let i = 0
    const text = t.youAreIncognito
    const id = setInterval(() => {
      i++
      if (i > text.length) { clearInterval(id); return }
      setTypedIncognito(text.slice(0, i))
    }, 55)
    return () => clearInterval(id)
  }, [isPrivateMode, t.youAreIncognito])

  const revokeDraftAttachment = useCallback((attachment: Attachment) => {
    if (attachment.preview_url) URL.revokeObjectURL(attachment.preview_url)
  }, [])

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
      uploadStagingAttachment(accessToken, att.file)
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
  }, [accessToken])

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
    uploadStagingAttachment(accessToken, file)
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
  }, [accessToken])

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
    const text = (chatInputRef.current?.getValue() ?? '').trim()
    if ((!text && attachments.length === 0) || sending) return

    setSending(true)
    setError(null)

    try {
      const title = deriveTitle(text, t.newChatTitle)
      const thread = await createThread(accessToken, { title, is_private: isPrivateMode })
      const uploaded = await Promise.all(
        attachments.map(async (attachment) => {
          if (attachment.uploaded) return attachment.uploaded
          return await uploadStagingAttachment(accessToken, attachment.file)
        }),
      )
      const userMessage = await createMessage(accessToken, thread.id, buildMessageRequest(text, uploaded))
      const run = await createRun(
        accessToken,
        thread.id,
        personaKey,
        modelOverride,
        readWorkFolder() ?? undefined,
        readSelectedThinkingEnabled() ? 'enabled' : undefined,
      )

      if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(thread.id)
      attachments.forEach((attachment) => revokeDraftAttachment(attachment))
      chatInputRef.current?.clear()
      setAttachments([])
      refreshCredits()
      writeActiveThreadIdToStorage(thread.id)
      if (appMode === 'work') transferGlobalWorkFolderToThread(thread.id)
      transferGlobalThinkingToThread(thread.id)
      onThreadCreated(thread)
      navigate(`/t/${thread.id}`, {
        state: {
          initialRunId: run.run_id,
          isSearch: personaKey === SEARCH_PERSONA_KEY,
          userEnterMessageId: userMessage.id,
          welcomeUserMessage: userMessage,
        },
      })
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
        {!isDesktop() && (
          <NotificationBell accessToken={accessToken} onClick={onOpenNotifications} refreshKey={notificationVersion} title={t.notificationsTitle} />
        )}
        {!isDesktop() && (
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
        )}
      </div>

      {/* 居中内容 — paddingTop 带过渡动画，模式切换时平滑移动 */}
      <div
        className="flex flex-1 flex-col items-center px-5"
        style={{
          paddingTop: appMode === 'work' ? '32vh' : '27vh',
          transition: 'padding-top 0.38s cubic-bezier(0.16, 1, 0.3, 1)',
        }}
      >
        {/* 标题：三层绝对定位交叉淡出 */}
        <div className="mb-[40px]" style={{ position: 'relative', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          {/* 常规问候 / 无痕文本 */}
          <h2
            className="relative whitespace-nowrap text-[40px] font-normal tracking-[-0.5px] text-[var(--c-text-heading)]"
            style={{
              opacity: (isSearchMode || appMode === 'work') ? 0 : 1,
              transform: (isSearchMode || appMode === 'work') ? 'translateY(-6px)' : 'translateY(0)',
              transition: 'opacity 0.22s ease, transform 0.24s ease',
              pointerEvents: (isSearchMode || appMode === 'work') ? 'none' : 'auto',
            }}
          >
            <span className="invisible select-none" aria-hidden="true">
              {isPrivateMode ? t.youAreIncognito : greeting}
            </span>
            <span className="absolute inset-0">
              {isPrivateMode ? typedIncognito : typedGreeting}
            </span>
          </h2>
          {/* Search for everything */}
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
          {/* Work 模式欢迎语 */}
          <h2
            className="absolute text-[40px] font-normal tracking-[-0.5px] text-[var(--c-text-heading)]"
            style={{
              opacity: appMode === 'work' && !isSearchMode ? 1 : 0,
              transform: appMode === 'work' && !isSearchMode ? 'translateY(0)' : 'translateY(6px)',
              transition: 'opacity 0.22s ease, transform 0.24s ease',
              pointerEvents: appMode === 'work' && !isSearchMode ? 'auto' : 'none',
              whiteSpace: 'nowrap',
            }}
          >
            {t.workGreeting}
          </h2>
        </div>

        <div className="w-full max-w-[675px]">
          <ChatInput
            ref={chatInputRef}
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
            appMode={appMode}
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
      {showDebugPanel && <DebugTrigger />}
    </div>
  )
}
