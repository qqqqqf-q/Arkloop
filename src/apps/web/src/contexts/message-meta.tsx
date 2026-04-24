import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import type {
  ArtifactRef,
  BrowserActionRef,
  CodeExecutionRef,
  FileOpRef,
  MessageSearchStepRef,
  MessageTerminalStatusRef,
  MessageThinkingRef,
  MsgRunEvent,
  SubAgentRef,
  WebFetchRef,
  WebSource,
  WidgetRef,
} from '../storage'
import {
  readMessageArtifacts,
  readMessageAssistantTurn,
  readMessageBrowserActions,
  readMessageCodeExecutions,
  readMessageCoveredRunIds,
  readMessageFileOps,
  readMessageSearchSteps,
  readMessageSources,
  readMessageSubAgents,
  readMessageThinking,
  readMessageWebFetches,
  readMessageWidgets,
  readMsgRunEvents,
  writeMessageArtifacts,
  writeMessageAssistantTurn,
  writeMessageBrowserActions,
  writeMessageCodeExecutions,
  writeMessageCoveredRunIds,
  writeMessageFileOps,
  writeMessageSearchSteps,
  writeMessageSources,
  writeMessageSubAgents,
  writeMessageTerminalStatus,
  writeMessageThinking,
  writeMessageWebFetches,
  writeMessageWidgets,
  writeMsgRunEvents,
  writeThreadRunHandoff,
} from '../storage'
import type { AssistantTurnUi } from '../assistantTurnSegments'
import type { AppError } from '@arkloop/shared'

export type MessageMeta = {
  sources?: WebSource[]
  artifacts?: ArtifactRef[]
  codeExecutions?: CodeExecutionRef[]
  browserActions?: BrowserActionRef[]
  subAgents?: SubAgentRef[]
  fileOps?: FileOpRef[]
  webFetches?: WebFetchRef[]
  thinking?: MessageThinkingRef
  searchSteps?: MessageSearchStepRef[]
  assistantTurn?: AssistantTurnUi
  widgets?: WidgetRef[]
  runEvents?: MsgRunEvent[]
  failedError?: AppError
  coveredRunIds?: string[]
}

interface MessageMetaContextValue {
  metaMap: Map<string, MessageMeta>

  // live run refs (SSE 热路径写入，不触发渲染)
  currentRunSourcesRef: React.RefObject<WebSource[]>
  currentRunArtifactsRef: React.RefObject<ArtifactRef[]>
  currentRunCodeExecutionsRef: React.RefObject<CodeExecutionRef[]>
  currentRunBrowserActionsRef: React.RefObject<BrowserActionRef[]>
  currentRunSubAgentsRef: React.RefObject<SubAgentRef[]>
  currentRunFileOpsRef: React.RefObject<FileOpRef[]>
  currentRunWebFetchesRef: React.RefObject<WebFetchRef[]>
  pendingSearchStepsRef: React.RefObject<MessageSearchStepRef[] | null>

  getMeta: (msgId: string) => MessageMeta | undefined
  setMeta: (msgId: string, partial: Partial<MessageMeta>) => void
  setMetaBatch: (entries: Array<[string, Partial<MessageMeta>]>) => void
  persistToStorage: (msgId: string, meta: MessageMeta) => void
  loadFromStorage: (msgIds: string[]) => void
  clearAll: () => void
  persistRunDataToMessage: (
    messageId: string,
    runData: {
      runSources: WebSource[]
      runArtifacts: ArtifactRef[]
      runWidgets: WidgetRef[]
      runCodeExecs: CodeExecutionRef[]
      runBrowserActions: BrowserActionRef[]
      runSubAgents: SubAgentRef[]
      runFileOps: FileOpRef[]
      runWebFetches: WebFetchRef[]
      runAssistantTurn: AssistantTurnUi
      handoffAssistantTurn: AssistantTurnUi
      pendingSearchSteps?: MessageSearchStepRef[] | null
      terminalStatus?: MessageTerminalStatusRef | null
      coveredRunIds?: string[]
    },
    runEvents: MsgRunEvent[],
    options?: {
      persistAssistantTurn?: boolean
      cacheAssistantTurn?: boolean
    },
  ) => void
  persistThreadRunHandoff: (
    threadId: string,
    runId: string,
    runData: {
      terminalStatus?: 'running' | MessageTerminalStatusRef | null
      coveredRunIds?: string[]
      handoffAssistantTurn: AssistantTurnUi
      runSources: WebSource[]
      runArtifacts: ArtifactRef[]
      runWidgets: WidgetRef[]
      runCodeExecs: CodeExecutionRef[]
      runBrowserActions: BrowserActionRef[]
      runSubAgents: SubAgentRef[]
      runFileOps: FileOpRef[]
      runWebFetches: WebFetchRef[]
      pendingSearchSteps?: MessageSearchStepRef[] | null
    },
  ) => void
  clearRunRefs: () => void
}

