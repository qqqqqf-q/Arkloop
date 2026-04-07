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
import { listMessages, type MessageResponse } from '../api'
import { findAssistantMessageForRun } from '../runEventProcessing'
import { type Attachment } from '../components/ChatInput'
import { useAuth } from './auth'
import { useChatSession } from './chat-session'

interface MessageStoreContextValue {
  messages: MessageResponse[]
  messagesLoading: boolean
  attachments: Attachment[]
  userEnterMessageId: string | null
  pendingIncognito: boolean

  sendMessageRef: React.RefObject<((text: string) => void) | null>
  attachmentsRef: React.RefObject<Attachment[]>

  setMessages: (msgs: MessageResponse[] | ((prev: MessageResponse[]) => MessageResponse[])) => void
  setMessagesLoading: (v: boolean) => void
  setAttachments: (v: Attachment[] | ((prev: Attachment[]) => Attachment[])) => void
  addAttachment: (a: Attachment) => void
  removeAttachment: (id: string) => void
  setUserEnterMessageId: (v: string | null) => void
  setPendingIncognito: (v: boolean) => void
  beginMessageSync: () => number
  isMessageSyncCurrent: (version: number) => boolean
  invalidateMessageSync: () => void
  readConsistentMessages: (requiredCompletedRunId?: string) => Promise<MessageResponse[]>
  refreshMessages: (options?: { syncVersion?: number; requiredCompletedRunId?: string }) => Promise<MessageResponse[]>
  wasLoadingRef: React.RefObject<boolean>
}

const Ctx = createContext<MessageStoreContextValue | null>(null)

export function MessageStoreProvider({ children }: { children: ReactNode }) {
  const { threadId } = useChatSession()
  return (
    <MessageStoreProviderContent key={threadId ?? '__no_thread__'} threadId={threadId}>
      {children}
    </MessageStoreProviderContent>
  )
}

function MessageStoreProviderContent({ children, threadId }: { children: ReactNode; threadId: string | null }) {
  const { accessToken } = useAuth()

  const [messages, setMessages] = useState<MessageResponse[]>([])
  const [messagesLoading, setMessagesLoading] = useState(true)
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const [userEnterMessageId, setUserEnterMessageId] = useState<string | null>(null)
  const [pendingIncognito, setPendingIncognito] = useState(false)

  const sendMessageRef = useRef<((text: string) => void) | null>(null)
  const attachmentsRef = useRef<Attachment[]>(attachments)
  useEffect(() => { attachmentsRef.current = attachments }, [attachments])

  const messageSyncVersionRef = useRef(0)
  const wasLoadingRef = useRef(false)
  const addAttachment = useCallback((a: Attachment) => {
    setAttachments((prev) => [...prev, a])
  }, [])

  const removeAttachment = useCallback((id: string) => {
    setAttachments((prev) => prev.filter((a) => a.id !== id))
  }, [])

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

  const refreshMessages = useCallback(async (options?: {
    syncVersion?: number
    requiredCompletedRunId?: string
  }): Promise<MessageResponse[]> => {
    if (!threadId) return []
    const syncVersion = options?.syncVersion ?? beginMessageSync()
    const items = await readConsistentMessages(options?.requiredCompletedRunId)
    if (!isMessageSyncCurrent(syncVersion)) return []
    setMessages(items)
    return items
  }, [threadId, beginMessageSync, readConsistentMessages, isMessageSyncCurrent])

  const value = useMemo<MessageStoreContextValue>(() => ({
    messages,
    messagesLoading,
    attachments,
    userEnterMessageId,
    pendingIncognito,
    sendMessageRef,
    attachmentsRef,
    setMessages,
    setMessagesLoading,
    setAttachments,
    addAttachment,
    removeAttachment,
    setUserEnterMessageId,
    setPendingIncognito,
    beginMessageSync,
    isMessageSyncCurrent,
    invalidateMessageSync,
    readConsistentMessages,
    refreshMessages,
    wasLoadingRef,
  }), [
    messages,
    messagesLoading,
    attachments,
    userEnterMessageId,
    pendingIncognito,
    addAttachment,
    removeAttachment,
    beginMessageSync,
    isMessageSyncCurrent,
    invalidateMessageSync,
    readConsistentMessages,
    refreshMessages,
  ])

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useMessageStore(): MessageStoreContextValue {
  const ctx = useContext(Ctx)
  if (!ctx) throw new Error('useMessageStore must be used within MessageStoreProvider')
  return ctx
}
