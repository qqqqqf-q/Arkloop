import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import type { CodeExecution } from '../components/CodeExecutionCard'
import type { ArtifactRef, SubAgentRef } from '../storage'
import { useSidebarUI } from './app-ui'

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
  | null

type ShareModalState = {
  open: boolean
  sharingMessageId: string | null
  sharedMessageId: string | null
}

type PanelContextValue = {
  activePanel: ActivePanel
  shareModal: ShareModalState
  openSourcePanel: (messageId: string) => void
  openCodePanel: (execution: CodeExecution) => void
  openDocumentPanel: (state: DocumentPanelState) => void
  openAgentPanel: (agent: SubAgentRef) => void
  closePanel: () => void
  openShareModal: (messageId?: string) => void
  closeShareModal: () => void
  setShareState: (sharingId: string | null, sharedId: string | null) => void
}

const Ctx = createContext<PanelContextValue | null>(null)

const defaultShareModal: ShareModalState = {
  open: false,
  sharingMessageId: null,
  sharedMessageId: null,
}

export function PanelProvider({ children }: { children: ReactNode }) {
  const { setRightPanelOpen } = useSidebarUI()

  const [activePanel, setActivePanel] = useState<ActivePanel>(null)
  const [shareModal, setShareModal] = useState<ShareModalState>(defaultShareModal)

  useEffect(() => {
    setRightPanelOpen(activePanel != null)
  }, [activePanel, setRightPanelOpen])

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

  const value = useMemo<PanelContextValue>(
    () => ({
      activePanel,
      shareModal,
      openSourcePanel,
      openCodePanel,
      openDocumentPanel,
      openAgentPanel,
      closePanel,
      openShareModal,
      closeShareModal,
      setShareState,
    }),
    [
      activePanel,
      shareModal,
      openSourcePanel,
      openCodePanel,
      openDocumentPanel,
      openAgentPanel,
      closePanel,
      openShareModal,
      closeShareModal,
      setShareState,
    ],
  )

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function usePanels(): PanelContextValue {
  const ctx = useContext(Ctx)
  if (!ctx) throw new Error('usePanels must be used within PanelProvider')
  return ctx
}