const MessageMetaContext = createContext<MessageMetaContextValue | null>(null)

function mergePartial(prev: MessageMeta | undefined, partial: Partial<MessageMeta>): MessageMeta {
  return { ...prev, ...partial }
}

export function MessageMetaProvider({ children }: { children: ReactNode }) {
  const [metaMap, setMetaMap] = useState<Map<string, MessageMeta>>(new Map())

  const currentRunSourcesRef = useRef<WebSource[]>([])
  const currentRunArtifactsRef = useRef<ArtifactRef[]>([])
  const currentRunCodeExecutionsRef = useRef<CodeExecutionRef[]>([])
  const currentRunBrowserActionsRef = useRef<BrowserActionRef[]>([])
  const currentRunSubAgentsRef = useRef<SubAgentRef[]>([])
  const currentRunFileOpsRef = useRef<FileOpRef[]>([])
  const currentRunWebFetchesRef = useRef<WebFetchRef[]>([])
  const pendingSearchStepsRef = useRef<MessageSearchStepRef[] | null>(null)

  const getMeta = useCallback(
    (msgId: string): MessageMeta | undefined => metaMap.get(msgId),
    [metaMap],
  )

  const setMeta = useCallback((msgId: string, partial: Partial<MessageMeta>) => {
    setMetaMap((prev) => {
      const next = new Map(prev)
      next.set(msgId, mergePartial(prev.get(msgId), partial))
      return next
    })
  }, [])

  const setMetaBatch = useCallback((entries: Array<[string, Partial<MessageMeta>]>) => {
    if (entries.length === 0) return
    setMetaMap((prev) => {
      const next = new Map(prev)
      for (const [msgId, partial] of entries) {
        next.set(msgId, mergePartial(prev.get(msgId), partial))
      }
      return next
    })
  }, [])

  const persistToStorage = useCallback((msgId: string, meta: MessageMeta) => {
    if (meta.sources && meta.sources.length > 0) writeMessageSources(msgId, meta.sources)
    if (meta.artifacts && meta.artifacts.length > 0) writeMessageArtifacts(msgId, meta.artifacts)
    if (meta.codeExecutions) writeMessageCodeExecutions(msgId, meta.codeExecutions)
    if (meta.browserActions && meta.browserActions.length > 0) writeMessageBrowserActions(msgId, meta.browserActions)
    if (meta.subAgents && meta.subAgents.length > 0) writeMessageSubAgents(msgId, meta.subAgents)
    if (meta.fileOps && meta.fileOps.length > 0) writeMessageFileOps(msgId, meta.fileOps)
    if (meta.webFetches && meta.webFetches.length > 0) writeMessageWebFetches(msgId, meta.webFetches)
    if (meta.thinking) writeMessageThinking(msgId, meta.thinking)
    if (meta.searchSteps && meta.searchSteps.length > 0) writeMessageSearchSteps(msgId, meta.searchSteps)
    if (meta.assistantTurn) writeMessageAssistantTurn(msgId, meta.assistantTurn)
    if (meta.widgets && meta.widgets.length > 0) writeMessageWidgets(msgId, meta.widgets)
    if (meta.runEvents && meta.runEvents.length > 0) writeMsgRunEvents(msgId, meta.runEvents)
    if (meta.coveredRunIds && meta.coveredRunIds.length > 0) writeMessageCoveredRunIds(msgId, meta.coveredRunIds)
  }, [])

  const loadFromStorage = useCallback((msgIds: string[]) => {
    if (msgIds.length === 0) return
    const entries: Array<[string, MessageMeta]> = []
    for (const id of msgIds) {
      const meta: MessageMeta = {}
      const sources = readMessageSources(id)
      if (sources) meta.sources = sources
      const artifacts = readMessageArtifacts(id)
      if (artifacts) meta.artifacts = artifacts
      const codeExecutions = readMessageCodeExecutions(id)
      if (codeExecutions) meta.codeExecutions = codeExecutions
      const browserActions = readMessageBrowserActions(id)
      if (browserActions) meta.browserActions = browserActions
      const subAgents = readMessageSubAgents(id)
      if (subAgents) meta.subAgents = subAgents
      const fileOps = readMessageFileOps(id)
      if (fileOps) meta.fileOps = fileOps
      const webFetches = readMessageWebFetches(id)
      if (webFetches) meta.webFetches = webFetches
      const thinking = readMessageThinking(id)
      if (thinking) meta.thinking = thinking
      const searchSteps = readMessageSearchSteps(id)
      if (searchSteps) meta.searchSteps = searchSteps
      const assistantTurn = readMessageAssistantTurn(id)
      if (assistantTurn) meta.assistantTurn = assistantTurn
      const widgets = readMessageWidgets(id)
      if (widgets) meta.widgets = widgets
      const runEvents = readMsgRunEvents(id)
      if (runEvents) meta.runEvents = runEvents
      const coveredRunIds = readMessageCoveredRunIds(id)
      if (coveredRunIds) meta.coveredRunIds = coveredRunIds
      if (Object.keys(meta).length > 0) entries.push([id, meta])
    }
    if (entries.length === 0) return
    setMetaMap((prev) => {
      const next = new Map(prev)
      for (const [id, meta] of entries) {
        next.set(id, mergePartial(prev.get(id), meta))
      }
      return next
    })
  }, [])

  const clearAll = useCallback(() => {
    setMetaMap(new Map())
    currentRunSourcesRef.current = []
    currentRunArtifactsRef.current = []
    currentRunCodeExecutionsRef.current = []
    currentRunBrowserActionsRef.current = []
    currentRunSubAgentsRef.current = []
    currentRunFileOpsRef.current = []
    currentRunWebFetchesRef.current = []
    pendingSearchStepsRef.current = null
  }, [])

  const persistRunDataToMessage = useCallback(
    (
      messageId: string,
      runData: {
        runSources: WebSource[]
        runArtifacts: ArtifactRef[]
        runWidgets: WidgetRef[]
        runCodeExecs: CodeExecutionRef[]
        runBrowserActions: BrowserActionRef[]
        runSubAgents: SubAgentRef[]
        runFileOps: FileOpRef[]
        runWebFetches: WebFetchRef[]
        runAssistantTurn: AssistantTurnUi
        handoffAssistantTurn: AssistantTurnUi
        pendingSearchSteps?: MessageSearchStepRef[] | null
        terminalStatus?: MessageTerminalStatusRef | null
        coveredRunIds?: string[]
      },
      runEvents: MsgRunEvent[],
      options?: {
        persistAssistantTurn?: boolean
        cacheAssistantTurn?: boolean
      },
    ) => {
      const persistAssistantTurn = options?.persistAssistantTurn ?? true
      const cacheAssistantTurn = options?.cacheAssistantTurn ?? true
      const meta: MessageMeta = {}

      if (runData.runWidgets.length > 0) {
        writeMessageWidgets(messageId, runData.runWidgets)
        meta.widgets = runData.runWidgets
      }
      if (runData.runSources.length > 0) {
        writeMessageSources(messageId, runData.runSources)
        meta.sources = runData.runSources
      }
      if (runData.pendingSearchSteps && runData.pendingSearchSteps.length > 0) {
        writeMessageSearchSteps(messageId, runData.pendingSearchSteps)
        meta.searchSteps = runData.pendingSearchSteps
      }
      if (runData.terminalStatus) {
        writeMessageTerminalStatus(messageId, runData.terminalStatus)
      }
      if (runData.coveredRunIds && runData.coveredRunIds.length > 0) {
        writeMessageCoveredRunIds(messageId, runData.coveredRunIds)
        meta.coveredRunIds = runData.coveredRunIds
      }
      if (runData.runAssistantTurn.segments.length > 0) {
        if (persistAssistantTurn) {
          writeMessageAssistantTurn(messageId, runData.runAssistantTurn)
        }
        if (cacheAssistantTurn) {
          meta.assistantTurn = runData.runAssistantTurn
        }
      }
      if (runData.runArtifacts.length > 0) {
        writeMessageArtifacts(messageId, runData.runArtifacts)
        meta.artifacts = runData.runArtifacts
      }
      writeMessageCodeExecutions(messageId, runData.runCodeExecs)
      meta.codeExecutions = runData.runCodeExecs
      if (runData.runBrowserActions.length > 0) {
        writeMessageBrowserActions(messageId, runData.runBrowserActions)
        meta.browserActions = runData.runBrowserActions
      }
      if (runData.runSubAgents.length > 0) {
        writeMessageSubAgents(messageId, runData.runSubAgents)
        meta.subAgents = runData.runSubAgents
      }
      if (runData.runFileOps.length > 0) {
        writeMessageFileOps(messageId, runData.runFileOps)
        meta.fileOps = runData.runFileOps
      }
      if (runData.runWebFetches.length > 0) {
        writeMessageWebFetches(messageId, runData.runWebFetches)
        meta.webFetches = runData.runWebFetches
      }
      if (runEvents.length > 0) {
        writeMsgRunEvents(messageId, runEvents)
        meta.runEvents = runEvents
      }

      if (Object.keys(meta).length > 0) {
        setMeta(messageId, meta)
      }
    },
    [setMeta],
  )

  const persistThreadRunHandoff = useCallback(
    (
      threadId: string,
      runId: string,
      runData: {
        terminalStatus?: 'running' | MessageTerminalStatusRef | null
        coveredRunIds?: string[]
        handoffAssistantTurn: AssistantTurnUi
        runSources: WebSource[]
        runArtifacts: ArtifactRef[]
        runWidgets: WidgetRef[]
        runCodeExecs: CodeExecutionRef[]
        runBrowserActions: BrowserActionRef[]
        runSubAgents: SubAgentRef[]
        runFileOps: FileOpRef[]
        runWebFetches: WebFetchRef[]
        pendingSearchSteps?: MessageSearchStepRef[] | null
      },
    ) => {
      if (!threadId || !runId) return
      writeThreadRunHandoff(threadId, {
        runId,
        status: (runData.terminalStatus ?? 'cancelled') as 'running' | Exclude<MessageTerminalStatusRef, 'completed'>,
        coveredRunIds: [...(runData.coveredRunIds ?? [])],
        assistantTurn: runData.handoffAssistantTurn.segments.length > 0 ? runData.handoffAssistantTurn : null,
        sources: [...runData.runSources],
        artifacts: [...runData.runArtifacts],
        widgets: [...runData.runWidgets],
        codeExecutions: [...runData.runCodeExecs],
        browserActions: [...runData.runBrowserActions],
        subAgents: [...runData.runSubAgents],
        fileOps: [...runData.runFileOps],
        webFetches: [...runData.runWebFetches],
        searchSteps: [...(runData.pendingSearchSteps ?? [])],
      })
    },
    [],
  )

  const clearRunRefs = useCallback(() => {
    currentRunSourcesRef.current = []
    currentRunArtifactsRef.current = []
    currentRunCodeExecutionsRef.current = []
    currentRunBrowserActionsRef.current = []
    currentRunSubAgentsRef.current = []
    currentRunFileOpsRef.current = []
    currentRunWebFetchesRef.current = []
    pendingSearchStepsRef.current = null
  }, [])

  const value = useMemo<MessageMetaContextValue>(() => ({
    metaMap,
    currentRunSourcesRef,
    currentRunArtifactsRef,
    currentRunCodeExecutionsRef,
    currentRunBrowserActionsRef,
    currentRunSubAgentsRef,
    currentRunFileOpsRef,
    currentRunWebFetchesRef,
    pendingSearchStepsRef,
    getMeta,
    setMeta,
    setMetaBatch,
    persistToStorage,
    loadFromStorage,
    clearAll,
    persistRunDataToMessage,
    persistThreadRunHandoff,
    clearRunRefs,
  }), [
    metaMap,
    getMeta,
    setMeta,
    setMetaBatch,
    persistToStorage,
    loadFromStorage,
    clearAll,
    persistRunDataToMessage,
    persistThreadRunHandoff,
    clearRunRefs,
  ])

  return (
    <MessageMetaContext.Provider value={value}>
      {children}
    </MessageMetaContext.Provider>
  )
}

export function useMessageMeta(): MessageMetaContextValue {
  const ctx = useContext(MessageMetaContext)
  if (!ctx) throw new Error('useMessageMeta must be used within MessageMetaProvider')
  return ctx
}
