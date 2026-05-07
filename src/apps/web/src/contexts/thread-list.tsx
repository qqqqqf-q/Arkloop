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
import { silentRefresh } from '@arkloop/shared'
import { getDesktopApi, isDesktop, isLocalMode } from '@arkloop/shared/desktop'
import {
  isApiError,
  listMessages,
  listThreads,
  streamThreadRunStateEvents,
  updateThreadMode,
  updateThreadSidebarState,
  type CollaborationMode,
  type MessageResponse,
  type ThreadGtdBucket,
  type ThreadResponse,
  type UpdateThreadSidebarRequest,
} from '../api'
import {
  clearThreadWorkFolder,
  readGtdArchivedThreadIds,
  readGtdInboxThreadIds,
  readGtdEnabled,
  readGtdSomedayThreadIds,
  readGtdTodoThreadIds,
  readGtdWaitingThreadIds,
  readLegacyThreadModesForMigration,
  readPinnedThreadIds,
  readThreadWorkFolder,
  writeLegacyThreadModesForMigration,
  writeGtdArchivedThreadIds,
  writeGtdInboxThreadIds,
  writeGtdSomedayThreadIds,
  writeGtdTodoThreadIds,
  writeGtdWaitingThreadIds,
  writePinnedThreadIds,
  writeThreadWorkFolder,
  type AppMode,
} from '../storage'
import { useAuth } from './auth'

export interface ThreadListContextValue {
  threads: ThreadResponse[]
  runningThreadIds: Set<string>
  completedUnreadThreadIds: Set<string>
  privateThreadIds: Set<string>
  isPrivateMode: boolean
  pendingIncognitoMode: boolean
  addThread: (thread: ThreadResponse) => void
  upsertThread: (thread: ThreadResponse) => void
  removeThread: (threadId: string) => void
  updateTitle: (threadId: string, title: string) => void
  updateCollaborationMode: (threadId: string, collaborationMode: CollaborationMode, revision?: number) => void
  markRunning: (threadId: string) => void
  markIdle: (threadId: string) => void
  markCompletionRead: (threadId: string) => void
  togglePrivateMode: () => void
  setPendingIncognito: (v: boolean) => void
  getFilteredThreads: (appMode: AppMode) => ThreadResponse[]
}

const ThreadListContext = createContext<ThreadListContextValue | null>(null)
const THREAD_RUN_STATE_RECONNECT_DELAY_MS = 1000
const GTD_BUCKETS: readonly ThreadGtdBucket[] = ['inbox', 'todo', 'waiting', 'someday', 'archived']
const COMPLETION_PROMPT_PREVIEW_LIMIT = 180

type CompletedThreadNotificationTarget = {
  threadId: string
  title?: string | null
}

function sortThreadsByActivity(threads: ThreadResponse[]): ThreadResponse[] {
  return [...threads].sort((a, b) => {
    const left = Date.parse(a.updated_at ?? a.created_at)
    const right = Date.parse(b.updated_at ?? b.created_at)
    return right - left
  })
}

function normalizeSidebarWorkFolder(value: string | null | undefined): string | null {
  const trimmed = (value ?? '').trim()
  return trimmed.length > 0 ? trimmed : null
}

function normalizeNotificationText(value: string): string {
  return value.replace(/\s+/g, ' ').trim()
}

function truncateNotificationText(value: string, limit: number): string {
  if (value.length <= limit) return value
  return `${value.slice(0, limit - 1).trimEnd()}…`
}

function messageUserText(message: MessageResponse): string {
  if (message.content_json?.parts?.length) {
    return message.content_json.parts
      .filter((part) => part.type === 'text')
      .map((part) => part.text)
      .join(' ')
  }
  return message.content
}

function lastUserPromptPreview(messages: MessageResponse[]): string {
  for (let idx = messages.length - 1; idx >= 0; idx--) {
    const message = messages[idx]
    if (message.role !== 'user') continue
    const text = normalizeNotificationText(messageUserText(message))
    if (text) return truncateNotificationText(text, COMPLETION_PROMPT_PREVIEW_LIMIT)
  }
  return ''
}

