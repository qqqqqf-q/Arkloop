import { useCallback, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { openExternal } from '../openExternal'
import { useLocale } from '../contexts/LocaleContext'
import { useAuth } from '../contexts/auth'
import { useThreadList } from '../contexts/thread-list'
import { useChatSession } from '../contexts/chat-session'
import { useMessageStore } from '../contexts/message-store'
import { useRunLifecycle } from '../contexts/run-lifecycle'
import { useStream } from '../contexts/stream'
import {
  forkThread,
  isApiError,
  type RunReasoningMode,
} from '../api'
import { type AgentMessage, type AgentMessageContent, useAgentClient } from '../agent-ui'
import {
  buildMessageRequest,
  buildOptimisticUserMessage,
  buildUserMessageRetryRequest,
  createClientMessageId,
  isLocalUserMessage,
  markDeliveryFailed,
  messageClientMessageId,
  replaceLocalUserMessage,
  undeliveredLocalUserMessages,
  withMessageDeliveryStatus,
} from '../messageContent'
import { createQueuedPrompt } from '../queuedPrompts'
import {
  addSearchThreadId,
  clearThreadRunHandoff,
  migrateMessageMetadata,
  readSelectedModelFromStorage,
  readSelectedPersonaKeyFromStorage,
  readThreadWorkFolder,
  readThreadReasoningMode,
  SEARCH_PERSONA_KEY,
} from '../storage'
import { normalizeError } from '../lib/chat-helpers'
import type { UserInputResponse } from '../userInputTypes'

type UseChatActionsDeps = {
  scrollToBottom: () => void
}

export function useChatActions({ scrollToBottom }: UseChatActionsDeps) {
  const navigate = useNavigate()
  const { t } = useLocale()
  const { accessToken, logout: onLoggedOut } = useAuth()
  const agentClient = useAgentClient()
  const { threads, addThread: onThreadCreated, markRunning: onRunStarted } = useThreadList()
  const { threadId } = useChatSession()
  const {
    messages,
    setMessages,
    setUserEnterMessageId,
    sendMessageRef,
    invalidateMessageSync,
  } = useMessageStore()
  const {
    activeRunId,
    setActiveRunId,
    sending,
    setSending,
    cancelSubmitting,
    setCancelSubmitting,
    setError,
    setInjectionBlocked,
    setQueuedPrompts,
    setAwaitingInput,
    pendingUserInput,
    setPendingUserInput,
    checkInDraft,
    setCheckInDraft,
    checkInSubmitting,
    setCheckInSubmitting,
    markTerminalRunHistory,
    isStreaming,
    injectionBlockedRunIdRef,
    freezeCutoffRef,
    lastVisibleNonTerminalSeqRef,
    noResponseMsgIdRef,
    setTerminalRunDisplayId,
    setTerminalRunHandoffStatus,
    setTerminalRunCoveredRunIds,
  } = useRunLifecycle()
  const {
    resetLiveState,
    setPendingThinking,
    setThinkingHint,
    resetSearchSteps,
  } = useStream()

  const sendMessage = useCallback(async (text: string) => {
    if (!threadId) {
      setError({ message: '当前没有活动会话，无法发送组件消息。' })
      return
    }
    const normalized = text.trim()
    if (!normalized) return
    markTerminalRunHistory(null)
    if (activeRunId || sending) {
      setQueuedPrompts((prev) => [...prev, createQueuedPrompt({ text: normalized })])
      return
    }

    const personaKey = readSelectedPersonaKeyFromStorage()
    const modelOverride = readSelectedModelFromStorage() ?? undefined

    setSending(true)
    setError(null)
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null
    clearThreadRunHandoff(threadId)
    resetLiveState()
    setTerminalRunDisplayId(null)
    setTerminalRunHandoffStatus(null)
    setTerminalRunCoveredRunIds([])
    const deliveryAttemptKeys: Array<{ messageId: string; clientMessageId: string }> = []
    try {
      const clientMessageId = createClientMessageId()
      const request = buildMessageRequest(normalized, [])
      const localMessage = buildOptimisticUserMessage(request, clientMessageId)
      invalidateMessageSync()
      setUserEnterMessageId(localMessage.id)
      setMessages((prev) => [...prev, localMessage])
      scrollToBottom()

      let lastCreatedMessage: AgentMessage | null = null
      const messagesToCreate = undeliveredLocalUserMessages([...messages, localMessage])
      for (const messageToCreate of messagesToCreate) {
        const retryClientMessageId = messageClientMessageId(messageToCreate) ?? createClientMessageId()
        deliveryAttemptKeys.push({ messageId: messageToCreate.id, clientMessageId: retryClientMessageId })
        setMessages((prev) => prev.map((item) =>
          item.id === messageToCreate.id
            ? withMessageDeliveryStatus(item, 'pending', retryClientMessageId)
            : item,
        ))
        const created = await agentClient.createMessage({
          threadId,
          request: buildUserMessageRetryRequest(messageToCreate, retryClientMessageId),
        })
        invalidateMessageSync()
        setUserEnterMessageId(created.id)
        setMessages((prev) => replaceLocalUserMessage(prev, messageToCreate.id, retryClientMessageId, created))
        lastCreatedMessage = created
      }

      if (!lastCreatedMessage) return
      noResponseMsgIdRef.current = lastCreatedMessage.id
      const workFolder = threads.find((thread) => thread.id === threadId)?.sidebar_work_folder ?? readThreadWorkFolder(threadId) ?? undefined
      const run = await agentClient.createRun({
        threadId,
        personaId: personaKey,
        modelOverride,
        workDir: workFolder,
        reasoningMode: readThreadReasoningMode(threadId) !== 'off' ? readThreadReasoningMode(threadId) as RunReasoningMode : undefined,
      })
      if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(threadId)
      resetSearchSteps()
      setActiveRunId(run.id)
      onRunStarted(threadId)
      scrollToBottom()
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setMessages((prev) => markDeliveryFailed(prev, deliveryAttemptKeys))
      setError(normalizeError(err))
    } finally {
      setSending(false)
    }
  }, [
    activeRunId,
    agentClient,
    invalidateMessageSync,
    markTerminalRunHistory,
    messages,
    noResponseMsgIdRef,
    onLoggedOut,
    onRunStarted,
    resetLiveState,
    resetSearchSteps,
    scrollToBottom,
    sending,
    setActiveRunId,
    setError,
    setInjectionBlocked,
    setQueuedPrompts,
    setMessages,
    setSending,
    setTerminalRunDisplayId,
    setTerminalRunHandoffStatus,
    setTerminalRunCoveredRunIds,
    setUserEnterMessageId,
    threadId,
    threads,
    injectionBlockedRunIdRef,
  ])

  useEffect(() => {
    sendMessageRef.current = sendMessage
  }, [sendMessage, sendMessageRef])

  const handleArtifactAction = useCallback((action: { type: string; text?: string; message?: string; url?: string }) => {
    if (action.type === 'prompt' && typeof action.text === 'string' && action.text.trim()) {
      sendMessageRef.current?.(action.text.trim())
      return
    }
    if (action.type === 'open_link' && typeof action.url === 'string') {
      const url = action.url.trim()
      if (url.startsWith('https://') || url.startsWith('http://')) {
        openExternal(url)
      }
      return
    }
    if (action.type === 'error' && typeof action.message === 'string' && action.message.trim()) {
      setError({ message: action.message.trim() })
    }
  }, [sendMessageRef, setError])

  const handleEditMessage = useCallback(async (original: AgentMessage, newContent: string) => {
    if (isStreaming || sending || !threadId) return
    setSending(true)
    setError(null)
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null
    clearThreadRunHandoff(threadId)
    resetLiveState()
    setTerminalRunDisplayId(null)
    setTerminalRunHandoffStatus(null)
    setTerminalRunCoveredRunIds([])
    try {
      const nonTextParts = original.contentJson?.parts?.filter((part) => part.type !== 'text') ?? []
      const newContentJson: AgentMessageContent | undefined = original.contentJson
        ? { parts: [{ type: 'text', text: newContent }, ...nonTextParts] }
        : undefined
      const personaKey = readSelectedPersonaKeyFromStorage() ?? undefined
      const modelOverride = readSelectedModelFromStorage() ?? undefined
      const reasoningMode = readThreadReasoningMode(threadId)
      const run = await agentClient.editMessage({
        threadId,
        messageId: original.id,
        content: newContent,
        contentJson: newContentJson,
        personaId: personaKey,
        modelOverride,
        workDir: threads.find((thread) => thread.id === threadId)?.sidebar_work_folder ?? readThreadWorkFolder(threadId) ?? undefined,
        reasoningMode: reasoningMode !== 'off' ? reasoningMode as RunReasoningMode : undefined,
      })
      invalidateMessageSync()
      setMessages((prev) => {
        const index = prev.findIndex((message) => message.id === original.id)
        if (index === -1) return prev
        return prev.slice(0, index + 1).map((message, currentIndex) =>
          currentIndex === index ? { ...message, content: newContent, contentJson: newContentJson ?? message.contentJson } : message,
        )
      })
      resetSearchSteps()
      setActiveRunId(run.id)
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
  }, [
    agentClient,
    injectionBlockedRunIdRef,
    invalidateMessageSync,
    isStreaming,
    onLoggedOut,
    onRunStarted,
    resetLiveState,
    resetSearchSteps,
    scrollToBottom,
    sending,
    setActiveRunId,
    setError,
    setInjectionBlocked,
    setMessages,
    setSending,
    setTerminalRunDisplayId,
    setTerminalRunHandoffStatus,
    setTerminalRunCoveredRunIds,
    threadId,
    threads,
  ])

  const handleRetryUserMessage = useCallback(async (message: AgentMessage) => {
    if (message.role !== 'user' || isStreaming || sending || !threadId) return
    const personaKey = readSelectedPersonaKeyFromStorage()
    const modelOverride = readSelectedModelFromStorage() ?? undefined
    const thinkingHint = t.copThinkingHints[Math.floor(Math.random() * t.copThinkingHints.length)]
    if (isLocalUserMessage(message)) {
      const clientMessageId = messageClientMessageId(message) ?? createClientMessageId()
      const request = {
        content: message.content || undefined,
        contentJson: message.contentJson,
        clientMessageId,
      }
      setSending(true)
      setError(null)
      setInjectionBlocked(null)
      injectionBlockedRunIdRef.current = null
      clearThreadRunHandoff(threadId)
      resetLiveState()
      setPendingThinking(true)
      setThinkingHint(thinkingHint)
      setTerminalRunDisplayId(null)
      setTerminalRunHandoffStatus(null)
      setTerminalRunCoveredRunIds([])
      setUserEnterMessageId(message.id)
      setMessages((prev) => prev.map((item) =>
        item.id === message.id
          ? withMessageDeliveryStatus(item, 'pending', clientMessageId)
          : item,
      ))
      try {
        const created = await agentClient.createMessage({
          threadId,
          request,
        })
        invalidateMessageSync()
        setUserEnterMessageId(created.id)
        setMessages((prev) => replaceLocalUserMessage(prev, message.id, clientMessageId, created))
        noResponseMsgIdRef.current = created.id
        const reasoningMode = readThreadReasoningMode(threadId)
        const run = await agentClient.createRun({
          threadId,
          personaId: personaKey,
          modelOverride,
          workDir: threads.find((thread) => thread.id === threadId)?.sidebar_work_folder ?? readThreadWorkFolder(threadId) ?? undefined,
          reasoningMode: reasoningMode !== 'off' ? reasoningMode as RunReasoningMode : undefined,
        })
        if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(threadId)
        resetSearchSteps()
        setActiveRunId(run.id)
        onRunStarted(threadId)
        scrollToBottom()
      } catch (err) {
        setPendingThinking(false)
        if (isApiError(err) && err.status === 401) {
          onLoggedOut()
          return
        }
        setMessages((prev) => markDeliveryFailed(prev, [{ messageId: message.id, clientMessageId }]))
        setError(normalizeError(err))
      } finally {
        setSending(false)
      }
      return
    }
    setSending(true)
    setError(null)
    setInjectionBlocked(null)
    injectionBlockedRunIdRef.current = null
    clearThreadRunHandoff(threadId)
    resetLiveState()
    setPendingThinking(true)
    setThinkingHint(thinkingHint)
    setTerminalRunDisplayId(null)
    setTerminalRunHandoffStatus(null)
    setTerminalRunCoveredRunIds([])
    try {
      const reasoningMode = readThreadReasoningMode(threadId)
      const run = await agentClient.retryMessage({
        threadId,
        messageId: message.id,
        personaId: personaKey,
        modelOverride,
        workDir: threads.find((thread) => thread.id === threadId)?.sidebar_work_folder ?? readThreadWorkFolder(threadId) ?? undefined,
        reasoningMode: reasoningMode !== 'off' ? reasoningMode as RunReasoningMode : undefined,
      })
      invalidateMessageSync()
      setMessages((prev) => {
        const index = prev.findIndex((item) => item.id === message.id)
        return index < 0 ? prev : prev.slice(0, index + 1)
      })
      if (personaKey === SEARCH_PERSONA_KEY) addSearchThreadId(threadId)
      resetSearchSteps()
      setActiveRunId(run.id)
      onRunStarted(threadId)
      scrollToBottom()
    } catch (err) {
      setPendingThinking(false)
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    } finally {
      setSending(false)
    }
  }, [
    agentClient,
    injectionBlockedRunIdRef,
    invalidateMessageSync,
    isStreaming,
    noResponseMsgIdRef,
    onLoggedOut,
    onRunStarted,
    resetLiveState,
    resetSearchSteps,
    scrollToBottom,
    sending,
    setActiveRunId,
    setError,
    setInjectionBlocked,
    setMessages,
    setPendingThinking,
    setSending,
    setTerminalRunCoveredRunIds,
    setTerminalRunDisplayId,
    setTerminalRunHandoffStatus,
    setThinkingHint,
    setUserEnterMessageId,
    t.copThinkingHints,
    threadId,
    threads,
  ])

  const handleFork = useCallback(async (messageId: string) => {
    if (!threadId || isStreaming || sending) return
    setError(null)
    setInjectionBlocked(null)
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
  }, [accessToken, isStreaming, navigate, onLoggedOut, onThreadCreated, sending, setError, setInjectionBlocked, threadId])

  const handleCheckInSubmit = useCallback(async () => {
    if (!activeRunId || checkInSubmitting) return
    const text = checkInDraft.trim()
    if (!text) return

    setCheckInSubmitting(true)
    setError(null)
    setInjectionBlocked(null)
    try {
      await agentClient.provideInput(activeRunId, text)
      setCheckInDraft('')
      setAwaitingInput(false)
      setPendingUserInput(null)
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    } finally {
      setCheckInSubmitting(false)
    }
  }, [
    agentClient,
    activeRunId,
    checkInDraft,
    checkInSubmitting,
    onLoggedOut,
    setAwaitingInput,
    setCheckInDraft,
    setCheckInSubmitting,
    setError,
    setInjectionBlocked,
    setPendingUserInput,
  ])

  const handleUserInputSubmit = useCallback(async (response: UserInputResponse) => {
    if (!activeRunId) return
    setError(null)
    setInjectionBlocked(null)
    try {
      await agentClient.provideInput(activeRunId, JSON.stringify(response.answers))
      setPendingUserInput(null)
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    }
  }, [agentClient, activeRunId, onLoggedOut, setError, setInjectionBlocked, setPendingUserInput])

  const handleUserInputDismiss = useCallback(async () => {
    if (!activeRunId || !pendingUserInput) return
    setError(null)
    setInjectionBlocked(null)
    try {
      await agentClient.provideInput(activeRunId, JSON.stringify({}))
      setPendingUserInput(null)
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
    }
  }, [agentClient, activeRunId, onLoggedOut, pendingUserInput, setError, setInjectionBlocked, setPendingUserInput])

  const handleAsrError = useCallback((err: unknown) => {
    if (isApiError(err) && err.status === 401) {
      onLoggedOut()
      return
    }
    setError(normalizeError(err))
  }, [onLoggedOut, setError])

  const handleCancel = useCallback(() => {
    if (!activeRunId || cancelSubmitting) return
    const runId = activeRunId
    const cancelBoundary = Math.max(0, lastVisibleNonTerminalSeqRef.current)
    freezeCutoffRef.current = cancelBoundary

    noResponseMsgIdRef.current = null

    setCancelSubmitting(true)
    setError(null)
    setInjectionBlocked(null)

    let cancelSucceeded = false
    void agentClient.cancelRun(runId, cancelBoundary)
      .then(() => {
        cancelSucceeded = true
      })
      .catch((err: unknown) => {
        setError(normalizeError(err))
      })
      .finally(() => {
        if (!cancelSucceeded) {
          freezeCutoffRef.current = null
          setCancelSubmitting(false)
        }
      })
  }, [
    agentClient,
    activeRunId,
    cancelSubmitting,
    freezeCutoffRef,
    lastVisibleNonTerminalSeqRef,
    noResponseMsgIdRef,
    setCancelSubmitting,
    setError,
    setInjectionBlocked,
  ])

  return {
    sendMessage,
    handleEditMessage,
    handleRetryUserMessage,
    handleFork,
    handleCancel,
    handleCheckInSubmit,
    handleUserInputSubmit,
    handleUserInputDismiss,
    handleAsrError,
    handleArtifactAction,
  }
}
