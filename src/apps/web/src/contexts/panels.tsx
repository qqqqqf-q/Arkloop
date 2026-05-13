import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import type { CodeExecution } from '../components/CodeExecutionCard'
import type { ArtifactRef, SubAgentRef } from '../storage'
import type { ResourceRef } from '../components/resource-preview/types'

export type DocumentPanelState = {
  artifact: ArtifactRef
  artifacts: ArtifactRef[]
  runId?: string
}

type ActivePanel =
  | { type: 'source'; messageId: string }
  | { type: 'code'; execution: CodeExecution }
  | { type: 'document'; artifact: DocumentPanelState }
  | { type: 'agent'; agent: SubAgentRef }
  | { type: 'resource'; resource: ResourceRef }
  | null

type ShareModalState = {
  open: boolean
  sharingMessageId: string | null
  sharedMessageId: string | null
}

type PanelActions = {
  openSourcePanel: (messageId: string) => void
  openCodePanel: (execution: CodeExecution) => void
  openDocumentPanel: (state: DocumentPanelState) => void
  openAgentPanel: (agent: SubAgentRef) => void
  openResourcePanel: (resource: ResourceRef) => void
  closePanel: () => void
  openShareModal: (messageId?: string) => void
  closeShareModal: () => void
  setShareState: (sharingId: string | null, sharedId: string | null) => void
}

type PanelContextValue = PanelActions & {
  activePanel: ActivePanel
  shareModal: ShareModalState
}

const Ctx = createContext<PanelContextValue | null>(null)

// Stable actions context — never changes value, so consumers that only need
// actions (like MessageList for closePanel / openSourcePanel / setShareState)
// never re-render on panel state changes.
const PanelActionsCtx = createContext<PanelActions | null>(null)

// Lightweight context: only the artifact key of the active document panel.
// Changing activePanel does NOT propagate through this context unless the
// document artifact key itself changes, avoiding cascading re-renders in
// MessageList / MessageBubble / MarkdownRenderer.
const ActiveArtifactKeyContext = createContext<string | null>(null)
export const useActiveArtifactKey = () => useContext(ActiveArtifactKeyContext)

const defaultShareModal: ShareModalState = {
  open: false,
  sharingMessageId: null,
  sharedMessageId: null,
}

export function PanelProvider({ children }: { children: ReactNode }) {
  const [activePanel, setActivePanel] = useState<ActivePanel>(null)
  const [shareModal, setShareModal] = useState<ShareModalState>(defaultShareModal)

  const openSourcePanel = useCallback((messageId: string) => {
    setActivePanel({ type: 'source', messageId })
  }, [])

  const openCodePanel = useCallback((execution: CodeExecution) => {
    setActivePanel({ type: 'code', execution })
  }, [])

  const openDocumentPanel = useCallback((state: DocumentPanelState) => {
    setActivePanel({ type: 'document', artifact: state })
  }, [])

  const openAgentPanel = useCallback((agent: SubAgentRef) => {
    setActivePanel({ type: 'agent', agent })
  }, [])

  const openResourcePanel = useCallback((resource: ResourceRef) => {
    setActivePanel({ type: 'resource', resource })
  }, [])

  const closePanel = useCallback(() => {
    setActivePanel(null)
  }, [])

  const openShareModal = useCallback((messageId?: string) => {
    setShareModal((prev) => ({
      ...prev,
      open: true,
      sharingMessageId: messageId ?? prev.sharingMessageId,
    }))
  }, [])

  const closeShareModal = useCallback(() => {
    setShareModal(defaultShareModal)
  }, [])

  const setShareState = useCallback(
    (sharingId: string | null, sharedId: string | null) => {
      setShareModal((prev) => ({
        ...prev,
        sharingMessageId: sharingId,
        sharedMessageId: sharedId,
      }))
    },
    [],
  )

  const actions = useMemo<PanelActions>(
    () => ({
      openSourcePanel,
      openCodePanel,
      openDocumentPanel,
      openAgentPanel,
      openResourcePanel,
      closePanel,
      openShareModal,
      closeShareModal,
      setShareState,
    }),
    [
      openSourcePanel,
      openCodePanel,
      openDocumentPanel,
      openAgentPanel,
      openResourcePanel,
      closePanel,
      openShareModal,
      closeShareModal,
      setShareState,
    ],
  )

  const value = useMemo<PanelContextValue>(
    () => ({
      ...actions,
      activePanel,
      shareModal,
    }),
    [actions, activePanel, shareModal],
  )

  const activePanelArtifactKey = activePanel?.type === 'document' ? activePanel.artifact.artifact.key : null

  return (
    <Ctx.Provider value={value}>
      <PanelActionsCtx.Provider value={actions}>
        <ActiveArtifactKeyContext.Provider value={activePanelArtifactKey}>
          {children}
        </ActiveArtifactKeyContext.Provider>
      </PanelActionsCtx.Provider>
    </Ctx.Provider>
  )
}

export function usePanels(): PanelContextValue {
  const ctx = useContext(Ctx)
  if (!ctx) throw new Error('usePanels must be used within PanelProvider')
  return ctx
}

export function usePanelActions(): PanelActions {
  const ctx = useContext(PanelActionsCtx)
  if (!ctx) throw new Error('usePanelActions must be used within PanelProvider')
  return ctx
}
