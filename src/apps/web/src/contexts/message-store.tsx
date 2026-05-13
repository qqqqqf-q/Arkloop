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
import { type AgentMessage, useAgentClient } from '../agent-ui'
import { findAssistantMessageForRun } from '../agentEventProcessing'
import { type Attachment } from '../components/ChatInput'
import { insertMessageByCreatedAt, mergeUndeliveredLocalUserMessages } from '../messageContent'
import { useChatSession } from './chat-session'

interface MessageStoreContextValue {
  messages: AgentMessage[]
  messagesLoading: boolean
  attachments: Attachment[]
  userEnterMessageId: string | null
  pendingIncognito: boolean

  sendMessageRef: React.RefObject<((text: string) => void) | null>
  attachmentsRef: React.RefObject<Attachment[]>

  setMessages: (msgs: AgentMessage[] | ((prev: AgentMessage[]) => AgentMessage[])) => void
  upsertLocalTerminalMessage: (message: AgentMessage) => void
  setMessagesLoading: (v: boolean) => void
  setAttachments: (v: Attachment[] | ((prev: Attachment[]) => Attachment[])) => void
  addAttachment: (a: Attachment) => void
  removeAttachment: (id: string) => void
  setUserEnterMessageId: (v: string | null) => void
  setPendingIncognito: (v: boolean) => void
  beginMessageSync: () => number
  isMessageSyncCurrent: (version: number) => boolean
  invalidateMessageSync: () => void
  readConsistentMessages: (requiredCompletedRunId?: string) => Promise<AgentMessage[]>
  refreshMessages: (options?: { syncVersion?: number; requiredCompletedRunId?: string }) => Promise<AgentMessage[]>
  wasLoadingRef: React.RefObject<boolean>
}

const Ctx = createContext<MessageStoreContextValue | null>(null)

const LOCAL_TERMINAL_MESSAGE_PREFIX = 'local-terminal-run:'

export function isLocalTerminalMessage(message: Pick<AgentMessage, 'id'>): boolean {
  return message.id.startsWith(LOCAL_TERMINAL_MESSAGE_PREFIX)
}

function mergeLocalTerminalMessages(
  remoteMessages: AgentMessage[],
  localMessages: Map<string, AgentMessage>,
): AgentMessage[] {
  const remoteRunIds = new Set<string>()
  for (const message of remoteMessages) {
    if (isLocalTerminalMessage(message)) continue
    if (message.role === 'assistant' && message.streamId) remoteRunIds.add(message.streamId)
  }
  for (const [id, message] of localMessages) {
    if (message.streamId && remoteRunIds.has(message.streamId)) {
      localMessages.delete(id)
    }
  }

  let merged = remoteMessages.filter((message) => !isLocalTerminalMessage(message))
  for (const message of localMessages.values()) {
    if (message.streamId && remoteRunIds.has(message.streamId)) continue
    merged = insertMessageByCreatedAt(merged, message)
  }
  return merged
}

export function MessageStoreProvider({ children }: { children: ReactNode }) {
  const { threadId } = useChatSession()
  return (
    <MessageStoreProviderContent key={threadId ?? '__no_thread__'} threadId={threadId}>
      {children}
    </MessageStoreProviderContent>
  )
}

function MessageStoreProviderContent({ children, threadId }: { children: ReactNode; threadId: string | null }) {
  const agentClient = useAgentClient()

  const [messages, setMessagesState] = useState<AgentMessage[]>([])
  const messagesRef = useRef<AgentMessage[]>([])
  const [messagesLoading, setMessagesLoading] = useState(true)
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const [userEnterMessageId, setUserEnterMessageId] = useState<string | null>(null)
  const [pendingIncognito, setPendingIncognito] = useState(false)

  const sendMessageRef = useRef<((text: string) => void) | null>(null)
  const attachmentsRef = useRef<Attachment[]>(attachments)
  const localTerminalMessagesRef = useRef<Map<string, AgentMessage>>(new Map())
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

  const setMessages = useCallback((value: AgentMessage[] | ((prev: AgentMessage[]) => AgentMessage[])) => {
    setMessagesState((prev) => {
      const next = typeof value === 'function' ? value(prev) : value
      messagesRef.current = next
      return next
    })
  }, [])

  const upsertLocalTerminalMessage = useCallback((message: AgentMessage) => {
    localTerminalMessagesRef.current.set(message.id, message)
    setMessages((prev) => mergeLocalTerminalMessages(prev, localTerminalMessagesRef.current))
  }, [setMessages])

  const readConsistentMessages = useCallback(async (requiredCompletedRunId?: string): Promise<AgentMessage[]> => {
    if (!threadId) return []
    let items = await agentClient.listMessages(threadId)
    items = mergeLocalTerminalMessages(items, localTerminalMessagesRef.current)
    items = mergeUndeliveredLocalUserMessages(items, messagesRef.current)
    if (requiredCompletedRunId && !findAssistantMessageForRun(items, requiredCompletedRunId)) {
      const retriedItems = await agentClient.listMessages(threadId)
      items = mergeLocalTerminalMessages(retriedItems, localTerminalMessagesRef.current)
      items = mergeUndeliveredLocalUserMessages(items, messagesRef.current)
    }
    return items
  }, [agentClient, threadId])

  const refreshMessages = useCallback(async (options?: {
    syncVersion?: number
    requiredCompletedRunId?: string
  }): Promise<AgentMessage[]> => {
    if (!threadId) return []
    const syncVersion = options?.syncVersion ?? beginMessageSync()
    const items = await readConsistentMessages(options?.requiredCompletedRunId)
    if (!isMessageSyncCurrent(syncVersion)) return []
    setMessages(items)
    return items
  }, [threadId, beginMessageSync, readConsistentMessages, isMessageSyncCurrent, setMessages])

  const value = useMemo<MessageStoreContextValue>(() => ({
    messages,
    messagesLoading,
    attachments,
    userEnterMessageId,
    pendingIncognito,
    sendMessageRef,
    attachmentsRef,
    setMessages,
    upsertLocalTerminalMessage,
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
    setMessages,
    upsertLocalTerminalMessage,
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