function legacyGtdBucketForThread(threadId: string): ThreadGtdBucket | null {
  if (readGtdInboxThreadIds().has(threadId)) return 'inbox'
  if (readGtdTodoThreadIds().has(threadId)) return 'todo'
  if (readGtdWaitingThreadIds().has(threadId)) return 'waiting'
  if (readGtdSomedayThreadIds().has(threadId)) return 'someday'
  if (readGtdArchivedThreadIds().has(threadId)) return 'archived'
  return null
}

function buildLegacySidebarPatch(thread: ThreadResponse, pinnedIds = readPinnedThreadIds()): UpdateThreadSidebarRequest {
  const patch: UpdateThreadSidebarRequest = {}
  if (thread.mode === 'work') {
    const folder = normalizeSidebarWorkFolder(readThreadWorkFolder(thread.id))
    if (folder && normalizeSidebarWorkFolder(thread.sidebar_work_folder) === null) {
      patch.sidebar_work_folder = folder
    }
    if (pinnedIds.has(thread.id) && !thread.sidebar_pinned_at) {
      patch.sidebar_pinned = true
    }
  } else {
    const bucket = legacyGtdBucketForThread(thread.id)
    if (bucket && !thread.sidebar_gtd_bucket) {
      patch.sidebar_gtd_bucket = bucket
    }
  }
  return patch
}

function mirrorSidebarStateToLocal(threads: ThreadResponse[], skipThreadIds = new Set<string>()): void {
  const pinnedIds = readPinnedThreadIds()
  const gtdIds: Record<ThreadGtdBucket, Set<string>> = {
    inbox: readGtdInboxThreadIds(),
    todo: readGtdTodoThreadIds(),
    waiting: readGtdWaitingThreadIds(),
    someday: readGtdSomedayThreadIds(),
    archived: readGtdArchivedThreadIds(),
  }

  for (const thread of threads) {
    if (skipThreadIds.has(thread.id)) continue

    const folder = normalizeSidebarWorkFolder(thread.sidebar_work_folder)
    if (folder) {
      if (readThreadWorkFolder(thread.id) !== folder) writeThreadWorkFolder(thread.id, folder)
    } else if (readThreadWorkFolder(thread.id) !== null) {
      clearThreadWorkFolder(thread.id)
    }

    if (thread.sidebar_pinned_at) pinnedIds.add(thread.id)
    else pinnedIds.delete(thread.id)

    for (const bucket of GTD_BUCKETS) {
      gtdIds[bucket].delete(thread.id)
    }
    const bucket = thread.sidebar_gtd_bucket
    if (bucket && GTD_BUCKETS.includes(bucket)) gtdIds[bucket].add(thread.id)
  }

  writePinnedThreadIds(pinnedIds)
  writeGtdInboxThreadIds(gtdIds.inbox)
  writeGtdTodoThreadIds(gtdIds.todo)
  writeGtdWaitingThreadIds(gtdIds.waiting)
  writeGtdSomedayThreadIds(gtdIds.someday)
  writeGtdArchivedThreadIds(gtdIds.archived)
}

export function ThreadListProvider({ children }: { children: ReactNode }) {
  const { accessToken } = useAuth()
  const mountedRef = useRef(true)

  const [threads, setThreads] = useState<ThreadResponse[]>([])
  const threadsRef = useRef<ThreadResponse[]>([])
  const [runningThreadIds, setRunningThreadIds] = useState<Set<string>>(new Set())
  const runningThreadIdsRef = useRef<Set<string>>(new Set())
  const [completedUnreadThreadIds, setCompletedUnreadThreadIds] = useState<Set<string>>(new Set())
  const [privateThreadIds, setPrivateThreadIds] = useState<Set<string>>(new Set())
  const [isPrivateMode, setIsPrivateMode] = useState(false)
  const [pendingIncognitoMode, setPendingIncognitoMode] = useState(false)
  const legacyModeMigrationInFlightRef = useRef(false)

  const replaceRunningThreadIds = useCallback((next: Set<string>) => {
    runningThreadIdsRef.current = next
    setRunningThreadIds(next)
  }, [])

  useEffect(() => {
    threadsRef.current = threads
  }, [threads])

  const notifyCompletedThreads = useCallback((targets: Iterable<CompletedThreadNotificationTarget>) => {
    if (!isDesktop()) return
    const items = Array.from(targets)
    if (items.length === 0 || !accessToken) return
    const api = getDesktopApi()
    if (!api?.config || !api.notifications) return
    void api.config.get().then(async (config) => {
      if (config.desktop?.desktopNotifications === false) return
      const threadsById = new Map(threadsRef.current.map((thread) => [thread.id, thread]))
      await Promise.all(items.map(async (item) => {
        const title = normalizeNotificationText(item.title ?? threadsById.get(item.threadId)?.title ?? '') || 'Arkloop'
        let body = ''
        try {
          body = lastUserPromptPreview(await listMessages(accessToken, item.threadId, 40))
        } catch {
          body = ''
        }
        await api.notifications?.show({ title, body: body || undefined })
      }))
    }).catch(() => {})
  }, [accessToken])

  const addCompletedUnread = useCallback((threadIds: Iterable<string>) => {
    setCompletedUnreadThreadIds((prev) => {
      let next: Set<string> | null = null
      for (const threadId of threadIds) {
        if (prev.has(threadId)) continue
        next ??= new Set(prev)
        next.add(threadId)
      }
      return next ?? prev
    })
  }, [])

  const clearCompletedUnread = useCallback((threadIds: Iterable<string>) => {
    setCompletedUnreadThreadIds((prev) => {
      let next: Set<string> | null = null
      for (const threadId of threadIds) {
        if (!prev.has(threadId)) continue
        next ??= new Set(prev)
        next.delete(threadId)
      }
      return next ?? prev
    })
  }, [])

  const applyThreadState = useCallback((threadId: string, activeRunId: string | null, title?: string | null) => {
    const isRunning = activeRunId != null
    const wasRunning = runningThreadIdsRef.current.has(threadId)
    if (wasRunning && !isRunning) {
      addCompletedUnread([threadId])
      notifyCompletedThreads([{ threadId, title }])
    } else if (isRunning) {
      clearCompletedUnread([threadId])
    }
    if (wasRunning !== isRunning) {
      const next = new Set(runningThreadIdsRef.current)
      if (isRunning) {
        next.add(threadId)
      } else {
        next.delete(threadId)
      }
      replaceRunningThreadIds(next)
    }

    setThreads((prev) => {
      const idx = prev.findIndex((t) => t.id === threadId)
      if (idx < 0) return prev
      const current = prev[idx]
      const nextTitle = title === undefined ? current.title : title
      const updated = current.active_run_id === activeRunId && current.title === nextTitle
        ? current
        : { ...current, active_run_id: activeRunId, title: nextTitle }
      if (activeRunId != null && idx > 0) {
        return [updated, ...prev.slice(0, idx), ...prev.slice(idx + 1)]
      }
      if (updated === current) return prev
      const next = prev.slice()
      next[idx] = updated
      return next
    })
  }, [addCompletedUnread, clearCompletedUnread, notifyCompletedThreads, replaceRunningThreadIds])

  const migrateLegacyThreadModes = useCallback(async (token: string): Promise<ThreadResponse[]> => {
    if (legacyModeMigrationInFlightRef.current) return []
    const legacyModes = readLegacyThreadModesForMigration()
    const workThreadIds = Object.entries(legacyModes)
      .filter(([, mode]) => mode === 'work')
      .map(([threadId]) => threadId)
    if (workThreadIds.length === 0) {
      writeLegacyThreadModesForMigration({})
      return []
    }

    legacyModeMigrationInFlightRef.current = true
    try {
      const migrated: ThreadResponse[] = []
      const remaining: Record<string, AppMode> = {}
      const results = await Promise.allSettled(
        workThreadIds.map((threadId) => updateThreadMode(token, threadId, 'work')),
      )
      results.forEach((result, index) => {
        const threadId = workThreadIds[index]
        if (result.status === 'fulfilled') {
          migrated.push(result.value)
          return
        }
        if (isApiError(result.reason) && [403, 404, 422].includes(result.reason.status)) return
        remaining[threadId] = 'work'
      })
      writeLegacyThreadModesForMigration(remaining)
      return migrated
    } finally {
      legacyModeMigrationInFlightRef.current = false
    }
  }, [])

  const migrateLegacySidebarState = useCallback(async (
    token: string,
    items: ThreadResponse[],
  ): Promise<{ migratedItems: ThreadResponse[]; failedThreadIds: Set<string> }> => {
    const pinnedIds = readPinnedThreadIds()
    const migratedItems: ThreadResponse[] = []
    const failedThreadIds = new Set<string>()
    const patches: Array<{ thread: ThreadResponse; patch: UpdateThreadSidebarRequest }> = []

    for (const thread of items) {
      const patch = buildLegacySidebarPatch(thread, pinnedIds)
      if (Object.keys(patch).length > 0) patches.push({ thread, patch })
    }

    if (patches.length === 0) return { migratedItems, failedThreadIds }

    const results = await Promise.allSettled(
      patches.map(({ thread, patch }) => updateThreadSidebarState(token, thread.id, patch)),
    )
    results.forEach((result, index) => {
      const threadId = patches[index].thread.id
      if (result.status === 'fulfilled') {
        migratedItems.push(result.value)
        return
      }
      if (isApiError(result.reason) && [403, 404, 422].includes(result.reason.status)) return
      failedThreadIds.add(threadId)
    })
    return { migratedItems, failedThreadIds }
  }, [])

  const syncThreadList = useCallback(async (token: string) => {
    const [chatItems, workItems] = await Promise.all([
      listThreads(token, { limit: 200, mode: 'chat' }),
      listThreads(token, { limit: 200, mode: 'work' }),
    ])
    const migratedItems = await migrateLegacyThreadModes(token)
    const modeItemsById = new Map<string, ThreadResponse>()
    for (const thread of [...chatItems, ...workItems, ...migratedItems]) {
      modeItemsById.set(thread.id, thread)
    }
    const sidebarMigration = await migrateLegacySidebarState(token, Array.from(modeItemsById.values()))
    if (!mountedRef.current) return
    const itemsById = new Map<string, ThreadResponse>()
    for (const thread of [...modeItemsById.values(), ...sidebarMigration.migratedItems]) {
      itemsById.set(thread.id, thread)
    }
    const items = sortThreadsByActivity(Array.from(itemsById.values()))
    mirrorSidebarStateToLocal(items, sidebarMigration.failedThreadIds)
    const nextRunning = new Set(items.filter((t) => t.active_run_id != null).map((t) => t.id))
    const completedThreadIds = Array.from(runningThreadIdsRef.current).filter((threadId) => !nextRunning.has(threadId))
    threadsRef.current = items
    setThreads(items)
    replaceRunningThreadIds(nextRunning)
    if (completedThreadIds.length > 0) {
      addCompletedUnread(completedThreadIds)
      notifyCompletedThreads(completedThreadIds.map((threadId) => ({
        threadId,
        title: itemsById.get(threadId)?.title,
      })))
    }
    if (nextRunning.size > 0) clearCompletedUnread(nextRunning)
  }, [addCompletedUnread, clearCompletedUnread, migrateLegacySidebarState, migrateLegacyThreadModes, notifyCompletedThreads, replaceRunningThreadIds])

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  useEffect(() => {
    if (!accessToken) return
    let stopped = false
    let streamController: AbortController | null = null
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    let streamAccessToken = accessToken

    const clearRetryTimer = () => {
      if (retryTimer === null) return
      clearTimeout(retryTimer)
      retryTimer = null
    }

    const scheduleReconnect = (connect: () => void) => {
      clearRetryTimer()
      retryTimer = setTimeout(connect, THREAD_RUN_STATE_RECONNECT_DELAY_MS)
    }

    const connect = () => {
      if (stopped) return
      const controller = new AbortController()
      streamController = controller
      let shouldReconnect = true

      void (async () => {
        await syncThreadList(streamAccessToken)
        if (stopped || controller.signal.aborted) return
        await streamThreadRunStateEvents(streamAccessToken, {
          signal: controller.signal,
          onEvent: (event) => {
            if (stopped) return
            applyThreadState(event.thread_id, event.active_run_id, event.title)
          },
        })
      })()
        .catch((err: unknown) => {
          if (controller.signal.aborted) return
          if (isApiError(err) && err.status === 401 && !isLocalMode()) {
            return silentRefresh()
              .then((token) => { streamAccessToken = token })
              .catch(() => { shouldReconnect = false })
          }
          if (isApiError(err) && (err.status === 401 || err.status === 403)) {
            shouldReconnect = false
          }
        })
        .finally(() => {
          if (stopped || controller.signal.aborted || !shouldReconnect) return
          scheduleReconnect(connect)
        })
    }

    connect()

    return () => {
      stopped = true
      clearRetryTimer()
      streamController?.abort()
    }
  }, [accessToken, applyThreadState, syncThreadList])

  useEffect(() => {
    if (!isDesktop()) return
    void getDesktopApi()?.power?.setSessionActive(runningThreadIds.size > 0).catch(() => {})
  }, [runningThreadIds])

  useEffect(() => () => {
    if (!isDesktop()) return
    void getDesktopApi()?.power?.setSessionActive(false).catch(() => {})
  }, [])

  const addThread = useCallback((thread: ThreadResponse) => {
    let nextThread = thread
    const sidebarPatch: UpdateThreadSidebarRequest = {}
    if (thread.is_private) {
      setPrivateThreadIds((prev) => new Set(prev).add(thread.id))
    } else {
      if (readGtdEnabled() && thread.mode === 'chat') {
        const inboxIds = readGtdInboxThreadIds()
        inboxIds.add(thread.id)
        writeGtdInboxThreadIds(inboxIds)
        sidebarPatch.sidebar_gtd_bucket = 'inbox'
        nextThread = { ...nextThread, sidebar_gtd_bucket: 'inbox' }
      }
      if (thread.mode === 'work') {
        const folder = normalizeSidebarWorkFolder(readThreadWorkFolder(thread.id))
        if (folder) {
          sidebarPatch.sidebar_work_folder = folder
          nextThread = { ...nextThread, sidebar_work_folder: folder }
        }
      }
    }
    setThreads((prev) => {
      if (prev.some((t) => t.id === thread.id)) return prev
      return [nextThread, ...prev]
    })
    if (accessToken && Object.keys(sidebarPatch).length > 0) {
      void updateThreadSidebarState(accessToken, thread.id, sidebarPatch).then((updated) => {
        if (!mountedRef.current) return
        setThreads((prev) => {
          const idx = prev.findIndex((item) => item.id === updated.id)
          if (idx < 0) return [updated, ...prev]
          return prev.map((item, currentIndex) => (currentIndex === idx ? { ...item, ...updated } : item))
        })
      }).catch(() => {})
    }
  }, [accessToken])

  const upsertThread = useCallback((thread: ThreadResponse) => {
    if (thread.is_private) {
      setPrivateThreadIds((prev) => new Set(prev).add(thread.id))
    }
    setThreads((prev) => {
      const idx = prev.findIndex((t) => t.id === thread.id)
      if (idx < 0) return [thread, ...prev]
      return prev.map((item, currentIndex) => (currentIndex === idx ? { ...item, ...thread } : item))
    })
    if (!accessToken || thread.is_private) return
    const patch = buildLegacySidebarPatch(thread)
    if (Object.keys(patch).length === 0) return
    void updateThreadSidebarState(accessToken, thread.id, patch).then((updated) => {
      if (!mountedRef.current) return
      setThreads((prev) => {
        const idx = prev.findIndex((item) => item.id === updated.id)
        if (idx < 0) return [updated, ...prev]
        return prev.map((item, currentIndex) => (currentIndex === idx ? { ...item, ...updated } : item))
      })
      mirrorSidebarStateToLocal([updated])
    }).catch(() => {})
  }, [accessToken])

  const removeThread = useCallback((threadId: string) => {
    setThreads((prev) => prev.filter((t) => t.id !== threadId))
    clearCompletedUnread([threadId])
    if (runningThreadIdsRef.current.has(threadId)) {
      const next = new Set(runningThreadIdsRef.current)
      next.delete(threadId)
      replaceRunningThreadIds(next)
    }
  }, [clearCompletedUnread, replaceRunningThreadIds])

  const updateTitle = useCallback((threadId: string, title: string) => {
    setThreads((prev) =>
      prev.map((t) => (t.id === threadId ? { ...t, title } : t)),
    )
  }, [])

  const updateCollaborationMode = useCallback((threadId: string, collaborationMode: CollaborationMode, revision?: number) => {
    setThreads((prev) =>
      prev.map((t) => {
        if (t.id !== threadId) return t
        if (revision !== undefined && revision <= (t.collaboration_mode_revision ?? 0)) return t
        return {
          ...t,
          collaboration_mode: collaborationMode,
          collaboration_mode_revision: revision ?? t.collaboration_mode_revision,
        }
      }),
    )
  }, [])

  const markRunning = useCallback((threadId: string) => {
    clearCompletedUnread([threadId])
    if (!runningThreadIdsRef.current.has(threadId)) {
      replaceRunningThreadIds(new Set(runningThreadIdsRef.current).add(threadId))
    }
    setThreads((prev) => {
      const idx = prev.findIndex((t) => t.id === threadId)
      if (idx <= 0) return prev
      const thread = prev[idx]
      return [thread, ...prev.slice(0, idx), ...prev.slice(idx + 1)]
    })
  }, [clearCompletedUnread, replaceRunningThreadIds])

  const markIdle = useCallback((threadId: string) => {
    applyThreadState(threadId, null)
  }, [applyThreadState])

  const markCompletionRead = useCallback((threadId: string) => {
    clearCompletedUnread([threadId])
  }, [clearCompletedUnread])

  const togglePrivateMode = useCallback(() => {
    setIsPrivateMode((prev) => !prev)
  }, [])

  const threadsByMode = useMemo<Record<AppMode, ThreadResponse[]>>(() => {
    const grouped: Record<AppMode, ThreadResponse[]> = {
      chat: [],
      work: [],
    }
    for (const thread of threads) {
      if (thread.is_private) continue
      const mode = thread.mode === 'work' ? 'work' : 'chat'
      grouped[mode].push(thread)
    }
    return grouped
  }, [threads])

  const getFilteredThreads = useCallback(
    (appMode: AppMode): ThreadResponse[] => threadsByMode[appMode],
    [threadsByMode],
  )

  const value = useMemo<ThreadListContextValue>(() => ({
    threads,
    runningThreadIds,
    completedUnreadThreadIds,
    privateThreadIds,
    isPrivateMode,
    pendingIncognitoMode,
    addThread,
    upsertThread,
    removeThread,
    updateTitle,
    updateCollaborationMode,
    markRunning,
    markIdle,
    markCompletionRead,
    togglePrivateMode,
    setPendingIncognito: setPendingIncognitoMode,
    getFilteredThreads,
  }), [
    threads,
    runningThreadIds,
    completedUnreadThreadIds,
    privateThreadIds,
    isPrivateMode,
    pendingIncognitoMode,
    addThread,
    upsertThread,
    removeThread,
    updateTitle,
    updateCollaborationMode,
    markRunning,
    markIdle,
    markCompletionRead,
    togglePrivateMode,
    getFilteredThreads,
  ])

  return (
    <ThreadListContext.Provider value={value}>
      {children}
    </ThreadListContext.Provider>
  )
}

export function ThreadListContextBridge({
  value,
  children,
}: {
  value: ThreadListContextValue
  children: ReactNode
}) {
  return (
    <ThreadListContext.Provider value={value}>
      {children}
    </ThreadListContext.Provider>
  )
}

export function useThreadList(): ThreadListContextValue {
  const ctx = useContext(ThreadListContext)
  if (!ctx) throw new Error('useThreadList must be used within ThreadListProvider')
  return ctx
}
